package main

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/insulavm"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func cliStoppedVMForFreeze(t *testing.T)(string,*insulavm.Engine,string){
 t.Helper()
 config,_:=cliConfigFixture(t)
 engine,err:=insulavm.New(config)
 if err!=nil{t.Fatal(err)}
 original:=filepath.Join(t.TempDir(),"original.qcow2")
 if err:=os.WriteFile(original,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
 if err:=engine.RegisterProfile(insulavm.Profile{
  Name:"installed",MemoryMiB:1024,VCPUs:1,DiskGB:8,
 },original);err!=nil{t.Fatal(err)}
 cfg:=engine.Configuration()
 sourceDir:=filepath.Join(cfg.Storage.Runs,"insula-vm-"+cliLoopbackID)
 if err:=os.MkdirAll(sourceDir,0700);err!=nil{t.Fatal(err)}
 overlay:=filepath.Join(sourceDir,"overlay.qcow2")
 if err:=os.WriteFile(overlay,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 entry:=registry.Entry{
  ID:cliLoopbackID,LibvirtUUID:cliLoopbackID,Name:"insula-installed",
  Profile:"installed",MAC:"52:54:00:00:33:44",
  SSHUser:"insula",SudoMode:"disabled",InstanceDir:sourceDir,
 }
 if err:=store.Insert(context.Background(),entry);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(context.Background(),entry.ID,"192.0.2.2");err!=nil{t.Fatal(err)}
 if err:=store.Close();err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 tools:=map[string]string{
  "virsh":"case \"$3\" in list) exit 0 ;; *) echo unexpected-virsh >&2; exit 12 ;; esac",
  "qemu-img":"case \"$1\" in\n"+
   "convert) for last; do :; done; printf 'QFI\\373standalone' > \"$last\" ;;\n"+
   "info) printf '{\"format\":\"qcow2\"}\\n' ;;\n"+
   "*) exit 11 ;; esac",
  "guestmount":"for last; do :; done\nmkdir -p \"$last/boot\"\n"+
   "printf kernel > \"$last/boot/vmlinuz-6.12-test\"\n"+
   "printf initrd > \"$last/boot/initrd.img-6.12-test\"",
  "guestunmount":"exit 0",
  "lsinitramfs":"echo scripts/live/boot",
  "mksquashfs":"printf compressed-root > \"$2\"",
  "grub-mkrescue":"printf generated-iso > \"$2\"",
 }
 for name,body:=range tools{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte("#!/bin/sh\nset -eu\n"+body+"\n"),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 return config,engine,sourceDir
}

func TestCLIRecoversStoppedQCOW2WithoutGuestMutation(t *testing.T){
 config,engine,sourceDir:=cliStoppedVMForFreeze(t)
 err:=run([]string{"vm","freeze","-config",config,"-id",cliLoopbackID,
  "-name","reusable","-export-iso=false","-skip-guest-cleanup","-recover-stopped"})
 if err!=nil{t.Fatalf("CLI freeze recovery failed: %v",err)}
 profile,err:=engine.GetProfile("reusable")
 if err!=nil{t.Fatal(err)}
 saved,err:=profile.ImagePath()
 if err!=nil||saved==""{t.Fatalf("standalone QCOW2 missing: %q %v",saved,err)}
 if _,err:=os.Stat(sourceDir);!os.IsNotExist(err){t.Fatalf("source disk was not cleaned: %v",err)}
 record,err:=engine.GetVM(context.Background(),cliLoopbackID)
 if err!=nil||record.Status!="destroyed"{t.Fatalf("source history incorrect: %+v %v",record,err)}
}

func TestCLIExportsISOFromFrozenRecoveryWithoutReinstall(t *testing.T){
 config,engine,sourceDir:=cliStoppedVMForFreeze(t)
 destination:=filepath.Join(t.TempDir(),"live-boot.iso")
 args:=[]string{"vm","export-iso","-config",config,"-id",cliLoopbackID,
  "-name","live-recovery","-output",destination,
  "-prepare-live-boot=false","-skip-guest-cleanup","-recover-stopped"}
 if err:=run(args);err!=nil{t.Fatalf("VM CLI freeze+ISO export failed: %v",err)}
 data,err:=os.ReadFile(destination)
 if err!=nil||string(data)!="generated-iso"{t.Fatalf("ISO output was not preserved: %q %v",data,err)}
 if _,err:=engine.GetProfile("live-recovery");err!=nil{t.Fatalf("reusable profile missing: %v",err)}
 if _,err:=os.Stat(sourceDir);!os.IsNotExist(err){t.Fatalf("source overlay not removed: %v",err)}
}

func TestCLIRejectsUnsafeFreezeRecoverySwitches(t *testing.T){
 cfg,_,sourceDir:=cliStoppedVMForFreeze(t)
 attempts:=[][]string{
  {"vm","freeze","-config",cfg,"-id",cliLoopbackID,"-name","unsafe",
   "-export-iso=true","-prepare-live-boot=true","-recover-stopped","-skip-guest-cleanup"},
  {"vm","freeze","-config",cfg,"-id",cliLoopbackID,"-name","unsafe",
   "-prepare-live-boot=false","-export-iso=false"},
  {"vm","freeze","-config",cfg,"-id",cliLoopbackID,"-name","unsafe",
   "-skip-guest-cleanup","-recover-stopped","-output","not-an-iso.txt"},
  {"vm","export-iso","-config",cfg,"-id",cliLoopbackID,"-name","unsafe",
   "-skip-guest-cleanup","-recover-stopped"},
 }
 for _,args:=range attempts{
  err:=run(args)
  if err==nil{t.Fatalf("unsafe CLI freeze request succeeded: %v",args)}
  if _,err:=os.Stat(sourceDir);err!=nil{t.Fatalf("invalid freeze removed source: %v",err)}
 }
 _ = strings.Contains
}
