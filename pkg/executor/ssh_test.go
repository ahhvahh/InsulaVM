package executor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ahhvahh/InsulaVM/pkg/storage"
	"golang.org/x/crypto/ssh"
)

func startTestSSH(t *testing.T) *ssh.Client {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_, channels, requests, err := ssh.NewServerConn(raw, config)
				if err != nil {
					_ = raw.Close()
					return
				}
				go ssh.DiscardRequests(requests)
				for newChannel := range channels {
					if newChannel.ChannelType() != "session" {
						_ = newChannel.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, reqs, err := newChannel.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer channel.Close()
						for req := range reqs {
							if req.Type != "exec" {
								_ = req.Reply(false, nil)
								continue
							}
							_ = req.Reply(true, nil)
							command := exec.Command("sh", "-s")
							command.Stdin = channel
							command.Stdout = channel
							command.Stderr = channel.Stderr()
							runErr := command.Run()
							status := uint32(0)
							if runErr != nil {
								var exitError *exec.ExitError
								if errors.As(runErr, &exitError) {
									status = uint32(exitError.ExitCode())
								} else {
									status = 255
								}
							}
							data := make([]byte, 4)
							binary.BigEndian.PutUint32(data, status)
							_, _ = channel.SendRequest("exit-status", false, data)
							return
						}
					}()
				}
			}()
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestSequentialCommandsStopAtFailedExit(t *testing.T) {
	client := startTestSSH(t)
	run, err := storage.NewRun(t.TempDir(), "repo", "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	log, err := run.OpenJob("commands")
	if err != nil {
		t.Fatal(err)
	}
	var console bytes.Buffer
	e := &Executor{Client: client, Workspace: t.TempDir(), Console: &console, CommandTimeout: 5 * time.Second}
	err = e.Run(context.Background(), "commands",
		[]string{"echo first", "exit 7", "echo should-not-run"}, false, log)
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("expected exit status 7, got %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(console.String(), "[commands] first") {
		t.Fatalf("missing streamed stdout: %s", console.String())
	}
	data, err := io.ReadAll(strings.NewReader(console.String()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "should-not-run") {
		t.Fatal("a later command executed after failure")
	}
}

func TestAllowFailureContinues(t *testing.T) {
	client := startTestSSH(t)
	run, err := storage.NewRun(t.TempDir(), "repo", "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	log, err := run.OpenJob("commands")
	if err != nil {
		t.Fatal(err)
	}
	var console bytes.Buffer
	e := &Executor{Client: client, Workspace: t.TempDir(), Console: &console, CommandTimeout: 5 * time.Second}
	if err := e.Run(context.Background(), "commands",
		[]string{"exit 9", "echo reached"}, true, log); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(console.String(), "reached") {
		t.Fatalf("command after allowed failure did not run: %s", console.String())
	}
}

func TestLongAndUnterminatedLines(t *testing.T) {
	var lines []string
	large := strings.Repeat("x", 70*1024)
	if err := streamLines(strings.NewReader(large+"\nend"), func(line string) error {
		lines = append(lines, line)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != large || lines[1] != "end" {
		t.Fatal("line streaming truncated output")
	}
}
