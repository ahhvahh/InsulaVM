package insulavm

import (
 installer "github.com/ahhvahh/InsulaVM/internal/installer"
 "github.com/ahhvahh/InsulaVM/internal/guestfs"
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/storage"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// CreateProfileFromISORequest installs Debian 13 amd64 from a local Debian
// Installer ISO into an independent QCOW2 and publishes a reusable profile.
// The installer is unattended and can download packages over the Libvirt
// network; this operation does not import an already installed Live ISO.
type CreateProfileFromISORequest struct {
 Name string
 ISOPath string
 MemoryMiB int
 VCPUs int
 DiskGB int
 GuestDestination string
 Timeout time.Duration
 OnProgress func(ProfileISOProgress)
}
type ProfileISOProgress struct {
 Stage string
 Message string
 At time.Time
}
type CreateProfileFromISOResult struct {
 Profile *Profile
 ImagePath string
 ISOPath string
 ISOChecksumSHA256 string
 Installed bool
}

var linuxInstallerUser=regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,30}$`)

func (e *Engine) CreateProfileFromISO(ctx context.Context,req CreateProfileFromISORequest)(result *CreateProfileFromISOResult,runErr error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 if err:=storage.ValidateName(req.Name);err!=nil{return nil,fmt.Errorf("invalid profile name: %w",err)}
 if err:=profile.ValidateGuestDestination(req.GuestDestination);err!=nil{return nil,fmt.Errorf("invalid guest destination: %w",err)}
 if !linuxInstallerUser.MatchString(e.config.SSH.User){return nil,errors.New("ssh.user must be a valid Linux account name for Debian Installer")}
 if !filepath.IsAbs(req.ISOPath){return nil,errors.New("ISO path must be absolute")}
 stat,err:=os.Lstat(req.ISOPath)
 if err!=nil{return nil,fmt.Errorf("open installer ISO: %w",err)}
 if !stat.Mode().IsRegular(){return nil,errors.New("installer ISO must be a regular file, not a symlink")}
 if err:=installer.CheckISO(req.ISOPath);err!=nil{return nil,err}
 if req.MemoryMiB<1024 || req.MemoryMiB>1024*1024{return nil,errors.New("installer memory_mib must be 1024..1048576")}
 if req.VCPUs<1||req.VCPUs>256{return nil,errors.New("installer vcpus must be 1..256")}
 if req.DiskGB<5||req.DiskGB>4096{return nil,errors.New("installer disk_gb must be 5..4096")}
 if req.Timeout<0{return nil,errors.New("installer timeout cannot be negative")}
 if req.Timeout==0{req.Timeout=90*time.Minute}
 if req.Timeout>6*time.Hour{return nil,errors.New("installer timeout exceeds 6 hours")}
 for _,tool:=range []string{"virt-install","virsh","qemu-img","guestmount","guestunmount","openssl"}{
  if _,err:=exec.LookPath(tool);err!=nil{return nil,fmt.Errorf("missing host tool %q: %w",tool,err)}
 }
 if err:=e.config.EnsureDirectories();err!=nil{return nil,err}
 target:=filepath.Join(e.config.Storage.Profiles,req.Name)
 if _,err:=os.Lstat(target);err==nil{return nil,fmt.Errorf("profile %q already exists",req.Name)}else if !errors.Is(err,os.ErrNotExist){return nil,err}
 notify:=func(stage,message string){
  if req.OnProgress!=nil{req.OnProgress(ProfileISOProgress{Stage:stage,Message:message,At:time.Now()})}
 }
 notify("validating","Debian installer ISO and profile parameters validated")
 hash,err:=installer.SHA256File(ctx,req.ISOPath)
 if err!=nil{return nil,err}
 stage,err:=os.MkdirTemp(e.config.Storage.Runs,"insula-install-")
 if err!=nil{return nil,err}
 disk:=filepath.Join(stage,"install.qcow2")
 preseed:=filepath.Join(stage,"preseed.cfg")
 domainID,err:=installer.RandomHex(8)
 if err!=nil{_ = os.RemoveAll(stage);return nil,err}
 domain:="insula-install-"+domainID
 createdDomain:=false
 safeRemove:=true
 defer func(){
  if createdDomain{
   cleanupCtx,cancel:=context.WithTimeout(context.Background(),40*time.Second)
   cleanupErr:=installer.CleanupDomain(cleanupCtx,e.config.Virtualization.LibvirtURI,domain)
   cancel()
   if cleanupErr!=nil{
    safeRemove=false
    runErr=errors.Join(runErr,fmt.Errorf("cannot verify installer VM is stopped; staging retained at %s: %w",stage,cleanupErr))
   }
  }
  if safeRemove{
   if err:=os.RemoveAll(stage);err!=nil{runErr=errors.Join(runErr,fmt.Errorf("remove temporary installer files: %w",err))}
  }
 }()
 password,err:=installer.RandomHex(32)
 if err!=nil{return nil,err}
 hashed,err:=installer.HashPassword(ctx,password)
 password=""
 if err!=nil{return nil,err}
 if err:=os.WriteFile(preseed,[]byte(installer.RenderPreseed(e.config.SSH.User,hashed,e.config.SSH.SudoMode=="passwordless")),0600);err!=nil{return nil,err}
 out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2",disk,strconv.Itoa(req.DiskGB)+"G").CombinedOutput()
 if err!=nil{return nil,fmt.Errorf("create installation disk: %w: %s",err,strings.TrimSpace(string(out)))}
 if e.config.Virtualization.QEMUUID>0{
  if err:=vm.GrantQEMUInstallAccess(ctx,e.config.Virtualization.QEMUUID,req.ISOPath,disk);err!=nil{return nil,err}
 }
 notify("disk","Temporary QCOW2 created; launching unattended Debian Installer")
 installCtx,cancel:=context.WithTimeout(ctx,req.Timeout)
 defer cancel()
 args:=debianVirtInstallArgs(e.config.Virtualization.LibvirtURI,e.config.Virtualization.Network,domain,disk,preseed,req)
 // The domain can be created even if virt-install returns an error. It has
 // a random, reserved name; cleanup checks its state before removing disks.
 createdDomain=true
 cmd:=exec.CommandContext(installCtx,"virt-install",args...)
 cmd.Env=append(os.Environ(),"LANG=C","LC_ALL=C")
 output,err:=cmd.CombinedOutput()
 if err!=nil{return nil,fmt.Errorf("Debian installation failed or timed out: %w: %s",err,installer.TailOutput(string(output)))}
 if err:=installer.CheckStopped(ctx,e.config.Virtualization.LibvirtURI,domain);err!=nil{
  return nil,fmt.Errorf("installer did not finish safely; disk preserved until domain cleanup: %w",err)
 }
 // Disassociate the stopped installer before touching its filesystem or
 // publishing the disk, so it cannot be restarted while we copy the image.
 cleanupCtx,stopCleanup:=context.WithTimeout(context.Background(),40*time.Second)
 cleanupErr:=installer.CleanupDomain(cleanupCtx,e.config.Virtualization.LibvirtURI,domain)
 stopCleanup()
 if cleanupErr!=nil{return nil,fmt.Errorf("cannot remove stopped installer domain before verification: %w",cleanupErr)}
 createdDomain=false
 notify("verification","Installer exited; validating Debian 13, SSH, sudo, cloud-init and live-boot on disk")
 if err:=installer.VerifyDebianDisk(ctx,disk,stage);err!=nil{
  if errors.Is(err,guestfs.ErrUnmount){safeRemove=false}
  return nil,err
 }
 if err:=installer.VerifyStandaloneQCOW2(ctx,disk);err!=nil{return nil,err}
 notify("publishing","Registering independent QCOW2 profile")
 p:=Profile{
  Name:req.Name,OS:"debian",MemoryMiB:req.MemoryMiB,VCPUs:req.VCPUs,
  DiskGB:req.DiskGB,ISO:req.ISOPath,SSH:profile.SSH{User:e.config.SSH.User},
   Guest:profile.Guest{Destination:req.GuestDestination},
 }
 if err:=e.RegisterProfile(p,disk);err!=nil{return nil,fmt.Errorf("publish installed profile: %w",err)}
 saved,err:=e.GetProfile(req.Name)
 if err!=nil{return nil,err}
 image,err:=saved.ImagePath()
 if err!=nil{return nil,err}
 notify("ready","Installed Debian 13 profile registered")
 return &CreateProfileFromISOResult{
  Profile:saved,ImagePath:image,ISOPath:req.ISOPath,ISOChecksumSHA256:hash,Installed:true,
 },nil
}

func debianVirtInstallArgs(uri,network,name,disk,preseed string,req CreateProfileFromISORequest)[]string{
 // Debian netboot mini.iso has /linux and /initrd.gz. virt-install may
 // not auto-detect them from an ISO; provide documented explicit paths.
 location:=req.ISOPath
 if filepath.Base(req.ISOPath)=="mini.iso"{
  location+=",kernel=linux,initrd=initrd.gz"
 }
 return []string{
  "--connect",uri,"--name",name,
  "--memory",strconv.Itoa(req.MemoryMiB),"--vcpus",strconv.Itoa(req.VCPUs),
  "--arch","x86_64",
  "--disk","path="+disk+",format=qcow2,bus=virtio",
  "--network","network="+network+",model=virtio",
  "--osinfo","detect=on,require=off",
  "--location",location,
  "--initrd-inject",preseed,
  "--extra-args","auto=true priority=critical locale=en_US.UTF-8 console=ttyS0,115200n8 interface=auto netcfg/dhcp_timeout=60 preseed/file=/preseed.cfg",
  "--graphics","none","--console","pty,target_type=serial",
  "--noautoconsole","--wait","-1","--noreboot",
 }
}
