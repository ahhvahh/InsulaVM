package insulavm

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/storage"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// VMFreezeRequest turns an already configured, running VM into an immutable,
// named profile for future disposable instances. It intentionally shuts down
// and removes the source VM only AFTER a successful standalone image export.
type VMFreezeRequest struct {
 VMID string
 Name string
 ShutdownTimeout time.Duration
 OnProgress func(VMFreezeProgress)
}

type VMFreezeProgress struct {
 Stage string
 Message string
}

type VMFreezeResult struct {
 VMID string
 ProfileName string
 ImagePath string
 SourceStopped bool
 SourceCleaned bool
}

// FreezeVM captures a reusable QCOW2 base, not a bootable ISO.
// Requires a managed SSH VM with passwordless sudo and cloud-init installed.
// It cleans cloud-init identity, gracefully shuts down the transient domain,
// flattens its overlay, registers the new profile, and removes source data.
// If the export fails, the original overlay is intentionally retained.
func(e *Engine)FreezeVM(ctx context.Context,req VMFreezeRequest)(*VMFreezeResult,error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 if strings.TrimSpace(req.VMID)==""{return nil,errors.New("VM ID is required")}
 if err:=storage.ValidateName(req.Name);err!=nil{return nil,fmt.Errorf("invalid freeze name: %w",err)}
 if req.ShutdownTimeout<0{return nil,errors.New("shutdown timeout must not be negative")}
 if req.ShutdownTimeout==0{req.ShutdownTimeout=3*time.Minute}
 notify:=func(stage,msg string){
  if req.OnProgress!=nil{req.OnProgress(VMFreezeProgress{Stage:stage,Message:msg})}
 }
 result:=&VMFreezeResult{VMID:req.VMID,ProfileName:req.Name}

 checked,err:=e.GetVMStatus(ctx,req.VMID)
 if err!=nil{return result,err}
 record:=checked.Record
 if record.ID!=record.LibvirtUUID {
  return result,errors.New("registered VM UUID does not match Libvirt domain; refusing freeze")
 }
 if record.Status!="running"||!checked.IsRunning(){
  return result,fmt.Errorf("VM must be running (SQLite=%s, Libvirt=%s)",record.Status,checked.LibvirtState)
 }
 if record.SudoMode!="passwordless" {
  return result,errors.New("freeze requires passwordless sudo for cloud-init identity cleanup")
 }
 original,err:=e.GetProfile(record.Profile)
 if err!=nil{return result,fmt.Errorf("load source profile: %w",err)}
 // Confirm this is the InsulaVM-owned temporary overlay, not an arbitrary disk.
 root,err:=filepath.Abs(e.config.Storage.Runs)
 if err!=nil{return result,err}
 sourceDir,err:=filepath.Abs(record.InstanceDir)
 if err!=nil{return result,err}
 relative,err:=filepath.Rel(root,sourceDir)
 if err!=nil||relative=="."||relative==".."||strings.HasPrefix(relative,".."+string(os.PathSeparator))||
  !strings.HasPrefix(filepath.Base(sourceDir),"insula-vm-"){
  return result,errors.New("unsafe registered instance storage path")
 }
 overlay:=filepath.Join(sourceDir,"overlay.qcow2")
 stat,err:=os.Lstat(overlay)
 if err!=nil{return result,fmt.Errorf("read VM overlay: %w",err)}
 if !stat.Mode().IsRegular(){return result,errors.New("VM overlay is not a regular file")}
 namePath:=filepath.Join(e.config.Storage.Profiles,req.Name)
 if _,err:=os.Lstat(namePath);err==nil{
  return result,fmt.Errorf("frozen profile %q already exists; choose another name",req.Name)
 }else if !errors.Is(err,os.ErrNotExist){return result,err}

 notify("clean","Resetting cloud-init and per-instance SSH identity before capture")
 // The original SSH connection remains valid after authorized_keys removal;
 // the new per-instance NoCloud seed will install new keys on next boot.
 guestCmd:="set -eu; command -v cloud-init >/dev/null; "+
  "home_dir=$(getent passwd "+quoteGuest(record.SSHUser)+" | cut -d: -f6); "+
  "test -n \"$home_dir\"; cloud-init clean --logs --machine-id --seed; "+
  "rm -f -- \"$home_dir/.ssh/authorized_keys\"; rm -f /etc/ssh/ssh_host_*"
 _,err=e.ExecVM(ctx,ExecRequest{
  VMID:req.VMID,Command:guestCmd,Sudo:true,CommandTimeout:2*time.Minute,
 })
 if err!=nil{return result,fmt.Errorf("prepare guest for cloning: %w",err)}

 notify("shutdown","Requesting graceful shutdown; source overlay is preserved")
 if err:=vm.ShutdownForFreeze(ctx,e.config.Virtualization.LibvirtURI,record.LibvirtUUID,req.ShutdownTimeout);err!=nil{
  return result,err
 }
 result.SourceStopped=true

 notify("convert","Flattening QCOW2 backing chain into a named independent image")
 frozenProfile:=*original
 frozenProfile.SSH=profile.SSH{User:record.SSHUser}
 image,err:=profile.FreezeFromOverlay(ctx,e.config.Storage.Profiles,frozenProfile,req.Name,overlay)
 if err!=nil{return result,fmt.Errorf("create frozen profile: %w (original overlay preserved at %s)",err,sourceDir)}
 result.ImagePath=image
 notify("registered","Reusable profile created: "+req.Name)

 // Only remove the original instance AFTER profile.yaml and image are complete.
 notify("cleanup","Removing the original disposable VM")
 if err:=e.StopVM(ctx,req.VMID);err!=nil{
  return result,fmt.Errorf("profile %s is already saved, but source VM cleanup failed: %w",req.Name,err)
 }
 result.SourceCleaned=true
 notify("ready","Use insula vm start -profile "+req.Name)
 return result,nil
}
