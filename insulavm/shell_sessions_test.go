package insulavm

import (
 shelltransport "github.com/ahhvahh/InsulaVM/internal/shell"
 "context"
 "strings"
 "testing"
 "time"
)

func TestShellAPIValidatesRequestsBeforeHypervisor(t *testing.T){
 var absent *Engine
 if _,err:=absent.ExecuteShellCommand(context.Background(),ShellCommandRequest{VMID:"id",Command:"ls"});err==nil{t.Fatal("nil engine accepted")}
 engine:=&Engine{}
 checks:=[]struct{name string;call func()error}{
  {"nil context",func()error{_,err:=engine.ExecuteShellCommand(nil,ShellCommandRequest{VMID:"id",Command:"ls"});return err}},
  {"missing VM ID",func()error{_,err:=engine.ExecuteShellCommand(context.Background(),ShellCommandRequest{Command:"ls"});return err}},
  {"missing command",func()error{_,err:=engine.ExecuteShellCommand(context.Background(),ShellCommandRequest{VMID:"vm"});return err}},
  {"command NUL",func()error{_,err:=engine.ExecuteShellCommand(context.Background(),ShellCommandRequest{VMID:"vm",Command:"ls\x00id"});return err}},
  {"command too long",func()error{_,err:=engine.ExecuteShellCommand(context.Background(),ShellCommandRequest{VMID:"vm",Command:strings.Repeat("x",shelltransport.MaxCommandBytes+1)});return err}},
  {"invalid timeout",func()error{_,err:=engine.ExecuteShellCommand(context.Background(),ShellCommandRequest{VMID:"vm",Command:"ls",CommandTimeout:-time.Second});return err}},
  {"history missing ID",func()error{_,err:=engine.ShellCommandHistory(context.Background(),"");return err}},
  {"VM history missing ID",func()error{_,err:=engine.ShellSessionsForVM(context.Background(),"");return err}},
  {"close nil context",func()error{return engine.CloseShellSession(nil,"shell")}},
  {"close unknown ID",func()error{return engine.CloseShellSession(context.Background(),"unknown")}},
 }
 for _,tc:=range checks{
  t.Run(tc.name,func(t *testing.T){if err:=tc.call();err==nil{t.Fatal("invalid request accepted")}})
 }
 if err:=engine.Close();err!=nil{t.Fatal(err)}
 if err:=engine.Close();err!=nil{t.Fatalf("Engine.Close must be idempotent: %v",err)}
}
