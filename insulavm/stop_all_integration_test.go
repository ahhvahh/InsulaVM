package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestStopAllVMsUsesRegistryNotForeignLibvirtDomains(t *testing.T){
 engine:=testEngine(t,"")
 ctx:=context.Background()
 const managedID="01a11111-1111-7111-8111-111111111111"
 const foreignID="01afffff-ffff-7fff-8fff-ffffffffffff"
 dir:=filepath.Join(engine.config.Storage.Runs,"insula-vm-managed")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"overlay.qcow2"),[]byte("overlay"),0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(engine.config.Storage.Database)
 if err!=nil{t.Fatal(err)}
 if err:=store.Insert(ctx,registry.Entry{
  ID:managedID,LibvirtUUID:managedID,Name:"insula-debian-"+managedID[:8],
  Profile:"debian",MAC:"52:54:00:aa:bb:cc",SSHUser:"insula",InstanceDir:dir,
 });err!=nil{t.Fatal(err)}
 _=store.Close()

 bin:=t.TempDir()
 logFile:=filepath.Join(bin,"virsh.log")
 virshScript:="#!/bin/sh\n"+
  "case \"$3\" in\n"+
  "  list) printf '%s\\n' \"$MOCK_MANAGED_ID\" \"$MOCK_FOREIGN_ID\" ;;\n"+
  "  domstate) echo running ;;\n"+
  "  destroy) printf '%s\\n' \"$4\" >> \"$MOCK_VIRSH_LOG\" ;;\n"+
  "  *) echo unexpected >&2; exit 91 ;;\n"+
  "esac\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(virshScript),0700);err!=nil{t.Fatal(err)}
 t.Setenv("MOCK_MANAGED_ID",managedID)
 t.Setenv("MOCK_FOREIGN_ID",foreignID)
 t.Setenv("MOCK_VIRSH_LOG",logFile)
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))

 dry,err:=engine.StopAllVMs(ctx,VMStopAllOptions{DryRun:true})
 if err!=nil||len(dry.Targets)!=1||len(dry.Stopped)!=0{t.Fatalf("dry run: %+v err=%v",dry,err)}
 if _,err:=os.Stat(dir);err!=nil{t.Fatalf("dry-run removed storage: %v",err)}
 if _,err:=os.Stat(logFile);!os.IsNotExist(err){t.Fatalf("dry-run issued destroy: %v",err)}

 done,err:=engine.StopAllVMs(ctx,VMStopAllOptions{})
 if err!=nil{t.Fatal(err)}
 if len(done.Stopped)!=1||done.Stopped[0]!=managedID{t.Fatalf("unexpected stopped VMs: %+v",done)}
 calls,err:=os.ReadFile(logFile)
 if err!=nil{t.Fatal(err)}
 if strings.TrimSpace(string(calls))!="insula-debian-"+managedID[:8]{
  t.Fatalf("attempted to destroy unrelated VM: %q",calls)
 }
 if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("VM overlay not removed: %v",err)}
 store,err=registry.Open(engine.config.Storage.Database)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 saved,err:=store.Get(ctx,managedID)
 if err!=nil||saved.Status!="destroyed"{t.Fatalf("VM not marked destroyed: %+v err=%v",saved,err)}
}
