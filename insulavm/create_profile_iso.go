package insulavm

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
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
 if !linuxInstallerUser.MatchString(e.config.SSH.User){return nil,errors.New("ssh.user must be a valid Linux account name for Debian Installer")}
 if !filepath.IsAbs(req.ISOPath){return nil,errors.New("ISO path must be absolute")}
 stat,err:=os.Lstat(req.ISOPath)
 if err!=nil{return nil,fmt.Errorf("open installer ISO: %w",err)}
 if !stat.Mode().IsRegular(){return nil,errors.New("installer ISO must be a regular file, not a symlink")}
 if err:=checkDebianInstallerISO(req.ISOPath);err!=nil{return nil,err}
 if req.MemoryMiB<1024 || req.MemoryMiB>1024*1024{return nil,errors.New("installer memory_mib must be 1024..1048576")}
 if req.VCPUs<1||req.VCPUs>256{return nil,errors.New("installer vcpus must be 1..256")}
 if req.DiskGB<8||req.DiskGB>4096{return nil,errors.New("installer disk_gb must be 8..4096")}
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
 hash,err:=fileSHA256(ctx,req.ISOPath)
 if err!=nil{return nil,err}
 stage,err:=os.MkdirTemp(e.config.Storage.Runs,"insula-install-")
 if err!=nil{return nil,err}
 disk:=filepath.Join(stage,"install.qcow2")
 preseed:=filepath.Join(stage,"preseed.cfg")
 domainID,err:=randomHex(8)
 if err!=nil{_ = os.RemoveAll(stage);return nil,err}
 domain:="insula-install-"+domainID
 createdDomain:=false
 safeRemove:=true
 defer func(){
  if createdDomain{
   cleanupCtx,cancel:=context.WithTimeout(context.Background(),40*time.Second)
   cleanupErr:=cleanupISOInstallerDomain(cleanupCtx,e.config.Virtualization.LibvirtURI,domain)
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
 password,err:=randomHex(32)
 if err!=nil{return nil,err}
 hashed,err:=hashInstallerPassword(ctx,password)
 password=""
 if err!=nil{return nil,err}
 if err:=os.WriteFile(preseed,[]byte(renderDebianPreseed(e.config.SSH.User,hashed,e.config.SSH.SudoMode=="passwordless")),0600);err!=nil{return nil,err}
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
 if err!=nil{return nil,fmt.Errorf("Debian installation failed or timed out: %w: %s",err,tailInstallOutput(string(output)))}
 if err:=checkInstallerStopped(ctx,e.config.Virtualization.LibvirtURI,domain);err!=nil{
  return nil,fmt.Errorf("installer did not finish safely; disk preserved until domain cleanup: %w",err)
 }
 // Disassociate the stopped installer before touching its filesystem or
 // publishing the disk, so it cannot be restarted while we copy the image.
 cleanupCtx,stopCleanup:=context.WithTimeout(context.Background(),40*time.Second)
 cleanupErr:=cleanupISOInstallerDomain(cleanupCtx,e.config.Virtualization.LibvirtURI,domain)
 stopCleanup()
 if cleanupErr!=nil{return nil,fmt.Errorf("cannot remove stopped installer domain before verification: %w",cleanupErr)}
 createdDomain=false
 notify("verification","Installer exited; validating Debian 13, SSH, sudo, cloud-init and live-boot on disk")
 if err:=verifyDebianInstalledDisk(ctx,disk,stage);err!=nil{
  if errors.Is(err,errInstallerUnmount){safeRemove=false}
  return nil,err
 }
 if err:=verifyStandaloneQCOW2(ctx,disk);err!=nil{return nil,err}
 notify("publishing","Registering independent QCOW2 profile")
 p:=Profile{
  Name:req.Name,OS:"debian",MemoryMiB:req.MemoryMiB,VCPUs:req.VCPUs,
  DiskGB:req.DiskGB,ISO:req.ISOPath,SSH:profile.SSH{User:e.config.SSH.User},
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

func checkDebianInstallerISO(filename string)error{
 f,err:=os.Open(filename)
 if err!=nil{return err}
 defer f.Close()
 magic:=make([]byte,5)
 if _,err:=f.ReadAt(magic,16*2048+1);err!=nil{return fmt.Errorf("read ISO9660 signature: %w",err)}
 if string(magic)!="CD001"{return errors.New("installer image has no ISO9660 CD001 signature")}
 return nil
}
func fileSHA256(ctx context.Context,filename string)(string,error){
 f,err:=os.Open(filename)
 if err!=nil{return "",err}
 defer f.Close()
 h:=sha256.New()
 buf:=make([]byte,1024*1024)
 for {
  if err:=ctx.Err();err!=nil{return "",err}
  n,err:=f.Read(buf)
  if n>0{_,_=h.Write(buf[:n])}
  if errors.Is(err,io.EOF){break}
  if err!=nil{return "",err}
 }
 return hex.EncodeToString(h.Sum(nil)),nil
}
func randomHex(n int)(string,error){
 b:=make([]byte,n)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return hex.EncodeToString(b),nil
}
func hashInstallerPassword(ctx context.Context,secret string)(string,error){
 cmd:=exec.CommandContext(ctx,"openssl","passwd","-6","-stdin")
 cmd.Stdin=strings.NewReader(secret+"\n")
 output,err:=cmd.CombinedOutput()
 if err!=nil{return "",fmt.Errorf("hash ephemeral installer password: %w",err)}
 value:=strings.TrimSpace(string(output))
 if !strings.HasPrefix(value,"$6$")||strings.ContainsAny(value,"\r\n"){return "",errors.New("OpenSSL did not return a SHA-512 crypt password hash")}
 return value,nil
}
func renderDebianPreseed(user,hash string,passwordlessSudo bool)string{
 late:=`in-target sh -c 'cloud-init clean --logs --machine-id --seed; systemctl enable ssh; systemctl enable qemu-guest-agent; update-initramfs -u'`
 if passwordlessSudo{
  late+=`; in-target sh -c 'printf "%s ALL=(ALL) NOPASSWD:ALL\n" `+user+` > /etc/sudoers.d/90-insulavm && chmod 0440 /etc/sudoers.d/90-insulavm'`
 }
 return fmt.Sprintf(`# InsulaVM unattended Debian 13 amd64; only a disposable QCOW2 is partitioned.
d-i debian-installer/locale string en_US.UTF-8
d-i keyboard-configuration/xkb-keymap select us
d-i netcfg/choose_interface select auto
d-i netcfg/get_hostname string insulavm-builder
d-i netcfg/get_domain string localdomain
d-i mirror/country string manual
d-i mirror/http/hostname string deb.debian.org
d-i mirror/http/directory string /debian
d-i mirror/http/proxy string
d-i clock-setup/utc boolean true
d-i time/zone string Etc/UTC
d-i clock-setup/ntp boolean true
d-i passwd/root-login boolean false
d-i passwd/user-fullname string InsulaVM
d-i passwd/username string %s
d-i passwd/user-password-crypted password %s
d-i partman-auto/disk string /dev/vda
d-i partman-auto/method string regular
d-i partman-auto/choose_recipe select atomic
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true
d-i apt-setup/use_mirror boolean true
tasksel tasksel/first multiselect standard, ssh-server
d-i pkgsel/include string openssh-server sudo cloud-init cloud-guest-utils qemu-guest-agent live-boot
d-i pkgsel/upgrade select none
popularity-contest popularity-contest/participate boolean false
d-i grub-installer/only_debian boolean true
d-i grub-installer/bootdev string /dev/vda
d-i preseed/late_command string %s
d-i finish-install/reboot_in_progress note
d-i debian-installer/exit/poweroff boolean true
`,user,hash,late)
}
func debianVirtInstallArgs(uri,network,name,disk,preseed string,req CreateProfileFromISORequest)[]string{
 return []string{
  "--connect",uri,"--name",name,
  "--memory",strconv.Itoa(req.MemoryMiB),"--vcpus",strconv.Itoa(req.VCPUs),
  "--arch","x86_64",
  "--disk","path="+disk+",format=qcow2,bus=virtio",
  "--network","network="+network+",model=virtio",
  "--osinfo","detect=on,require=off",
  "--location",req.ISOPath,
  "--initrd-inject",preseed,
  "--extra-args","auto=true priority=critical locale=en_US.UTF-8 console=ttyS0,115200n8 interface=auto netcfg/dhcp_timeout=60 preseed/file=/preseed.cfg",
  "--graphics","none","--console","pty,target_type=serial",
  "--noautoconsole","--wait","-1","--noreboot",
 }
}
func tailInstallOutput(output string)string{
 const max=3000
 output=strings.TrimSpace(output)
 if len(output)>max{return "..."+output[len(output)-max:]}
 return output
}
func installerVirsh(ctx context.Context,uri string,args ...string)(string,error){
 cmd:=exec.CommandContext(ctx,"virsh",append([]string{"-c",uri},args...)...)
 cmd.Env=append(os.Environ(),"LANG=C","LC_ALL=C")
 out,err:=cmd.CombinedOutput()
 return strings.TrimSpace(string(out)),err
}
func checkInstallerStopped(ctx context.Context,uri,name string)error{
 state,err:=installerVirsh(ctx,uri,"domstate",name)
 if err!=nil{return fmt.Errorf("read installer domain state: %w: %s",err,state)}
 if state!="shut off"{return fmt.Errorf("domain %s still exists in state %q (expected shut off)",name,state)}
 return nil
}
func cleanupISOInstallerDomain(ctx context.Context,uri,name string)error{
 state,err:=installerVirsh(ctx,uri,"domstate",name)
 if err!=nil{
  // When virt-install failed before creating a domain, the operation may
  // report "not found". Inspect all domain names before assuming absence.
  all,listErr:=installerVirsh(ctx,uri,"list","--all","--name")
  if listErr!=nil{return fmt.Errorf("cannot inspect Libvirt after installer error: %w: %s",listErr,all)}
  for _,line:=range strings.Split(all,"\n"){if strings.TrimSpace(line)==name{return fmt.Errorf("domain %s exists but domstate failed: %w",name,err)}}
  return nil
 }
 if state!="shut off"{
  if out,err:=installerVirsh(ctx,uri,"destroy",name);err!=nil{return fmt.Errorf("stop installer VM: %w: %s",err,out)}
  state,err=installerVirsh(ctx,uri,"domstate",name)
  if err!=nil||state!="shut off"{return fmt.Errorf("installer VM did not stop safely: %q %w",state,err)}
 }
 if out,err:=installerVirsh(ctx,uri,"undefine",name);err!=nil{return fmt.Errorf("remove installer domain: %w: %s",err,out)}
 return nil
}
var errInstallerUnmount=errors.New("guest filesystem remains mounted")

func verifyDebianInstalledDisk(ctx context.Context,disk,stage string)error{
 mount:=filepath.Join(stage,"root")
 if err:=os.MkdirAll(mount,0700);err!=nil{return err}
 output,err:=exec.CommandContext(ctx,"guestmount","--ro","-a",disk,"-i",mount).CombinedOutput()
 if err!=nil{return fmt.Errorf("mount installed disk: %w: %s",err,strings.TrimSpace(string(output)))}
 checkErr:=func()error{
  release,err:=os.ReadFile(filepath.Join(mount,"etc","os-release"))
  if err!=nil{return fmt.Errorf("installed system missing /etc/os-release: %w",err)}
  contents:=string(release)
  if !strings.Contains("\n"+contents,"\nID=debian\n")||
    (!strings.Contains(contents,"VERSION_ID=\"13\"")&&!strings.Contains(contents,"VERSION_ID=13\n")){
   return errors.New("installed disk is not Debian 13")
  }
  for _,name:=range []string{"usr/bin/cloud-init","usr/sbin/sshd","usr/bin/sudo","usr/share/initramfs-tools/scripts/live"}{
   if _,err:=os.Stat(filepath.Join(mount,filepath.FromSlash(name)));err!=nil{
    return fmt.Errorf("installed Debian missing required component %s: %w",name,err)
   }
  }
  return nil
 }()
 unmountCtx,cancel:=context.WithTimeout(context.Background(),40*time.Second)
 defer cancel()
 unmountOutput,unmountErr:=exec.CommandContext(unmountCtx,"guestunmount",mount).CombinedOutput()
 if unmountErr!=nil{return errors.Join(checkErr,fmt.Errorf("%w: guestunmount failed; staging must be retained: %v: %s",errInstallerUnmount,unmountErr,strings.TrimSpace(string(unmountOutput))))}
 return checkErr
}
func verifyStandaloneQCOW2(ctx context.Context,disk string)error{
 output,err:=exec.CommandContext(ctx,"qemu-img","info","--output=json",disk).CombinedOutput()
 if err!=nil{return fmt.Errorf("qemu-img info: %w: %s",err,strings.TrimSpace(string(output)))}
 var meta struct{Format string `json:"format"`;Backing string `json:"backing-filename"`}
 if err:=json.Unmarshal(output,&meta);err!=nil{return err}
 if meta.Format!="qcow2"||meta.Backing!=""{return errors.New("installed disk is not standalone QCOW2")}
 return nil
}
