// Package executor executes remote commands sequentially over one SSH client.
// Each command uses a fresh SSH session, because ssh.Session supports only one
// Start/Run call. The underlying authenticated SSH connection is reused.
package executor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Recorder keeps all commands and response lines in one persistent job file.
type Recorder interface {
	Request(string) error
	Response(string) error
	Flush() error
}

// DialConfig requires known_hosts verification; accepting unknown host keys is
// intentionally not supported for production connections.
type DialConfig struct {
	Address        string
	User           string
	PrivateKeyPath string
	KnownHostsPath string
	Timeout        time.Duration
}

func Dial(config DialConfig) (*ssh.Client, error) {
	if config.Address == "" || config.User == "" ||
		config.PrivateKeyPath == "" || config.KnownHostsPath == "" {
		return nil, errors.New("SSH address, user, private key and known_hosts are required")
	}
	key, err := os.ReadFile(config.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse private key (unencrypted key required): %w", err)
	}
	hostKeyCallback, err := knownhosts.New(config.KnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	if config.Timeout <= 0 {
		config.Timeout = 15 * time.Second
	}
	client, err := ssh.Dial("tcp", config.Address, &ssh.ClientConfig{
		User:            config.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         config.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("connect SSH: %w", err)
	}
	return client, nil
}

// Executor streams output to Console and a Recorder while reusing Client.
type Executor struct {
	Client         *ssh.Client
	Workspace      string
	Console        io.Writer
	CommandTimeout time.Duration
	outputMu       sync.Mutex
}

// Run executes commands in their declared order. If allowFailure is true,
// only nonzero remote exit statuses are ignored; transport/log errors are fatal.
func (e *Executor) Run(ctx context.Context, job string, commands []string, allowFailure bool, record Recorder) error {
	if e.Client == nil || record == nil {
		return errors.New("SSH client and recorder are required")
	}
	if e.Workspace == "" || strings.ContainsAny(e.Workspace, "\r\n\x00") {
		return errors.New("invalid workspace")
	}
	for index, command := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		// The target directory is quoted, while the command is deliberately
		// interpreted by the remote POSIX shell as requested by the manifest.
		script := "cd " + shellQuote(e.Workspace) + " && " + command
		if err := record.Request(script); err != nil {
			return fmt.Errorf("write job %s request: %w", job, err)
		}
		err := e.executeOne(ctx, job, script, record)
		if flushErr := record.Flush(); flushErr != nil {
			return fmt.Errorf("flush job %s log: %w", job, flushErr)
		}
		if err != nil {
			var exitError *ssh.ExitError
			if allowFailure && errors.As(err, &exitError) {
				e.outputMu.Lock()
				if e.Console != nil {
					_, _ = fmt.Fprintf(e.Console, "[%s] command %d exited with %d (allow_failure=true)\n",
						job, index+1, exitError.ExitStatus())
				}
				e.outputMu.Unlock()
				continue
			}
			return fmt.Errorf("job %s, command %d: %w", job, index+1, err)
		}
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// executeOne starts "sh -s", sends one script through StdinPipe, consumes both
// output streams concurrently and calls Wait before allowing another command.
func (e *Executor) executeOne(parent context.Context, job, script string, record Recorder) error {
	ctx := parent
	cancel := func() {}
	if e.CommandTimeout > 0 {
		ctx, cancel = context.WithTimeout(parent, e.CommandTimeout)
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	session, err := e.Client.NewSession()
	if err != nil {
		return fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()
	stopOnCancel := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stopOnCancel()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("open stderr: %w", err)
	}
	if err := session.Start("sh -s"); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	// Never wait for exit while ignoring stderr: SSH streams share finite
	// buffering and an undrained stream can block the guest process.
	result := make(chan error, 2)
	for _, stream := range []struct {
		source string
		reader io.Reader
	}{{"stdout", stdout}, {"stderr", stderr}} {
		go func(source string, reader io.Reader) {
			result <- streamLines(reader, func(line string) error {
				e.outputMu.Lock()
				defer e.outputMu.Unlock()
				if err := record.Response(line); err != nil {
					_ = session.Close()
					return err
				}
				if e.Console == nil {
					return nil
				}
				label := "[" + job + "]"
				if source == "stderr" {
					label += "[stderr]"
				}
				_, err := fmt.Fprintf(e.Console, "%s %s\n", label, line)
				return err
			})
		}(stream.source, stream.reader)
	}
	_, writeErr := io.WriteString(stdin, script+"\n")
	closeErr := stdin.Close()
	if writeErr != nil || closeErr != nil {
		_ = session.Close()
	}
	streamErr1, streamErr2 := <-result, <-result
	waitErr := session.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(writeErr, closeErr, streamErr1, streamErr2, waitErr)
}

// streamLines emits the final non-newline-terminated line as well. Reading
// lines instead of fixed-size scanner tokens prevents 64-KiB truncation.
func streamLines(r io.Reader, emit func(string) error) error {
	reader := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := reader.ReadString('\n')
		if len(line) != 0 {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if emitErr := emit(line); emitErr != nil {
				return emitErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
