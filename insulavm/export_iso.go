package insulavm

import (
 "context"
 "errors"
 "fmt"
 "os"
 "io"
 "os/exec"
 "path/filepath"
 "sort"
 "strings"
 "time"
)

// ExportProfileLiveISO builds a BIOS/UEFI Debian Live ISO from a saved QCOW2.
// Prerequisite: guest has live-boot installed and an initramfs regenerated
// with live-boot support before it was frozen. No arbitrary guest execution
// is performed by the host: libguestfs mounts the image read-only.
func(e *Engine)ExportProfileLiveISO(ctx context.Context,name string)(string,error){
 if e==nil{return "",errors.New("engine is required")}
 if ctx==nil{return "",errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return "",err}
 p,err:=e.GetProfile(name)
 if err!=nil{return "",err}
 image,err:=p.ImagePath()
 if err!=nil{return "",err}
 for _,tool:=range []string{"guestmount","guestunmount","mksquashfs","grub-mkrescue","lsinitramfs"}{
  if _,err:=exec.LookPath(tool);err!=nil{return "",fmt.Errorf("required host tool %s: %w",tool,err)}
 }
 work,err:=os.MkdirTemp("","insula-live-iso-*")
 if err!=nil{return "",err}
 defer os.RemoveAll(work)
 mount:=filepath.Join(work,"mount")
 stage:=filepath.Join(work,"iso")
 if err:=os.MkdirAll(mount,0700);err!=nil{return "",err}
 if err:=os.MkdirAll(filepath.Join(stage,"live"),0700);err!=nil{return "",err}
 if err:=os.MkdirAll(filepath.Join(stage,"boot","grub"),0700);err!=nil{return "",err}
 if err:=runISOTool(ctx,"guestmount","--ro","-a",image,"-i",mount);err!=nil{return "",err}
 defer func(){
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
  defer cancel()
  _=runISOTool(cleanupCtx,"guestunmount",mount)
 }()
 kernel,initrd,err:=selectLiveKernel(mount)
 if err!=nil{return "",err}
 // Debian Live requires live-boot scripts INSIDE the initramfs. Otherwise
 // grub-mkrescue could create an ISO that has no bootable root filesystem.
 contents,err:=exec.CommandContext(ctx,"lsinitramfs",initrd).Output()
 if err!=nil{return "",fmt.Errorf("inspect live initramfs: %w",err)}
 if !strings.Contains(string(contents),"scripts/live"){
  return "",errors.New("guest initramfs lacks live-boot scripts; install live-boot and run update-initramfs -u in the VM before freeze")
 }
 if err:=copyISOFile(kernel,filepath.Join(stage,"live","vmlinuz"));err!=nil{return "",err}
 if err:=copyISOFile(initrd,filepath.Join(stage,"live","initrd.img"));err!=nil{return "",err}
 if err:=runISOTool(ctx,"mksquashfs",mount,filepath.Join(stage,"live","filesystem.squashfs"),"-noappend","-comp","xz");err!=nil{return "",err}
 grub:="set timeout=2\nset default=0\nmenuentry 'InsulaVM Debian Live' {\n linux /live/vmlinuz boot=live components console=tty0 console=ttyS0,115200n8\n initrd /live/initrd.img\n}\n"
 if err:=os.WriteFile(filepath.Join(stage,"boot","grub","grub.cfg"),[]byte(grub),0600);err!=nil{return "",err}
 exports:=filepath.Join(p.Directory,"exports")
 if err:=os.MkdirAll(exports,0700);err!=nil{return "",err}
 dest:=filepath.Join(exports,p.Name+"-live.iso")
 if _,err:=os.Lstat(dest);err==nil{return "",fmt.Errorf("ISO already exists: %s",dest)}else if !errors.Is(err,os.ErrNotExist){return "",err}
 temp:=filepath.Join(exports,"."+p.Name+"-live.part.iso")
 if _,err:=os.Lstat(temp);err==nil{return "",fmt.Errorf("unfinished export exists: %s",temp)}else if !errors.Is(err,os.ErrNotExist){return "",err}
 defer os.Remove(temp)
 if err:=runISOTool(ctx,"grub-mkrescue","-o",temp,stage);err!=nil{return "",err}
 stat,err:=os.Stat(temp)
 if err!=nil{return "",err}
 if stat.Size()==0{return "",errors.New("empty ISO generated")}
 if err:=os.Chmod(temp,0600);err!=nil{return "",err}
 if err:=os.Rename(temp,dest);err!=nil{return "",err}
 return dest,nil
}

func runISOTool(ctx context.Context,name string,args ...string)error{
 output,err:=exec.CommandContext(ctx,name,args...).CombinedOutput()
 if err!=nil{return fmt.Errorf("%s failed: %w: %s",name,err,strings.TrimSpace(string(output)))}
 return nil
}

func selectLiveKernel(root string)(string,string,error){
 candidates,err:=filepath.Glob(filepath.Join(root,"boot","vmlinuz-*"))
 if err!=nil{return "","",err}
 sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
 for _,kernel:=range candidates{
  suffix:=strings.TrimPrefix(filepath.Base(kernel),"vmlinuz-")
  initrd:=filepath.Join(root,"boot","initrd.img-"+suffix)
  k,ke:=os.Stat(kernel)
  i,ie:=os.Stat(initrd)
  if ke==nil&&ie==nil&&k.Mode().IsRegular()&&i.Mode().IsRegular(){return kernel,initrd,nil}
 }
 return "","",errors.New("guest has no matching /boot/vmlinuz-* and /boot/initrd.img-* files")
}

func copyISOFile(src,dst string)error{
 input,err:=os.Open(src)
 if err!=nil{return err}
 defer input.Close()
 output,err:=os.OpenFile(dst,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err!=nil{return err}
 _,copyErr:=io.Copy(output,input)
 closeErr:=output.Close()
 return errors.Join(copyErr,closeErr)
}
