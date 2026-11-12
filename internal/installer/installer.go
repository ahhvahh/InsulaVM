// Package installer contains private Debian installer helpers used by the public InsulaVM Engine.
package installer

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
 "strings"

 "github.com/ahhvahh/InsulaVM/internal/guestfs"
)

func CheckISO(filename string)error{
 f,err:=os.Open(filename)
 if err!=nil{return err}
 defer f.Close()
 magic:=make([]byte,5)
 if _,err:=f.ReadAt(magic,16*2048+1);err!=nil{return fmt.Errorf("read ISO9660 signature: %w",err)}
 if string(magic)!="CD001"{return errors.New("installer image has no ISO9660 CD001 signature")}
 return nil
}
func SHA256File(ctx context.Context,filename string)(string,error){
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
func RandomHex(n int)(string,error){
 b:=make([]byte,n)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return hex.EncodeToString(b),nil
}
func HashPassword(ctx context.Context,secret string)(string,error){
 cmd:=exec.CommandContext(ctx,"openssl","passwd","-6","-stdin")
 cmd.Stdin=strings.NewReader(secret+"\n")
 output,err:=cmd.CombinedOutput()
 if err!=nil{return "",fmt.Errorf("hash ephemeral installer password: %w",err)}
 value:=strings.TrimSpace(string(output))
 if !strings.HasPrefix(value,"$6$")||strings.ContainsAny(value,"\r\n"){return "",errors.New("OpenSSL did not return a SHA-512 crypt password hash")}
 return value,nil
}
func RenderPreseed(user,hash string,passwordlessSudo bool)string{
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

func TailOutput(output string)string{
 const max=3000
 output=strings.TrimSpace(output)
 if len(output)>max{return "..."+output[len(output)-max:]}
 return output
}
func virsh(ctx context.Context,uri string,args ...string)(string,error){
 cmd:=exec.CommandContext(ctx,"virsh",append([]string{"-c",uri},args...)...)
 cmd.Env=append(os.Environ(),"LANG=C","LC_ALL=C")
 out,err:=cmd.CombinedOutput()
 return strings.TrimSpace(string(out)),err
}
func CheckStopped(ctx context.Context,uri,name string)error{
 state,err:=virsh(ctx,uri,"domstate",name)
 if err!=nil{return fmt.Errorf("read installer domain state: %w: %s",err,state)}
 if state!="shut off"{return fmt.Errorf("domain %s still exists in state %q (expected shut off)",name,state)}
 return nil
}
func CleanupDomain(ctx context.Context,uri,name string)error{
 state,err:=virsh(ctx,uri,"domstate",name)
 if err!=nil{
  // When virt-install failed before creating a domain, the operation may
  // report "not found". Inspect all domain names before assuming absence.
  all,listErr:=virsh(ctx,uri,"list","--all","--name")
  if listErr!=nil{return fmt.Errorf("cannot inspect Libvirt after installer error: %w: %s",listErr,all)}
  for _,line:=range strings.Split(all,"\n"){if strings.TrimSpace(line)==name{return fmt.Errorf("domain %s exists but domstate failed: %w",name,err)}}
  return nil
 }
 if state!="shut off"{
  if out,err:=virsh(ctx,uri,"destroy",name);err!=nil{return fmt.Errorf("stop installer VM: %w: %s",err,out)}
  state,err=virsh(ctx,uri,"domstate",name)
  if err!=nil||state!="shut off"{return fmt.Errorf("installer VM did not stop safely: %q %w",state,err)}
 }
 if out,err:=virsh(ctx,uri,"undefine",name);err!=nil{return fmt.Errorf("remove installer domain: %w: %s",err,out)}
 return nil
}
func VerifyDebianDisk(ctx context.Context,disk,stage string)error{
 mount:=filepath.Join(stage,"root")
 if err:=os.MkdirAll(mount,0700);err!=nil{return err}
 guestfsCleanup,err:=guestfs.MountReadOnly(ctx,disk,mount)
 if err!=nil{return fmt.Errorf("mount installed disk: %w",err)}
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
 if unmountErr:=guestfs.UnmountReadOnly(mount,guestfsCleanup);unmountErr!=nil{
  return errors.Join(checkErr,unmountErr)
 }
 return checkErr
}
func VerifyStandaloneQCOW2(ctx context.Context,disk string)error{
 output,err:=exec.CommandContext(ctx,"qemu-img","info","--output=json",disk).CombinedOutput()
 if err!=nil{return fmt.Errorf("qemu-img info: %w: %s",err,strings.TrimSpace(string(output)))}
 var meta struct{Format string `json:"format"`;Backing string `json:"backing-filename"`}
 if err:=json.Unmarshal(output,&meta);err!=nil{return err}
 if meta.Format!="qcow2"||meta.Backing!=""{return errors.New("installed disk is not standalone QCOW2")}
 return nil
}
