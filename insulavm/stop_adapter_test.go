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
)

// This is an integration of the REAL Engine, SQLite and subprocess-based
// Libvirt adapter. Only the virsh executable is replaced by a deterministic
// shell shim. Unlike a pure mock it verifies that "shutdown" precedes
// "destroy", the owned VM's overlay is removed, and SQLite is updated.
func simulatedLibvirtEngine(t *testing.T) (*Engine,string,string,string) {
 t.Helper()
 e:=testEngine(t,"ssh:\n  sudo_mode: passwordless\n")
 const id="019a5d11-1234-7123-8123-123456789abc"
 dir:=filepath.Join(e.config.Storage.Runs,"insula-vm-force-test")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"overlay.qcow2"),[]byte("test-overlay"),0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{t.Fatal(err)}
 err=store.Insert(context.Background(),registry.Entry{
  ID:id,LibvirtUUID:id,Name:"insula-force-test",Profile:"debian",
  MAC:"52:54:00:aa:bb:cc",SSHUser:"insula",SudoMode:"passwordless",
  InstanceDir:dir,
 })
 if err!=nil{_ = store.Close();t.Fatal(err)}
 if err:=store.SetRunning(context.Background(),id,"127.0.0.1");err!=nil{_ = store.Close();t.Fatal(err)}
 _=store.Close()
 bin:=t.TempDir()
 calls:=filepath.Join(bin,"virsh-commands.log")
 script:=`#!/bin/sh
case "$3" in
 list) printf '%s\n' "$MOCK_UUID" ;;
 domstate) echo running ;;
 shutdown) echo "shutdown $4 $5 $6" >> "$MOCK_CALLS" ;;
 destroy) echo "destroy $4" >> "$MOCK_CALLS" ;;
 console) echo "CONSOLE_OK" ;;
 *) echo "unexpected virsh operation: $3" >&2; exit 97 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("MOCK_UUID",id)
 t.Setenv("MOCK_CALLS",calls)
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 return e,id,dir,calls
}

func TestStopVMWithOptionsRealAdapterEscalatesAfterDeadline(t *testing.T) {
 e,id,dir,calls:=simulatedLibvirtEngine(t)
 start:=time.Now()
 result,err:=e.StopVMWithOptions(context.Background(),VMStopOptions{
  VMID:id,GracePeriod:60*time.Millisecond,ForceAfterTimeout:true,
 })
 if err!=nil{t.Fatalf("forced shutdown failed: %v",err)}
 if result==nil||!result.TimedOut||!result.Forced||result.Graceful{
  t.Fatalf("wrong escalation result: %+v",result)
 }
 if elapsed:=time.Since(start);elapsed<55*time.Millisecond||elapsed>3*time.Second{
  t.Fatalf("escalation did not obey configured timeout: %s",elapsed)
 }
 contents,err:=os.ReadFile(calls)
 if err!=nil{t.Fatal(err)}
 commands:=strings.Split(strings.TrimSpace(string(contents)),"\n")
 if len(commands)!=2||commands[0]!="shutdown "+id+" --mode acpi"||commands[1]!="destroy insula-force-test"{
  t.Fatalf("wrong command ordering/ownership: %q",contents)
 }
 if _,err:=os.Stat(dir);!errors.Is(err,os.ErrNotExist){
  t.Fatalf("overlay should be removed after destroy: %v",err)
 }
 record,err:=e.GetVM(context.Background(),id)
 if err!=nil{t.Fatal(err)}
 if record.Status!="destroyed"{t.Fatalf("SQLite not marked destroyed: %+v",record)}
}

func TestStopVMWithOptionsTimeoutDoesNotDestroyWhenDisabled(t *testing.T){
 e,id,dir,calls:=simulatedLibvirtEngine(t)
 result,err:=e.StopVMWithOptions(context.Background(),VMStopOptions{
  VMID:id,GracePeriod:40*time.Millisecond,ForceAfterTimeout:false,
 })
 if !errors.Is(err,context.DeadlineExceeded)||result==nil||result.Forced{
  t.Fatalf("expected timeout without force: result=%+v err=%v",result,err)
 }
 content,err:=os.ReadFile(calls)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(content),"destroy"){
  t.Fatalf("disabled force destroyed VM: %q",content)
 }
 if _,err:=os.Stat(dir);err!=nil{t.Fatalf("VM overlay deleted without force: %v",err)}
}

func TestOpenConsoleRealAdapterForwardsStreams(t *testing.T){
 e,id,_,_:=simulatedLibvirtEngine(t)
 var stdout,stderr bytes.Buffer
 err:=e.OpenConsole(context.Background(),id,strings.NewReader("input\n"),&stdout,&stderr)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(stdout.String(),"CONSOLE_OK"){t.Fatalf("missing console output: %q",stdout.String())}
 if stderr.Len()!=0{t.Fatalf("unexpected console stderr: %q",stderr.String())}
 if err:=e.OpenConsole(context.Background(),"unknown",nil,nil,nil);err==nil{
  t.Fatal("allowed console access for unregistered VM")
 }
}
