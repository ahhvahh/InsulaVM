package insulavm

import (
	"bytes"
	"context"
	"errors"
 "io"
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
	if result.LogDirectory != "" {
		t.Fatalf("QGA must not create a command log directory: %q", result.LogDirectory)
	}
	if _, err := os.Stat(engine.config.Storage.Logs); !os.IsNotExist(err) {
		t.Fatalf("QGA must not write captured output to storage.logs: %v", err)
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
		t.Fatalf("QGA failure must return exit code and captured bytes: %+v %v", res, err)
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

func TestExecVMQGABinaryNativeBytesWithoutLog(t *testing.T) {
 engine := testEngine(t, "")
 id := "019a5d11-1234-7123-8123-123456789abe"
 registerQGANoIPVM(t, engine, id)
 installQGAMock(t, id, `{"return":{"exited":true,"exitcode":0,"out-data":"AP9PSwo=","err-data":"RQBS"}}`)
 wantOut := []byte{0, 255, 'O', 'K', '\n'}
 wantErr := []byte{'E', 0, 'R'}
 var stdout, stderr bytes.Buffer
 result, err := engine.ExecVM(context.Background(), ExecRequest{
  VMID: id, Command: "binary", QGA: true, Output: &stdout, ErrorOutput: &stderr,
 })
 if err != nil {t.Fatal(err)}
 if !bytes.Equal(result.Stdout,wantOut) || !bytes.Equal(result.Stderr,wantErr) ||
  !bytes.Equal(stdout.Bytes(),wantOut)||!bytes.Equal(stderr.Bytes(),wantErr) {
  t.Fatalf("QGA modified native bytes: result=%+v stdout=%v stderr=%v",result,stdout.Bytes(),stderr.Bytes())
 }
 if result.LogDirectory!="" {t.Fatalf("unexpected QGA log path: %q",result.LogDirectory)}
 if _,err:=os.Stat(engine.config.Storage.Logs);!os.IsNotExist(err) {
  t.Fatalf("QGA persisted binary output: %v",err)
 }
 // Providing no writers must still return the same bytes.
 result,err=engine.ExecVM(context.Background(),ExecRequest{VMID:id,Command:"binary",QGA:true})
 if err!=nil||!bytes.Equal(result.Stdout,wantOut)||!bytes.Equal(result.Stderr,wantErr){
  t.Fatalf("nil writers lost captured bytes: %+v %v",result,err)
 }
}

type shortQGAWriter struct{}
func (shortQGAWriter)Write(p []byte)(int,error){return 0,nil}

func TestExecVMQGAWriterShortWritePreservesResult(t *testing.T){
 engine:=testEngine(t,"")
 id:="019a5d11-1234-7123-8123-123456789abf"
 registerQGANoIPVM(t,engine,id)
 installQGAMock(t,id,`{"return":{"exited":true,"exitcode":0,"out-data":"cm9vdAo="}}`)
 result,err:=engine.ExecVM(context.Background(),ExecRequest{VMID:id,Command:"whoami",QGA:true,Output:shortQGAWriter{}})
 if !errors.Is(err,io.ErrShortWrite)||result==nil||string(result.Stdout)!="root\n"{
  t.Fatalf("writer short write must surface error without losing response: %+v %v",result,err)
 }
 if result.LogDirectory!=""{t.Fatalf("unexpected log path: %q",result.LogDirectory)}
}

func TestExecVMQGAOptInLogsOutputWithoutChangingWriters(t *testing.T){
 engine:=testEngine(t,"")
 id:="019a5d11-1234-7123-8123-123456789abb"
 registerQGANoIPVM(t,engine,id)
 installQGAMock(t,id,`{"return":{"exited":true,"exitcode":0,"out-data":"AP9PSwo=","err-data":"RQBS"}}`)
 var stdout,stderr bytes.Buffer
 result,err:=engine.ExecVM(context.Background(),ExecRequest{
  VMID:id,Command:"echo binary",QGA:true,Log:true,Output:&stdout,ErrorOutput:&stderr,
 })
 if err!=nil{t.Fatal(err)}
 if result==nil||result.LogDirectory==""{t.Fatalf("missing opt-in QGA log: %+v",result)}
 if !bytes.Equal(stdout.Bytes(),result.Stdout)||!bytes.Equal(stderr.Bytes(),result.Stderr){
  t.Fatalf("logging changed process output: %+v out=%v err=%v",result,stdout.Bytes(),stderr.Bytes())
 }
 content,err:=os.ReadFile(filepath.Join(result.LogDirectory,"command.log"))
 if err!=nil{t.Fatal(err)}
 for _,part:=range []string{"echo binary","stdout (base64): AP9PSwo=","stderr (base64): RQBS","exit_code: 0"}{
  if !strings.Contains(string(content),part){t.Fatalf("missing %q in opt-in log: %s",part,content)}
 }
}
