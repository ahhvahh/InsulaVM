//go:build integration

package integration

import (
 "bytes"
 "context"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/ahhvahh/InsulaVM/insulavm"
)

// This suite is opt-in and executes real Libvirt/QEMU guests. It cannot run
// on ordinary GitHub Actions runners without nested KVM access.
func TestPublicGoAPIOnRealDebianVM(t *testing.T) {
 configPath:=os.Getenv("INSULA_TEST_CONFIG")
 imagePath:=os.Getenv("INSULA_TEST_QCOW2")
 if configPath==""||imagePath==""{t.Fatal("INSULA_TEST_CONFIG and INSULA_TEST_QCOW2 are required")}
 if _,err:=os.Stat(imagePath);err!=nil{t.Fatal(err)}
 engine,err:=insulavm.New(configPath)
 if err!=nil{t.Fatal(err)}
 tag:=fmt.Sprintf("go-api-it-%d",time.Now().UnixNano())
 base:=tag+"-base"
 clone:=tag+"-clone"
 frozen:=tag+"-frozen"
 if err:=engine.RegisterProfile(insulavm.Profile{Name:base,OS:"debian",MemoryMiB:2048,VCPUs:2,DiskGB:8},imagePath);err!=nil{t.Fatal(err)}
 list,err:=engine.ListProfiles()
 if err!=nil||!contains(list,base){t.Fatalf("registered base missing: %v %v",list,err)}
 profile,err:=engine.GetProfile(base)
 if err!=nil||profile.Name!=base{t.Fatalf("GetProfile: %+v %v",profile,err)}
 copy,err:=engine.CloneProfile(context.Background(),base,clone)
 if err!=nil||copy.Name!=clone{t.Fatalf("CloneProfile: %+v %v",copy,err)}
 ctx,cancel:=context.WithTimeout(context.Background(),40*time.Minute)
 defer cancel()
 var startEvents []insulavm.VMProgress
 vm,err:=engine.StartVMWithOptions(ctx,insulavm.VMStartOptions{Profile:clone},func(p insulavm.VMProgress){
  startEvents=append(startEvents,p)
 })
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){
  cleanupCtx,done:=context.WithTimeout(context.Background(),45*time.Second)
  defer done()
  _,_=engine.StopVMWithOptions(cleanupCtx,insulavm.VMStopOptions{VMID:vm.ID,GracePeriod:5*time.Second,ForceAfterTimeout:true})
 })
 if len(startEvents)==0{t.Fatal("StartVMWithOptions produced no progress events")}
 meta,err:=engine.GetVM(ctx,vm.ID)
 if err!=nil||meta.ID!=vm.ID{t.Fatalf("GetVM: %+v %v",meta,err)}
 live,err:=engine.GetVMStatus(ctx,vm.ID)
 if err!=nil||!live.IsRunning(){t.Fatalf("GetVMStatus: %+v %v",live,err)}
 all,err:=engine.ListVMs(ctx)
 if err!=nil||len(all)==0{t.Fatalf("ListVMs: %+v %v",all,err)}
 allLive,err:=engine.ListVMStatus(ctx)
 if err!=nil||len(allLive)==0{t.Fatalf("ListVMStatus: %+v %v",allLive,err)}
 planned,err:=engine.StopAllVMs(ctx,insulavm.VMStopAllOptions{DryRun:true})
 if err!=nil||!contains(planned.Targets,vm.ID){t.Fatalf("StopAllVMs dry-run: %+v %v",planned,err)}
 var stdout bytes.Buffer
 executed,err:=engine.ExecVM(ctx,insulavm.ExecRequest{VMID:vm.ID,Command:"echo INSULA_GO_API_OK",Output:&stdout})
 if err!=nil||executed.VMID!=vm.ID||!strings.Contains(stdout.String(),"INSULA_GO_API_OK"){
  t.Fatalf("ExecVM: %+v %v output=%q",executed,err,stdout.String())
 }
 workspace:=t.TempDir()
 payload:=filepath.Join(workspace,"api-probe.txt")
 if err:=os.WriteFile(payload,[]byte("test-payload"),0600);err!=nil{t.Fatal(err)}
 uploaded,err:=engine.ImportToVM(ctx,insulavm.VMImportRequest{VMID:vm.ID,SourcePath:payload})
 if err!=nil||!uploaded.Imported{t.Fatalf("ImportToVM: %+v %v",uploaded,err)}
 script:=filepath.Join(workspace,"probe.sh")
 if err:=os.WriteFile(script,[]byte("#!/bin/sh\nset -eu\ntest -s api-probe.txt\necho INSULA_DEPLOY_API_OK\n"),0600);err!=nil{t.Fatal(err)}
 var deployOutput bytes.Buffer
 deployment,err:=engine.DeployToVM(ctx,insulavm.VMDeployRequest{VMID:vm.ID,SourcePath:payload,ScriptPath:script,Output:&deployOutput})
 if err!=nil||!deployment.ScriptSucceeded{
  t.Fatalf("DeployToVM: %+v %v output=%s",deployment,err,deployOutput.String())
 }
 if !strings.Contains(deployOutput.String(),"INSULA_DEPLOY_API_OK"){t.Fatalf("remote deploy did not report marker: %q",deployOutput.String())}
 stopped,err:=engine.StopVMWithOptions(ctx,insulavm.VMStopOptions{VMID:vm.ID,GracePeriod:30*time.Second,ForceAfterTimeout:true})
 if err!=nil||stopped==nil{t.Fatalf("StopVMWithOptions: %+v %v",stopped,err)}
 after,err:=engine.GetVMStatus(ctx,vm.ID)
 if err!=nil||after.LibvirtState!="absent"{t.Fatalf("VM still running: %+v %v",after,err)}
 t.Log("Go API profile/clone/start/status/exec/import/deploy/stop: OK")
 // A second guest is used for FreezeVM because freeze necessarily shuts
 // down and consumes its source VM.
 var freezeEvents []insulavm.VMProgress
 source,err:=engine.StartVMWithProgress(ctx,base,func(p insulavm.VMProgress){freezeEvents=append(freezeEvents,p)})
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){
  cleanupCtx,done:=context.WithTimeout(context.Background(),45*time.Second)
  defer done()
  _=engine.StopVM(cleanupCtx,source.ID)
 })
 if len(freezeEvents)==0{t.Fatal("StartVMWithProgress returned no events")}
 liveISO:=os.Getenv("INSULA_TEST_LIVE_ISO")=="1"
 if liveISO{
  var prep bytes.Buffer
  _,err=engine.ExecVM(ctx,insulavm.ExecRequest{
   VMID:source.ID,Sudo:true,
   Command:"DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends live-boot && update-initramfs -u && test -d /usr/share/initramfs-tools/scripts/live && echo INSULA_LIVE_BOOT_READY",
   CommandTimeout:25*time.Minute,Output:&prep,
  })
  if err!=nil||!strings.Contains(prep.String(),"INSULA_LIVE_BOOT_READY"){t.Fatalf("prepare live-boot: %v %s",err,prep.String())}
 }
 frozenResult,err:=engine.FreezeVM(ctx,insulavm.VMFreezeRequest{VMID:source.ID,Name:frozen,ShutdownTimeout:3*time.Minute})
 if err!=nil||frozenResult==nil||!frozenResult.SourceStopped||!frozenResult.SourceCleaned||
  frozenResult.ForcedShutdown||frozenResult.ImagePath==""{t.Fatalf("FreezeVM: %+v %v",frozenResult,err)}
 // A second real KVM guest must boot using the compressed independent QCOW2.
 restored,err:=engine.StartVMWithOptions(ctx,insulavm.VMStartOptions{Profile:frozen},nil)
 if err!=nil{t.Fatalf("boot second VM from frozen compressed QCOW2: %v",err)}
 t.Cleanup(func(){
  cleanupCtx,done:=context.WithTimeout(context.Background(),45*time.Second)
  defer done()
  _,_=engine.StopVMWithOptions(cleanupCtx,insulavm.VMStopOptions{VMID:restored.ID,GracePeriod:5*time.Second,ForceAfterTimeout:true})
 })
 var restoredOutput bytes.Buffer
 _,err=engine.ExecVM(ctx,insulavm.ExecRequest{
  VMID:restored.ID,Command:"test -f /etc/debian_version && echo INSULA_FROZEN_QCOW2_BOOT_OK",Output:&restoredOutput,
 })
 if err!=nil||!strings.Contains(restoredOutput.String(),"INSULA_FROZEN_QCOW2_BOOT_OK"){
  t.Fatalf("restored QCOW2 guest validation: %v %s",err,restoredOutput.String())
 }
 restoredStop,err:=engine.StopVMWithOptions(ctx,insulavm.VMStopOptions{
  VMID:restored.ID,GracePeriod:50*time.Second,ForceAfterTimeout:false,
 })
 if err!=nil||restoredStop==nil||!restoredStop.Graceful||restoredStop.Forced{
  t.Fatalf("restored guest did not stop gracefully: %+v %v",restoredStop,err)
 }
 t.Log("INSULA_GO_API_QCOW2_RESTORE_OK")
 if !liveISO{t.Log("ISO Live scenario disabled by default; opt in via INSULA_TEST_LIVE_ISO=1");return}
 iso,err:=engine.ExportProfileLiveISO(ctx,frozen)
 if err!=nil{t.Fatalf("ExportProfileLiveISO: %v",err)}
 if info,err:=os.Stat(iso);err!=nil||info.Size()==0{t.Fatalf("ISO file absent: %s %v",iso,err)}
 isoVM,err:=engine.StartVMFromISO(ctx,insulavm.LiveISOStartOptions{Profile:frozen,ISOPath:iso})
 if err!=nil{t.Fatalf("StartVMFromISO: %v",err)}
 t.Cleanup(func(){
  cleanupCtx,done:=context.WithTimeout(context.Background(),45*time.Second)
  defer done()
  _,_=engine.StopVMWithOptions(cleanupCtx,insulavm.VMStopOptions{VMID:isoVM.ID,GracePeriod:5*time.Second,ForceAfterTimeout:true})
 })
 var isoOutput bytes.Buffer
 _,err=engine.ExecVM(ctx,insulavm.ExecRequest{VMID:isoVM.ID,Command:"echo INSULA_GO_ISO_OK",Output:&isoOutput})
 if err!=nil||!strings.Contains(isoOutput.String(),"INSULA_GO_ISO_OK"){t.Fatalf("ISO boot+command: %v %s",err,isoOutput.String())}
 _,err=engine.StopVMWithOptions(ctx,insulavm.VMStopOptions{VMID:isoVM.ID,GracePeriod:30*time.Second,ForceAfterTimeout:true})
 if err!=nil{t.Fatal(err)}
 t.Log("INSULA_GO_API_INTEGRATION_OK")
}

