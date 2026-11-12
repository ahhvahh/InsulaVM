package main

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestCLIAndLibrarySharedVMSSHOperations(t *testing.T){
 cfg,workspace:=cliConfigFixture(t)
 address,key,known:=cliLocalSSH(t)
 engine:=cliRegisteredGuest(t,cfg,address,key,known)
 for _,args:=range [][]string{
  {"vm","list","-config",cfg},
  {"vm","status","-config",cfg,"-id",cliLoopbackID},
  {"vm","exec","-config",cfg,"-id",cliLoopbackID,"-command","printf cli-execution-success"},
  {"vm","diagnose","-config",cfg,"-id",cliLoopbackID},
  {"test","ssh","-config",cfg,"-id",cliLoopbackID},
 }{
  if err:=run(args);err!=nil{t.Fatalf("CLI %v returned %v",args,err)}
 }
 if _,err:=os.Stat(workspace);err!=nil{t.Fatalf("CLI Exec did not establish guest workspace: %v",err)}
 sourceFile:=filepath.Join(t.TempDir(),"payload.bin")
 if err:=os.WriteFile(sourceFile,[]byte("ssh-import-payload"),0600);err!=nil{t.Fatal(err)}
 if err:=run([]string{"vm","import","-config",cfg,"-id",cliLoopbackID,"-source",sourceFile});err!=nil{
  t.Fatalf("CLI import failed: %v",err)
 }
 payload,err:=os.ReadFile(filepath.Join(workspace,"payload.bin"))
 if err!=nil||string(payload)!="ssh-import-payload"{
  t.Fatalf("import was not persisted in guest workspace: %q %v",payload,err)
 }
 script:=filepath.Join(t.TempDir(),"post-load.sh")
 scriptText:="#!/bin/bash\nset -euo pipefail\ntest \"$1\" = payload.bin\ntest -f \"$1\"\nprintf 'deploy completed' > deploy-success.txt\n"
 if err:=os.WriteFile(script,[]byte(scriptText),0700);err!=nil{t.Fatal(err)}
 if err:=run([]string{"vm","deploy","-config",cfg,"-id",cliLoopbackID,"-source",sourceFile,"-script",script});err!=nil{
  t.Fatalf("CLI deploy failed: %v",err)
 }
 result,err:=os.ReadFile(filepath.Join(workspace,"deploy-success.txt"))
 if err!=nil||string(result)!="deploy completed"{t.Fatalf("deploy script did not execute: %q %v",result,err)}
 if err:=run([]string{"vm","stop","-config",cfg,"-id",cliLoopbackID,"-grace-timeout","2s"});err!=nil{
  t.Fatalf("CLI graceful stop failed: %v",err)
 }
 if _,err:=os.Stat(filepath.Join(engine.Configuration().Storage.Runs,"insula-vm-"+cliLoopbackID));!os.IsNotExist(err){
  t.Fatalf("source VM directory was not removed: %v",err)
 }
 record,err:=engine.GetVM(context.Background(),cliLoopbackID)
 if err!=nil||record.Status!="destroyed"{t.Fatalf("history mismatch after CLI stop: %+v %v",record,err)}
}

func TestCLIRunManifestUsesRealSSHExecution(t *testing.T){
 cfg,workspace:=cliConfigFixture(t)
 address,key,known:=cliLocalSSH(t)
 manifest:=filepath.Join(t.TempDir(),"insula.yaml")
 src:="repository:\n  name: sample\n"+
  "action: ci\nworkspace: "+workspace+"\nsetup:\n  - echo prepared\n"+
  "pipelines:\n  - name: checks\n    jobs:\n      - name: verify\n        commands:\n          - echo cli-run-success\n"
 if err:=os.WriteFile(manifest,[]byte(src),0600);err!=nil{t.Fatal(err)}
 args:=[]string{"run","-config",cfg,"-manifest",manifest,"-host",address,
  "-user","test","-key",key,"-known-hosts",known,"-command-timeout","10s"}
 if err:=run(args);err!=nil{t.Fatalf("real SSH runAction failed: %v",err)}
 for _,args:=range [][]string{
  {"run","-config",cfg,"-manifest",manifest,"-host",address,"-command-timeout","0s"},
  {"run","-config",cfg,"-manifest",manifest,"-host",address,"extra"},
  {"run","-config",cfg,"-manifest",filepath.Join(t.TempDir(),"missing.yaml"),"-host",address},
 }{
  if err:=run(args);err==nil{t.Fatalf("malformed CLI action accepted: %v",args)}
 }
 _ = time.Second
}

func TestCLIProfileCloneAndLiveISOExportViaNativeFileTools(t *testing.T){
 cfg,_:=cliConfigFixture(t)
 disk:=filepath.Join(t.TempDir(),"installed.qcow2")
 if err:=os.WriteFile(disk,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
 for _,args:=range [][]string{
  {"profile","register","-config",cfg,"-name","original","-image",disk,
   "-memory-mib","1024","-vcpus","1","-disk-gb","8"},
  {"profile","clone","-config",cfg,"-source","original","-name","copy"},
  {"profile","list","-config",cfg},
  {"profile","show","-config",cfg,"-name","copy"},
 }{
  if err:=run(args);err!=nil{t.Fatalf("CLI profile %v failed: %v",args,err)}
 }
 bin:=t.TempDir()
 scripts:=map[string]string{
  "guestmount":"for last; do :; done\nmkdir -p \"$last/boot\"\n"+
   "printf kernel > \"$last/boot/vmlinuz-6.12-test\"\n"+
   "printf initrd > \"$last/boot/initrd.img-6.12-test\"",
  "guestunmount":"exit 0",
  "lsinitramfs":"echo scripts/live",
  "mksquashfs":"printf squashfs > \"$2\"",
  "grub-mkrescue":"printf bootable-iso-for-unit-test > \"$2\"",
 }
 for name,body:=range scripts{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte("#!/bin/sh\nset -eu\n"+body+"\n"),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 iso:=filepath.Join(t.TempDir(),"snapshot.iso")
 args:=[]string{"profile","export-iso","-config",cfg,"-name","copy","-output",iso}
 if err:=run(args);err!=nil{t.Fatalf("CLI ISO export did not publish: %v",err)}
 output,err:=os.ReadFile(iso)
 if err!=nil||string(output)!="bootable-iso-for-unit-test"{t.Fatalf("ISO data mismatch: %q %v",output,err)}
 if err:=run(args);err==nil||!strings.Contains(err.Error(),"already exists"){
  t.Fatalf("CLI export clobber protection missing: %v",err)
 }
}
