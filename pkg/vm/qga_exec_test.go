package vm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mockQGAVirsh(t *testing.T, response string) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "request.json")
	script := "#!/bin/sh\n" +
		"case \"$3\" in\n" +
		"qemu-agent-command)\n" +
		" case \"$7\" in\n" +
		" *guest-exec-status*) printf '%s\\n' '" + response + "' ;;\n" +
		" *guest-exec*) printf '%s' \"$7\" > \"$QGA_CAPTURE\"; printf '%s\\n' '{\"return\":{\"pid\":1234}}' ;;\n" +
		" *guest-ping*) printf '%s\\n' '{\"return\":{}}' ;;\n" +
		" *) exit 20 ;;\n" +
		" esac ;;\n" +
		"*) exit 21 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "virsh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QGA_CAPTURE", capture)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}

func TestExecuteQGAResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		wantStdout []byte
		wantStderr []byte
		wantExit   int
		wantError  string
	}{
		{
			name: "binary stdout and separate stderr",
			status: `{"return":{"exited":true,"exitcode":0,"out-data":"` + base64.StdEncoding.EncodeToString([]byte{'h', 'i', 0, '\n'}) + `","err-data":"d2Fybgo="}}`,
			wantStdout: []byte{'h', 'i', 0, '\n'},
			wantStderr: []byte("warn\n"),
			wantExit: 0,
		},
		{
			name: "nonzero exit retains output",
			status: `{"return":{"exited":true,"exitcode":7,"out-data":"aGVsbG8K"}}`,
			wantStdout: []byte("hello\n"), wantExit: 7, wantError: "exited with code 7",
		},
		{
			name: "truncated output is an error",
			status: `{"return":{"exited":true,"exitcode":0,"out-data":"aGk=","out-truncated":true}}`,
			wantStdout: []byte("hi"), wantExit: 0, wantError: "truncated",
		},
		{
			name: "signalled process",
			status: `{"return":{"exited":true,"signal":9}}`,
			wantError: "signal 9",
		},
		{
			name: "invalid base64",
			status: `{"return":{"exited":true,"exitcode":0,"out-data":"%%%%"}}`,
			wantExit: 0, wantError: "decode QGA stdout",
		},
		{
			name: "missing exitcode",
			status: `{"return":{"exited":true}}`,
			wantError: "without exit code",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			capture := mockQGAVirsh(t, tc.status)
			got, err := ExecuteQGA(context.Background(), "qemu:///system",
				"019a5d11-1234-7123-8123-123456789abc", "printf 'hello'; : # a \" quote",
				2*time.Second)
			if tc.wantError == "" && err != nil {
				t.Fatalf("unexpected QGA error: %v", err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("QGA error = %v, want %q", err, tc.wantError)
			}
			if string(got.Stdout) != string(tc.wantStdout) || string(got.Stderr) != string(tc.wantStderr) {
				t.Fatalf("stdout=%q stderr=%q", got.Stdout, got.Stderr)
			}
			if tc.wantExit != 0 || tc.wantStdout != nil {
				if got.ExitCode == nil || *got.ExitCode != tc.wantExit {
					t.Fatalf("exit code %v, want %d", got.ExitCode, tc.wantExit)
				}
			}
			reqBytes, readErr := os.ReadFile(capture)
			if readErr != nil {
				t.Fatalf("guest-exec request not captured: %v", readErr)
			}
			var req struct {
				Execute string `json:"execute"`
				Arguments struct {
					Path string   `json:"path"`
					Arg  []string `json:"arg"`
					CaptureOutput bool `json:"capture-output"`
				} `json:"arguments"`
			}
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				t.Fatal(err)
			}
			if req.Execute != "guest-exec" || req.Arguments.Path != "/bin/sh" ||
				len(req.Arguments.Arg) != 2 || req.Arguments.Arg[0] != "-c" ||
				req.Arguments.Arg[1] != "printf 'hello'; : # a \" quote" || !req.Arguments.CaptureOutput {
				t.Fatalf("unsafe or wrong guest-exec request: %+v", req)
			}
			var exitErr *QGAExitError
			if tc.wantExit == 7 && !errors.As(err, &exitErr) {
				t.Fatalf("nonzero exit must produce typed exit error: %v", err)
			}
		})
	}
}

func TestExecuteQGABasicValidation(t *testing.T) {
	cases := []struct {
		name string
		ctx context.Context
		uri, uuid, cmd string
		timeout time.Duration
	}{
		{"nil context", nil, "qemu:///system", "id", "id", time.Second},
		{"missing URI", context.Background(), "", "id", "id", time.Second},
		{"missing ID", context.Background(), "qemu:///system", "", "id", time.Second},
		{"blank command", context.Background(), "qemu:///system", "id", " ", time.Second},
		{"NUL", context.Background(), "qemu:///system", "id", "bad\x00cmd", time.Second},
		{"negative timeout", context.Background(), "qemu:///system", "id", "id", -time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ExecuteQGA(tc.ctx, tc.uri, tc.uuid, tc.cmd, tc.timeout); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestExecuteQGATimeoutDoesNotPretendToKillGuest(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$3\" = qemu-agent-command ]; then exec sleep 3; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "virsh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := ExecuteQGA(context.Background(), "qemu:///system", "test-id", "sleep 10", 25*time.Millisecond)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) ||
		!strings.Contains(err.Error(), "may continue") {
		t.Fatalf("timeout must preserve uncertainty about guest process: %v", err)
	}
}

func TestQGAPing(t *testing.T) {
	mockQGAVirsh(t, `{"return":{"exited":true,"exitcode":0}}`)
	if err := QGAPing(context.Background(), "qemu:///system", "test-id"); err != nil {
		t.Fatal(err)
	}
}
