package insulavm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func testEngine(t *testing.T, sshConfig string) *Engine {
	t.Helper()
	root := t.TempDir()
	filename := filepath.Join(root, "config.yaml")
	src := "storage:\n  root: ./data\nvirtualization:\n  provider: kvm\n  default_disk_gb: 8\n" + sshConfig
	if err := os.WriteFile(filename, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := New(filename)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPublicLibraryProfiles(t *testing.T) {
	engine := testEngine(t, "")
	source := filepath.Join(t.TempDir(), "installed.qcow2")
	if err := os.WriteFile(source, []byte{'Q', 'F', 'I', 0xfb}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := engine.RegisterProfile(Profile{
		Name: "debian-go",
		MemoryMiB: 1024,
		VCPUs: 1,
	}, source); err != nil {
		t.Fatal(err)
	}
	names, err := engine.ListProfiles()
	if err != nil || len(names) != 1 || names[0] != "debian-go" {
		t.Fatalf("list: %v, %v", names, err)
	}
	p, err := engine.GetProfile("debian-go")
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "v001" || p.DiskGB != 8 {
		t.Fatalf("unexpected registered profile: %+v", p)
	}
	if engine.Configuration().Storage.Profiles == "" {
		t.Fatal("missing profiles root")
	}
}

func TestRunInMemoryRejectsInvalidManifest(t *testing.T) {
	engine := testEngine(t, "")
	result, err := engine.Run(context.Background(), RunRequest{Manifest: &Manifest{
		Repository: Repository{Name: "../escape"},
		Action: "ci",
		Workspace: "/tmp",
		Setup: []string{"pwd"},
	}})
	if err == nil || result != nil {
		t.Fatalf("expected invalid manifest, got result=%+v err=%v", result, err)
	}
	_, err = engine.Run(context.Background(), RunRequest{ManifestPath:"m.yaml", Manifest:&Manifest{}})
	if err == nil {
		t.Fatal("mixed manifest inputs accepted")
	}
}

func TestRunCancellation(t *testing.T) {
	engine := testEngine(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := engine.Run(ctx, RunRequest{Manifest: &Manifest{
		Repository: Repository{Name: "repo"},
		Action: "ci",
		Workspace: "/tmp",
		Setup: []string{"pwd"},
	}, Host: "127.0.0.1:22"})
	if !errors.Is(err, context.Canceled) || result == nil {
		t.Fatalf("expected cancellation and partial result, got result=%+v err=%v", result, err)
	}
}

func runTestSSHServer(t *testing.T) (address, keyPath, hostsPath string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	server := &ssh.ServerConfig{NoClientAuth: true}
	server.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				conn, chans, requests, err := ssh.NewServerConn(raw, server)
				if err != nil {
					_ = raw.Close()
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range chans {
					if incoming.ChannelType() != "session" {
						_ = incoming.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, reqs, err := incoming.Accept()
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
							// The client sends "sh -s" and the script on stdin.
							process := exec.Command("sh", "-s")
							process.Stdin = channel
							process.Stdout = channel
							process.Stderr = channel.Stderr()
							err := process.Run()
							status := uint32(0)
							if err != nil {
								var exit *exec.ExitError
								if errors.As(err, &exit) {
									status = uint32(exit.ExitCode())
								} else {
									status = 255
								}
							}
							payload := make([]byte, 4)
							binary.BigEndian.PutUint32(payload, status)
							_, _ = channel.SendRequest("exit-status", false, payload)
							return
						}
					}()
				}
			}()
		}
	}()
	keyBytes, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	keyPath = filepath.Join(path, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type:"PRIVATE KEY", Bytes:keyBytes}), 0600); err != nil {
		t.Fatal(err)
	}
	hostsPath = filepath.Join(path, "known_hosts")
	_ = public
	if err := os.WriteFile(hostsPath, []byte(knownhosts.Line(
		[]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return listener.Addr().String(), keyPath, hostsPath
}

func TestRunInMemoryThroughSSH(t *testing.T) {
	host, key, hosts := runTestSSHServer(t)
	engine := testEngine(t, "")
	var output bytes.Buffer
	m := &Manifest{
		Repository: Repository{Name: "test-project"},
		Action: "ci",
		Workspace: t.TempDir(),
		Setup: []string{"echo prepared"},
		Pipelines: []Pipeline{{Name:"checks", Jobs:[]Job{{
			Name: "test",
			Commands: []string{"echo first", "echo second", "echo >&2 stderr-line"},
		}}}},
	}
	result, err := engine.Run(context.Background(), RunRequest{
		Manifest:m, Host:host, User:"test", PrivateKey:key,
		KnownHosts:hosts, Output:&output,
		CommandTimeout:5*time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || len(result.CompletedJobs) != 2 || result.CompletedJobs[1] != "test" {
		t.Fatalf("incomplete result: %+v", result)
	}
	if result.LogDirectory == "" || result.FinishedAt.Before(result.StartedAt) {
		t.Fatalf("invalid result metadata: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(result.LogDirectory, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, want := range []string{">> cd '", "&& echo first", "<< first", "&& echo second", "<< second", "<< stderr-line"} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in log: %s", want, log)
		}
	}
	if !strings.Contains(output.String(), "[test] first") {
		t.Fatalf("no streaming output: %s", output.String())
	}
}

func TestRunFailureDoesNotExecuteFollowingCommands(t *testing.T) {
	host, key, hosts := runTestSSHServer(t)
	engine := testEngine(t, "")
	m := &Manifest{
		Repository: Repository{Name:"repo"},
		Action:"ci",
		Workspace:t.TempDir(),
		Pipelines:[]Pipeline{{Name:"validate",Jobs:[]Job{{
			Name:"fail", Commands:[]string{"echo before", "exit 7", "echo should-not-run"},
		}}}},
	}
	result, err := engine.Run(context.Background(), RunRequest{
		Manifest:m,Host:host,User:"test",PrivateKey:key,KnownHosts:hosts,
		CommandTimeout:5*time.Second,
	})
	if err == nil || result == nil || len(result.CompletedJobs) != 0 {
		t.Fatalf("expected partial result and error; result=%+v err=%v", result, err)
	}
	content, readErr := os.ReadFile(filepath.Join(result.LogDirectory,"fail.log"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(content),"should-not-run") {
		t.Fatalf("later command was sent: %s", content)
	}
	if !strings.Contains(string(content), "<< before") {
		t.Fatalf("missing early output: %s", content)
	}
	_ = fmt.Sprint(io.Discard) // compile-time import sanity for client example
}
