package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

// This matrix exercises every public CLI command without a hypervisor.
// Successful stateful commands use a private test configuration. VM commands
// are checked for argument validation and missing/unsafe managed targets;
// actual guest execution is tested by scripts/tests/integration.sh.
func TestCLICommandContractMatrix(t *testing.T) {
 root:=t.TempDir()
 cfg:=filepath.Join(root,"config.yaml")
 contents:="storage:\n  root: "+filepath.Join(root,"state")+"\nvirtualization:\n  provider: kvm\n"
 if err:=os.WriteFile(cfg,[]byte(contents),0600);err!=nil{t.Fatal(err)}
 source:=filepath.Join(root,"debian.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb,0,0,0,0},0600);err!=nil{t.Fatal(err)}
 options:=[]struct{name string;args []string;wantError bool;message string}{
  {"help",[]string{"help"},false,""},
  {"config check",[]string{"config","check","-config",cfg},false,""},
  {"config rejects unknown operation",[]string{"config","bad","-config",cfg},true,"usage"},
  {"profile register",[]string{"profile","register","-config",cfg,"-name","debian","-image",source,"-memory-mib","1024","-vcpus","1","-disk-gb","8"},false,""},
  {"profile list",[]string{"profile","list","-config",cfg},false,""},
  {"profile show",[]string{"profile","show","-config",cfg,"-name","debian"},false,""},
  {"profile clone",[]string{"profile","clone","-config",cfg,"-source","debian","-name","copy"},false,""},
  {"profile clone duplicate",[]string{"profile","clone","-config",cfg,"-source","debian","-name","copy"},true,""},
  {"profile export-iso missing profile",[]string{"profile","export-iso","-config",cfg,"-name","missing"},true,""},
  {"profile create-from-iso requires ISO",[]string{"profile","create-from-iso","-config",cfg,"-name","new"},true,"requires"},
  {"profile create-from-iso invalid installer",[]string{"profile","create-from-iso","-config",cfg,"-name","new","-iso",source},true,"ISO"},
  {"profile export-iso requires name",[]string{"profile","export-iso","-config",cfg},true,"requires"},
  {"profile bad operation",[]string{"profile","unknown","-config",cfg},true,"expected"},
  {"vm list empty",[]string{"vm","list","-config",cfg},false,""},
  {"vm stop-all dry-run empty",[]string{"vm","stop-all","-config",cfg,"-dry-run"},false,""},
  {"vm stop-all forbids id",[]string{"vm","stop-all","-config",cfg,"-id","foreign"},true,"does not accept"},
  {"vm start requires profile",[]string{"vm","start","-config",cfg},true,"requires"},
  {"vm start-iso requires profile",[]string{"vm","start-iso","-config",cfg},true,"requires"},
  {"vm start-iso rejects absent ISO",[]string{"vm","start-iso","-config",cfg,"-profile","debian"},true,"ISO"},
  {"vm console requires id",[]string{"vm","console","-config",cfg},true,"requires"},
  {"vm console unknown UUID",[]string{"vm","console","-config",cfg,"-id","unknown"},true,""},
  {"vm status requires id",[]string{"vm","status","-config",cfg},true,"requires"},
  {"vm status unknown UUID",[]string{"vm","status","-config",cfg,"-id","unknown"},true,""},
  {"vm diagnose requires id",[]string{"vm","diagnose","-config",cfg},true,"requires"},
  {"vm diagnose unknown UUID",[]string{"vm","diagnose","-config",cfg,"-id","unknown"},true,""},
  {"vm shell requires id",[]string{"vm","shell","-config",cfg},true,""},
  {"vm exec requires command",[]string{"vm","exec","-config",cfg,"-id","unknown"},true,"requires"},
  {"vm import requires source",[]string{"vm","import","-config",cfg,"-id","unknown"},true,"requires"},
  {"vm deploy requires script",[]string{"vm","deploy","-config",cfg,"-id","unknown","-source",source},true,"requires"},
  {"vm export-iso requires name",[]string{"vm","export-iso","-config",cfg,"-id","unknown"},true,"requires"},
  {"vm export-iso unknown VM",[]string{"vm","export-iso","-config",cfg,"-id","unknown","-name","export"},true,""},
  {"vm freeze requires name",[]string{"vm","freeze","-config",cfg,"-id","unknown"},true,"requires"},
  {"vm stop requires id",[]string{"vm","stop","-config",cfg},true,"requires"},
  {"vm stop unknown UUID",[]string{"vm","stop","-config",cfg,"-id","unknown"},true,""},
  {"vm invalid timeout",[]string{"vm","stop","-config",cfg,"-id","unknown","-grace-timeout","invalid"},true,""},
  {"vm unknown operation",[]string{"vm","unknown","-config",cfg},true,"usage"},
  {"run validation of timeout",[]string{"run","-config",cfg,"-command-timeout","0s"},true,"positive"},
  {"import requires host and source",[]string{"import","-config",cfg},true,"requires"},
  {"unknown top-level command",[]string{"unexpected"},true,"usage"},
 }
 for _,tc:=range options{
  t.Run(tc.name,func(t *testing.T){
   err:=run(tc.args)
   if tc.wantError && err==nil{t.Fatal("expected error but CLI succeeded")}
   if !tc.wantError && err!=nil{t.Fatalf("CLI returned unexpected error: %v",err)}
   if tc.message!="" && (err==nil||!strings.Contains(strings.ToLower(err.Error()),strings.ToLower(tc.message))){
    t.Fatalf("error %v does not contain %q",err,tc.message)
   }
  })
 }
}
