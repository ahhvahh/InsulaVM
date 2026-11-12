package insulavm

import (
 "github.com/ahhvahh/InsulaVM/internal/liveiso"
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/storage"
)

// ExportProfileLiveISO builds a BIOS/UEFI Debian Live ISO from a saved QCOW2.
// Prerequisite: guest has live-boot installed and an initramfs regenerated
// with live-boot support before it was frozen. No arbitrary guest execution
// is performed by the host: libguestfs mounts the image read-only.
func(e *Engine)ExportProfileLiveISO(ctx context.Context,name string)(string,error){
 return e.ExportProfileLiveISOTo(ctx,name,"")
}

// ExportProfileLiveISOTo exports a saved QCOW2 as a Debian Live ISO.
// outputPath can be absolute or relative to the process working directory.
// An existing destination is never overwritten. Empty outputPath uses the
// profile's managed exports directory.
func(e *Engine)ExportProfileLiveISOTo(ctx context.Context,name,outputPath string)(isoPath string,runErr error){
 if e==nil{return "",errors.New("engine is required")}
 if ctx==nil{return "",errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return "",err}
 dest,err:=e.liveISODestination(name,outputPath)
 if err!=nil{return "",err}
 p,err:=e.GetProfile(name)
 if err!=nil{return "",err}
 image,err:=p.ImagePath()
 if err!=nil{return "",err}
 for _,tool:=range []string{"guestmount","guestunmount","mksquashfs","grub-mkrescue","lsinitramfs"}{
  if _,err:=exec.LookPath(tool);err!=nil{return "",fmt.Errorf("required host tool %s: %w",tool,err)}
 }
 work,err:=os.MkdirTemp("","insula-live-iso-*")
 if err!=nil{return "",err}
 // If a FUSE unmount fails, do not traverse/remove a potentially mounted
 // guest filesystem. Preserve the staging directory for diagnosis.
 preserveWork:=false
 defer func(){if !preserveWork{_ = os.RemoveAll(work)}}()
 mount:=filepath.Join(work,"mount")
 stage:=filepath.Join(work,"iso")
 if err:=os.MkdirAll(mount,0700);err!=nil{return "",err}
 if err:=os.MkdirAll(filepath.Join(stage,"live"),0700);err!=nil{return "",err}
 if err:=os.MkdirAll(filepath.Join(stage,"boot","grub"),0700);err!=nil{return "",err}
 guestfsCleanup,err:=mountGuestDiskReadOnly(ctx,image,mount)
 if err!=nil{return "",err}
 mounted:=true
 defer func(){
  if !mounted{return}
  if unmountErr:=unmountGuestDiskReadOnly(mount,guestfsCleanup);unmountErr!=nil{
   preserveWork=true
   runErr=errors.Join(runErr,fmt.Errorf("ISO staging retained at %s: %w",work,unmountErr))
   isoPath=""
  }
 }()
 kernel,initrd,err:=liveiso.SelectKernel(mount)
 if err!=nil{return "",err}
 // Debian Live requires live-boot scripts INSIDE the initramfs. Otherwise
 // grub-mkrescue could create an ISO that has no bootable root filesystem.
 contents,err:=exec.CommandContext(ctx,"lsinitramfs",initrd).Output()
 if err!=nil{return "",fmt.Errorf("inspect live initramfs: %w",err)}
 if !strings.Contains(string(contents),"scripts/live"){
  return "",errors.New("guest initramfs lacks live-boot scripts; install live-boot and run update-initramfs -u in the VM before freeze")
 }
 if err:=liveiso.CopyFile(kernel,filepath.Join(stage,"live","vmlinuz"));err!=nil{return "",err}
 if err:=liveiso.CopyFile(initrd,filepath.Join(stage,"live","initrd.img"));err!=nil{return "",err}
 if err:=liveiso.RunTool(ctx,"mksquashfs",mount,filepath.Join(stage,"live","filesystem.squashfs"),"-noappend","-comp","xz");err!=nil{return "",err}
 // Both the kernel/initramfs and SquashFS have now been copied to staging.
 // A successful ISO must never be published while guestmount is still active.
 unmountErr:=unmountGuestDiskReadOnly(mount,guestfsCleanup)
 mounted=false
 if unmountErr!=nil{
  preserveWork=true
  return "",fmt.Errorf("ISO staging retained at %s: %w",work,unmountErr)
 }
 grub:=liveiso.GrubConfig()
 if err:=os.WriteFile(filepath.Join(stage,"boot","grub","grub.cfg"),[]byte(grub),0600);err!=nil{return "",err}
 if err:=os.MkdirAll(filepath.Dir(dest),0700);err!=nil{return "",err}
 // Always recheck after building the SquashFS: a second process may have
 // published the same output while this image was being assembled.
 if err:=liveiso.CheckDestinationVacant(dest);err!=nil{return "",err}
 staging,err:=os.CreateTemp(filepath.Dir(dest),".insula-live-*.part.iso")
 if err!=nil{return "",err}
 temp:=staging.Name()
 if err:=staging.Close();err!=nil{_ = os.Remove(temp);return "",err}
 defer os.Remove(temp)
 if err:=liveiso.RunTool(ctx,"grub-mkrescue","-o",temp,stage);err!=nil{return "",err}
 stat,err:=os.Stat(temp)
 if err!=nil{return "",err}
 if !stat.Mode().IsRegular()||stat.Size()==0{return "",errors.New("empty or invalid ISO generated")}
 if err:=os.Chmod(temp,0600);err!=nil{return "",err}
 // The temp and destination share a filesystem: Link is atomic and fails
 // rather than overwriting an existing ISO. The temp is removed by defer.
 if err:=os.Link(temp,dest);err!=nil{return "",fmt.Errorf("publish ISO without overwriting %s: %w",dest,err)}
 return dest,nil
}


// liveISODestination resolves user paths before any guest or VM mutation.
// Relative output paths use the caller's working directory (not config.yaml).
func(e *Engine)liveISODestination(name,outputPath string)(string,error){
 if e==nil{return "",errors.New("engine is required")}
 if err:=storage.ValidateName(name);err!=nil{return "",fmt.Errorf("invalid ISO profile name: %w",err)}
 if strings.TrimSpace(outputPath)==""{
  p,err:=e.GetProfile(name)
  if err!=nil{return "",err}
  outputPath=filepath.Join(p.Directory,"exports",name+"-live.iso")
 }else if !strings.EqualFold(filepath.Ext(outputPath),".iso"){
  return "",errors.New("ISO destination must end in .iso")
 }
 abs,err:=filepath.Abs(outputPath)
 if err!=nil{return "",fmt.Errorf("resolve ISO output path: %w",err)}
 if err:=liveiso.CheckDestinationVacant(abs);err!=nil{return "",err}
 return abs,nil
}