func contains(values []string,target string)bool{
 for _,value:=range values{if value==target{return true}}
 return false
}


// TestCreateProfileFromDebianInstallerISO verifies the newly exposed Go API
// with real Debian Installer media. It runs only when an installer ISO is
// deliberately provided to scripts/tests/integration.sh.
func TestCreateProfileFromDebianInstallerISO(t *testing.T){
 configPath:=os.Getenv("INSULA_TEST_CONFIG")
 installerISO:=os.Getenv("INSULA_TEST_INSTALLER_ISO")
 if installerISO==""{t.Skip("provide --installer-iso to run the real Debian installer test")}
 if configPath==""{t.Fatal("INSULA_TEST_CONFIG required")}
 engine,err:=insulavm.New(configPath)
 if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithTimeout(context.Background(),115*time.Minute)
 defer cancel()
 name:=fmt.Sprintf("go-api-iso-%d",time.Now().UnixNano())
 result,err:=engine.CreateProfileFromISO(ctx,insulavm.CreateProfileFromISORequest{
  Name:name,ISOPath:installerISO,MemoryMiB:2048,VCPUs:2,DiskGB:12,
  Timeout:90*time.Minute,
  OnProgress:func(p insulavm.ProfileISOProgress){t.Logf("Installer [%s] %s",p.Stage,p.Message)},
 })
 if err!=nil{t.Fatal(err)}
 if !result.Installed || result.Profile.Name!=name{t.Fatalf("installer did not create a reusable profile: %+v",result)}
 instance,err:=engine.StartVMWithOptions(ctx,insulavm.VMStartOptions{Profile:name},nil)
 if err!=nil{t.Fatalf("boot newly installed profile: %v",err)}
 t.Cleanup(func(){
  cleanupCtx,done:=context.WithTimeout(context.Background(),45*time.Second)
  defer done()
  _,_=engine.StopVMWithOptions(cleanupCtx,insulavm.VMStopOptions{VMID:instance.ID,GracePeriod:5*time.Second,ForceAfterTimeout:true})
 })
 var output bytes.Buffer
 _,err=engine.ExecVM(ctx,insulavm.ExecRequest{VMID:instance.ID,Command:"test -f /etc/debian_version && echo INSULA_INSTALLER_API_OK",Output:&output})
 if err!=nil||!strings.Contains(output.String(),"INSULA_INSTALLER_API_OK"){t.Fatalf("new Debian VM failed: %v %s",err,output.String())}
 if os.Getenv("INSULA_TEST_LIVE_ISO")!="1"{
  _,err=engine.StopVMWithOptions(ctx,insulavm.VMStopOptions{VMID:instance.ID,GracePeriod:30*time.Second,ForceAfterTimeout:true})
  if err!=nil{t.Fatal(err)}
  return
 }
 exported,err:=engine.ExportVMToLiveISO(ctx,insulavm.VMExportISORequest{
  VMID:instance.ID,Name:name+"-frozen",
  PrepareLiveBoot:true,CommandTimeout:30*time.Minute,ShutdownTimeout:3*time.Minute,
 })
 if err!=nil{t.Fatalf("export VM directly to Live ISO: %+v %v",exported,err)}
 stat,err:=os.Stat(exported.ISOPath)
 if err!=nil||stat.Size()==0{t.Fatalf("Live ISO missing after freezing VM: %v",err)}
 live,err:=engine.StartVMFromISO(ctx,insulavm.LiveISOStartOptions{Profile:name+"-frozen",ISOPath:exported.ISOPath})
 if err!=nil{t.Fatalf("ISO could not boot: %v",err)}
 t.Cleanup(func(){
  cleanupCtx,done:=context.WithTimeout(context.Background(),45*time.Second)
  defer done()
  _,_=engine.StopVMWithOptions(cleanupCtx,insulavm.VMStopOptions{VMID:live.ID,GracePeriod:5*time.Second,ForceAfterTimeout:true})
 })
 var liveOutput bytes.Buffer
 _,err=engine.ExecVM(ctx,insulavm.ExecRequest{VMID:live.ID,Command:"echo INSULA_ISO_FROM_INSTALLER_OK",Output:&liveOutput})
 if err!=nil||!strings.Contains(liveOutput.String(),"INSULA_ISO_FROM_INSTALLER_OK"){
  t.Fatalf("Live ISO guest validation: %v %s",err,liveOutput.String())
 }
 if _,err:=engine.StopVMWithOptions(ctx,insulavm.VMStopOptions{VMID:live.ID,GracePeriod:30*time.Second,ForceAfterTimeout:true});err!=nil{t.Fatal(err)}
 t.Log("INSULA_INSTALLER_ISO_INTEGRATION_OK")
}
