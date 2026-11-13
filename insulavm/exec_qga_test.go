package insulavm

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahhvahh/InsulaVM/pkg/registry"
	vmtransport "github.com/ahhvahh/InsulaVM/pkg/vm"
)

func installQGAMock(t *testing.T, id, guestStatus string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$3\" in\n" +
		" list) printf '%s\\n' '" + id + "' ;;\n" +
		" domstate) printf '%s\\n' running ;;\n" +
		" qemu-agent-command) case \"$7\" in\n" +
		"  *guest-exec-status*) printf '%s\\n' '" + guestStatus + "' ;;\n" +
		"  *guest-exec*) printf '%s\\n' '{\"return\":{\"pid\":733}}' ;;\n" +
		"  *) exit 12 ;; esac ;;\n" +
		" *) exit 13 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "virsh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func registerQGANoIPVM(t *testing.T, engine *Engine, id string) {
	t.Helper()
	store, err := registry.Open(engine.config.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := registry.Entry{
		ID: id, LibvirtUUID: id, Name: "insula-qga-test-" + id, Profile: "missing-ssh-profile",
		MAC: "52:54:00:00:00:33", SSHPort: 22, InstanceDir: t.TempDir(), SudoMode: "disabled",
	}
	if err := store.Insert(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRunning(context.Background(), id, ""); err != nil {
		t.Fatal(err)
	}
}

func TestExecVMQGAWithoutIPOrSSH(t *testing.T) {
	engine := testEngine(t, "")
	id := "019a5d11-1234-7123-8123-123456789abc"
	registerQGANoIPVM(t, engine, id)
	installQGAMock(t, id, `{"return":{"exited":true,"exitcode":0,"out-data":"cm9vdAo=","err-data":"d2Fybgo="}}`)

	var stdout, stderr bytes.Buffer
	result, err := engine.ExecVM(context.Background(), ExecRequest{
		VMID: id, Command: "whoami", QGA: true, Output: &stdout, ErrorOutput: &stderr,
		CommandTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("QGA execution without IP or SSH failed: %v", err)
	}
	if stdout.String() != "root\n" || stderr.String() != "warn\n" ||
		string(result.Stdout) != "root\n" || string(result.Stderr) != "warn\n" ||
		result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("invalid QGA output: %+v; out=%q err=%q", result, &stdout, &stderr)
	}
	logged, err := os.ReadFile(filepath.Join(result.LogDirectory, "command.log"))
	if err != nil || !strings.Contains(string(logged), "whoami") ||
		!strings.Contains(string(logged), "root") {
		t.Fatalf("QGA execution log not preserved: %q %v", logged, err)
	}
}

func TestExecVMQGASecurityAndExitErrors(t *testing.T) {
	engine := testEngine(t, "")
	id := "019a5d11-1234-7123-8123-123456789abd"
	registerQGANoIPVM(t, engine, id)
	installQGAMock(t, id, `{"return":{"exited":true,"exitcode":23,"out-data":"ZmFpbAo="}}`)

	for _, tc := range []struct {
		name string
		req ExecRequest
		want string
	}{
		{"sudo rejected", ExecRequest{VMID:id,Command:"id",QGA:true,Sudo:true}, "sudo is not supported"},
		{"relative workspace", ExecRequest{VMID:id,Command:"pwd",QGA:true,Workspace:"tmp"}, "absolute guest path"},
		{"bad workspace", ExecRequest{VMID:id,Command:"pwd",QGA:true,Workspace:"/tmp\ninvalid"}, "absolute guest path"},
		{"negative timeout", ExecRequest{VMID:id,Command:"pwd",QGA:true,CommandTimeout:-time.Second}, "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := engine.ExecVM(context.Background(), tc.req)
			if err == nil || res != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid request not rejected: %+v %v", res, err)
			}
		})
	}
	res, err := engine.ExecVM(context.Background(), ExecRequest{VMID:id,Command:"false",QGA:true})
	if res == nil || res.ExitCode == nil || *res.ExitCode != 23 || err == nil {
		t.Fatalf("QGA failure must return exit code and logs: %+v %v", res, err)
	}
	var remote *vmtransport.QGAExitError
	if !errors.As(err, &remote) || remote.Code != 23 {
		t.Fatalf("typed remote exit error lost: %v", err)
	}
}


func TestExecVMRejectsOverlappingCommandsWithTypedError(t *testing.T) {
	for _, tc := range []struct {
		name string
		qga  bool
	}{
		{"QGA", true},
		{"SSH", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := testEngine(t, "")
			id := "019a5d11-1234-7123-8123-123456789abc"
			registerQGANoIPVM(t, engine, id)
			installQGAMock(t, id, `{"return":{"exited":true,"exitcode":0}}`)

			unlock, err := acquireVMExecLock(engine.config.Storage.Database, id)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()

			result, err := engine.ExecVM(context.Background(), ExecRequest{
				VMID: id, Command: "whoami", QGA: tc.qga,
			})
			if result != nil || !errors.Is(err, ErrVMExecBusy) ||
				!strings.Contains(err.Error(), id) {
				t.Fatalf("concurrent %s exec should be rejected before guest execution: result=%+v err=%v", tc.name, result, err)
			}
		})
	}
}
