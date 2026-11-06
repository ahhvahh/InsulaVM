package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestStopByIDRemovesStaleVMOnlyAfterCheckingLibvirt(t *testing.T){
 root:=t.TempDir()
 bin:=t.TempDir()
 script:="#!/bin/sh\n"+
  "if [ \"$3\" = \"list\" ]; then exit 0; fi\n"+
  "echo 'unexpected virsh call' >&2\nexit 7\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 cfg:=&config.Config{
  Storage:config.Storage{Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"db","state.sqlite")},
  Virtualization:config.Virtualization{LibvirtURI:"qemu:///system"},
 }
 runDir:=filepath.Join(cfg.Storage.Runs,"insula-vm-stale")
 if err:=os.MkdirAll(runDir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(runDir,"overlay.qcow2"),[]byte("test"),0600);err!=nil{t.Fatal(err)}
 s,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 const id="019a5d11-1234-7123-8123-123456789abc"
 if err:=s.Insert(context.Background(),registry.Entry{
  ID:id,LibvirtUUID:id,Name:"insula-stale",Profile:"debian13",MAC:"52:54:00:aa:bb:cc",
  SSHUser:"insula",InstanceDir:runDir,
 });err!=nil{t.Fatal(err)}
 _=s.Close()
 if err:=StopByID(context.Background(),cfg,id);err!=nil{t.Fatal(err)}
 if _,err:=os.Stat(runDir);!os.IsNotExist(err){t.Fatalf("expected removed stale VM storage: %v",err)}
 s,err=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 defer s.Close()
 entry,err:=s.Get(context.Background(),id)
 if err!=nil{t.Fatal(err)}
 if entry.Status!="destroyed"{t.Fatalf("expected destroyed, got %s",entry.Status)}
}

func TestStopByIDDoesNotDeleteOnLibvirtError(t *testing.T){
 root:=t.TempDir()
 bin:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte("#!/bin/sh\necho 'connection refused' >&2\nexit 13\n"),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 cfg:=&config.Config{
  Storage:config.Storage{Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"db","state.sqlite")},
  Virtualization:config.Virtualization{LibvirtURI:"qemu:///system"},
 }
 runDir:=filepath.Join(cfg.Storage.Runs,"insula-vm-safe")
 if err:=os.MkdirAll(runDir,0700);err!=nil{t.Fatal(err)}
 s,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 const id="019a5d11-1234-7123-8123-123456789abe"
 if err:=s.Insert(context.Background(),registry.Entry{
  ID:id,LibvirtUUID:id,Name:"insula-safe",Profile:"debian13",MAC:"52:54:00:aa:bb:cc",
  SSHUser:"insula",InstanceDir:runDir,
 });err!=nil{t.Fatal(err)}
 _=s.Close()
 err=StopByID(context.Background(),cfg,id)
 if err==nil||!strings.Contains(err.Error(),"connection refused"){t.Fatalf("expected Libvirt error: %v",err)}
 if _,err:=os.Stat(runDir);err!=nil{t.Fatalf("unsafe storage deletion on Libvirt error: %v",err)}
}

func TestStartWithProgressReportsProfileError(t *testing.T){
 var events []Progress
 p:=&profile.Profile{Name:"debian13",Version:"v001",Image:"base.qcow2",MemoryMiB:1024,VCPUs:1,DiskGB:8,Directory:t.TempDir()}
 instance,err:=StartWithProgress(context.Background(),&config.Config{},p,func(p Progress){events=append(events,p)})
 if err==nil||instance!=nil{t.Fatal("missing profile image should prevent startup")}
 if len(events)==0||events[0].Stage!="profile"{
  t.Fatalf("expected initial profile check event: %+v",events)
 }
}
