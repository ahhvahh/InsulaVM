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
 // Explicit recovery controls. Never force shutdown or skip credential cleanup by default.
 ForceShutdown bool
 SkipGuestCleanup bool
 // AllowStopped safely captures a stopped owned disk (Libvirt absent or
 // shut off) only when the operator explicitly opts in. Cloud-Init profiles
 // additionally require SkipGuestCleanup; installer disks do not.
 AllowStopped bool
 // ExportISO creates a Debian Live ISO after publishing the QCOW2.
 // The QCOW2 always survives if ISO creation fails.
 ExportISO bool
 // DisableQCOW2Compression opts out of the native compressed QCOW2 base.
 // By default, FreezeVM converts the stopped overlay with qemu-img convert -c.
 DisableQCOW2Compression bool
 // OutputISOPath is an optional absolute or CWD-relative .iso destination.
 // Empty preserves the managed profile/exports location.
 OutputISOPath string
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
 ForcedShutdown bool
 ISOPath string
}

// FreezeVM captures a reusable QCOW2 base, not a bootable ISO.
// Cloud-Init VMs require managed SSH and passwordless sudo for cleanup.
// Installer-ISO VMs use installer.qcow2 without guest identity cleanup.
// It gracefully shuts down the transient domain,
// flattens its overlay, registers the new profile, and removes source data.
// If the export fails, the original overlay is intentionally retained.
func(e *Engine)FreezeVM(ctx context.Context,req VMFreezeRequest)(*VMFreezeResult,error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 if strings.TrimSpace(req.VMID)==""{return nil,errors.New("VM ID is required")}
 if err:=storage.ValidateName(req.Name);err!=nil{return nil,fmt.Errorf("invalid freeze name: %w",err)}
 if req.OutputISOPath!=""&&!req.ExportISO{return nil,errors.New("output ISO path requires ExportISO=true")}
 // Validate destination BEFORE touching guest identity, requesting shutdown,
 // or publishing a disk profile.
 if req.ExportISO&&req.OutputISOPath!=""{
  if _,err:=e.liveISODestination(req.Name,req.OutputISOPath);err!=nil{return nil,err}
 }
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
 sourceProfile,sourceDisk,installerISO,err:=e.resolveFreezeSource(record.Profile)
 if err!=nil{return result,err}
 // An already-stopped VM may be recovered only by explicit operator opt-in.
 // A powered-off defined domain is safe to read; no QEMU disk writer remains.
 // Guest cleanup must have already completed for Cloud-Init profiles.
 running:=record.Status=="running"&&checked.IsRunning()
 stopped:=record.Status=="running"&&(checked.LibvirtState=="absent"||checked.LibvirtState=="shut off")
 if !running && !(stopped&&req.AllowStopped&&(installerISO||req.SkipGuestCleanup)){
  return result,fmt.Errorf("VM must be running or explicitly recoverable (SQLite=%s, Libvirt=%s); use -recover-stopped for an installed ISO disk, plus -skip-guest-cleanup for a managed Cloud-Init VM",record.Status,checked.LibvirtState)
 }
 if !installerISO && !req.SkipGuestCleanup && record.SudoMode!="passwordless"{
  return result,errors.New("freeze requires passwordless sudo for cloud-init identity cleanup")
 }
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
 overlay:=filepath.Join(sourceDir,sourceDisk)
 stat,err:=os.Lstat(overlay)
 if err!=nil{return result,fmt.Errorf("read VM overlay: %w",err)}
 if !stat.Mode().IsRegular(){return result,errors.New("VM overlay is not a regular file")}
 namePath:=filepath.Join(e.config.Storage.Profiles,req.Name)
 if _,err:=os.Lstat(namePath);err==nil{
  return result,fmt.Errorf("frozen profile %q already exists; choose another name",req.Name)
 }else if !errors.Is(err,os.ErrNotExist){return result,err}

 if installerISO{
  notify("clean","Installer disk: no Cloud-Init/SSH identity cleanup; guest machine-id, SSH host keys and application secrets are preserved. Sanitize before reuse when needed")
 }else if !req.SkipGuestCleanup{
  notify("clean","Resetting cloud-init and per-instance SSH identity before capture")
  // The existing SSH connection remains alive long enough to finish cleanup.
  // On retry after a shutdown failure, an operator must set SkipGuestCleanup.
  guestCmd:="set -eu; command -v cloud-init >/dev/null; "+
   // The guest is asked to power off itself even on older Libvirt domains
   // with broken ACPI/agent wiring. systemd-run schedules the request and
   // returns while the SSH command can still finish.
   "if command -v systemd-run >/dev/null 2>&1; then "+
   "systemd-run --collect --on-active=30s /usr/bin/systemctl poweroff --no-wall >/dev/null 2>&1 || :; fi; "+
   "home_dir=$(getent passwd "+quoteGuest(record.SSHUser)+" | cut -d: -f6); "+
   "test -n \"$home_dir\"; cloud-init clean --logs --machine-id --seed; "+
   "rm -f -- \"$home_dir/.ssh/authorized_keys\"; rm -f /etc/ssh/ssh_host_*"
  _,err=e.ExecVM(ctx,ExecRequest{
   VMID:req.VMID,Command:guestCmd,Sudo:true,CommandTimeout:2*time.Minute,
  })
  if err!=nil{return result,fmt.Errorf("prepare guest for cloning: %w",err)}
 }else{
  notify("clean","Guest cleanup skipped by explicit recovery option (verify identity was cleaned previously)")
 }
 if running{
  notify("shutdown","Requesting graceful VM shutdown; original disk preserved on failure")
  var forced bool
  var shutdownErr error
  if installerISO{
   forced,shutdownErr=vm.ShutdownInstallerForFreeze(ctx,e.config.Virtualization.LibvirtURI,record.LibvirtUUID,req.ShutdownTimeout,req.ForceShutdown,func(message string){notify("shutdown",message)})
  }else{
   forced,shutdownErr=vm.ShutdownForFreezeWithOptions(ctx,e.config.Virtualization.LibvirtURI,record.LibvirtUUID,req.ShutdownTimeout,req.ForceShutdown)
  }
  result.ForcedShutdown=forced
  if shutdownErr!=nil{return result,shutdownErr}
  if forced{notify("shutdown","WARNING: forced power-off; captured disk may need filesystem recovery")}
 }else{
  notify("shutdown","VM already stopped in Libvirt ("+checked.LibvirtState+"); recovering intact disk")
 }
 result.SourceStopped=true

 notify("convert","Flattening QCOW2 backing chain into a named independent image")
 frozenProfile:=sourceProfile
 if frozenProfile.Guest.Destination=="" {frozenProfile.Guest.Destination=e.config.Guest.Destination}
 if !installerISO {frozenProfile.SSH=profile.SSH{User:record.SSHUser}}
 image,err:=profile.FreezeFromOverlayWithOptions(ctx,e.config.Storage.Profiles,frozenProfile,req.Name,overlay,profile.FreezeOptions{Compress:!req.DisableQCOW2Compression})
 if err!=nil{return result,fmt.Errorf("create frozen profile: %w (original overlay preserved at %s)",err,sourceDir)}
 result.ImagePath=image
 notify("registered","Reusable profile created: "+req.Name)

 // Keep the source overlay until BOTH the independent QCOW2 is saved and
 // the requested ISO has been published. On ISO failure, the operator can
 // retry from the saved profile without losing the original source overlay.
 if req.ExportISO{
  notify("iso","Mapping the stopped QCOW2 disk and exporting Debian Live ISO")
  iso,isoErr:=e.ExportProfileLiveISOTo(ctx,req.Name,req.OutputISOPath)
  if isoErr!=nil{
   return result,fmt.Errorf("QCOW2 profile %s saved; Live ISO export failed; source overlay preserved; retry: insula profile export-iso -name %s: %w",req.Name,req.Name,isoErr)
  }
  result.ISOPath=iso
 }
 notify("cleanup","Removing transient VM only after the requested artifacts are saved")
 if err:=e.StopVM(ctx,req.VMID);err!=nil{
  return result,fmt.Errorf("artifacts saved but source VM cleanup failed (source preserved): %w",err)
 }
 result.SourceCleaned=true
 notify("ready","Frozen QCOW2 profile ready: "+req.Name)

 return result,nil
}

 
// resolveFreezeSource supports both Cloud-Init overlays and disks created by
// vm install-iso. An installer profile is not a ready-to-boot profile: only
// after freeze has safely shut down and converted its installer.qcow2 disk
// do we publish it as a direct-boot reusable profile.
func(e *Engine)resolveFreezeSource(name string)(profile.Profile,string,bool,error){
 original,err:=e.GetProfile(name)
 if err==nil{return *original,"overlay.qcow2",false,nil}
 installer,installerErr:=profile.ReadInstaller(e.config.Storage.Profiles,name)
 if installerErr!=nil{
  return profile.Profile{},"",false,fmt.Errorf("load source profile %q: %w (installer metadata: %v)",name,err,installerErr)
 }
 frozen:=profile.Profile{
  Name:installer.Name,OS:installer.OS,
  MemoryMiB:installer.Hardware.RAM.MiB,VCPUs:installer.Hardware.CPU.Quantity,
  DiskGB:installer.Disk.SizeGB,BootMode:"direct",
  Workspace:profile.Workspace{Destination:installer.Workspace.Destination},
 }
 return frozen,"installer.qcow2",true,nil
}
