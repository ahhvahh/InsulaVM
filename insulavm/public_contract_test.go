package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "testing"
)

// The public-method contract matrix exercises the entry point of every
// exported Engine method. It does NOT claim that preflight checks replace
// the hypervisor/SSH end-to-end tests in scripts/tests/integration.sh.
func TestPublicEngineMethodContractMatrix(t *testing.T) {
 e:=testEngine(t,"ssh:\n  sudo_mode: passwordless\n")
 ctx:=context.Background()
 if _,err:=New(filepath.Join(t.TempDir(),"no-config.yaml"));err==nil{t.Fatal("New accepted missing configuration")}
 if got:=e.Configuration();got.Storage.Root==""||got.Guest.Destination==""{t.Fatalf("Configuration returned invalid defaults: %+v",got)}
 source:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb,0,0,0,0},0600);err!=nil{t.Fatal(err)}
 if err:=e.RegisterProfile(Profile{Name:"base",MemoryMiB:1024,VCPUs:1,DiskGB:8},source);err!=nil{t.Fatal(err)}
 if _,err:=e.GetProfile("base");err!=nil{t.Fatal(err)}
 names,err:=e.ListProfiles()
 if err!=nil||len(names)!=1||names[0]!="base"{t.Fatalf("ListProfiles: %v %v",names,err)}
 if _,err:=e.CloneProfile(ctx,"base","copy");err!=nil{t.Fatalf("CloneProfile: %v",err)}
 if _,err:=e.GetProfile("copy");err!=nil{t.Fatalf("GetProfile: %v",err)}
 if _,err:=e.ListVMs(ctx);err!=nil{t.Fatal(err)}
 statuses,err:=e.ListVMStatus(ctx)
 if err!=nil||len(statuses)!=0{t.Fatalf("ListVMStatus: %+v %v",statuses,err)}
 dry,err:=e.StopAllVMs(ctx,VMStopAllOptions{DryRun:true})
 if err!=nil||dry.Checked!=0{t.Fatalf("StopAllVMs: %+v %v",dry,err)}
 checks:=[]struct{name string;call func()error}{
  {"StartVM",func()error{_,err:=e.StartVM(ctx,"not-found");return err}},
  {"StartVMWithProgress",func()error{_,err:=e.StartVMWithProgress(ctx,"not-found",nil);return err}},
  {"StartVMWithOptions",func()error{_,err:=e.StartVMWithOptions(ctx,VMStartOptions{Profile:"not-found"},nil);return err}},
  {"StartVMFromISO",func()error{_,err:=e.StartVMFromISO(ctx,LiveISOStartOptions{Profile:"not-found"});return err}},
  {"ExportProfileLiveISO",func()error{_,err:=e.ExportProfileLiveISO(ctx,"not-found");return err}},
  {"GetVM",func()error{_,err:=e.GetVM(ctx,"not-found");return err}},
  {"GetVMStatus",func()error{_,err:=e.GetVMStatus(ctx,"not-found");return err}},
  {"StopVM",func()error{return e.StopVM(ctx,"not-found")}},
  {"StopVMWithOptions",func()error{_,err:=e.StopVMWithOptions(ctx,VMStopOptions{});return err}},
  {"OpenConsole",func()error{return e.OpenConsole(ctx,"",nil,nil,nil)}},
  {"ExecVM",func()error{_,err:=e.ExecVM(ctx,ExecRequest{});return err}},
  {"ImportPath",func()error{_,err:=e.ImportPath(ctx,ImportRequest{});return err}},
  {"ImportToVM",func()error{_,err:=e.ImportToVM(ctx,VMImportRequest{});return err}},
  {"DeployToVM",func()error{_,err:=e.DeployToVM(ctx,VMDeployRequest{});return err}},
  {"FreezeVM",func()error{_,err:=e.FreezeVM(ctx,VMFreezeRequest{});return err}},
  {"CloneProfile rejects missing",func()error{_,err:=e.CloneProfile(ctx,"not-found","another");return err}},
  {"Run",func()error{_,err:=e.Run(ctx,RunRequest{});return err}},
 }
 for _,tc:=range checks{
  t.Run(tc.name,func(t *testing.T){if err:=tc.call();err==nil{t.Fatal("method accepted an invalid request")}})
 }
}

func TestPublicEngineNilContextContract(t *testing.T) {
 e:=testEngine(t,"")
 checks:=[]struct{name string;call func()error}{
  {"StartVM",func()error{_,err:=e.StartVM(nil,"invalid");return err}},
  {"StartVMWithProgress",func()error{_,err:=e.StartVMWithProgress(nil,"invalid",nil);return err}},
  {"StartVMWithOptions",func()error{_,err:=e.StartVMWithOptions(nil,VMStartOptions{},nil);return err}},
  {"StartVMFromISO",func()error{_,err:=e.StartVMFromISO(nil,LiveISOStartOptions{});return err}},
  {"GetVM",func()error{_,err:=e.GetVM(nil,"any");return err}},
  {"GetVMStatus",func()error{_,err:=e.GetVMStatus(nil,"any");return err}},
  {"ListVMs",func()error{_,err:=e.ListVMs(nil);return err}},
  {"ListVMStatus",func()error{_,err:=e.ListVMStatus(nil);return err}},
  {"ExecVM",func()error{_,err:=e.ExecVM(nil,ExecRequest{});return err}},
  {"ImportPath",func()error{_,err:=e.ImportPath(nil,ImportRequest{});return err}},
  {"ImportToVM",func()error{_,err:=e.ImportToVM(nil,VMImportRequest{});return err}},
  {"DeployToVM",func()error{_,err:=e.DeployToVM(nil,VMDeployRequest{});return err}},
  {"FreezeVM",func()error{_,err:=e.FreezeVM(nil,VMFreezeRequest{});return err}},
  {"StopVM",func()error{return e.StopVM(nil,"any")}},
  {"StopAllVMs",func()error{_,err:=e.StopAllVMs(nil,VMStopAllOptions{});return err}},
  {"StopVMWithOptions",func()error{_,err:=e.StopVMWithOptions(nil,VMStopOptions{VMID:"any"});return err}},
  {"OpenConsole",func()error{return e.OpenConsole(nil,"any",nil,nil,nil)}},
  {"CloneProfile",func()error{_,err:=e.CloneProfile(nil,"a","b");return err}},
  {"ExportProfileLiveISO",func()error{_,err:=e.ExportProfileLiveISO(nil,"bad");return err}},
 }
 for _,tc:=range checks{
  t.Run(tc.name,func(t *testing.T){if err:=tc.call();err==nil{t.Fatal("method accepted nil context")}})
 }
}
