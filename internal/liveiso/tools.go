// Package liveiso holds host-side ISO building primitives independent of the InsulaVM Engine.
package liveiso

import (
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "sort"
 "strings"
)

func CheckDestinationVacant(path string)error{
 if path==""{return errors.New("ISO destination path required")}
 _,err:=os.Lstat(path)
 if err==nil{return fmt.Errorf("ISO destination already exists (will not overwrite): %s",path)}
 if !errors.Is(err,os.ErrNotExist){return fmt.Errorf("inspect ISO output: %w",err)}
 return nil
}


// GrubConfig emits early-boot diagnostics to both VGA and ttyS0.
// The kernel's console=ttyS0 alone is insufficient: GRUB has its own
// terminal configuration and may stop before loading the kernel.
func GrubConfig()string{
 return "set timeout_style=menu\n"+
  "set timeout=2\n"+
  "set default=0\n"+
  "serial --unit=0 --speed=115200 --word=8 --parity=no --stop=1\n"+
  "terminal_input serial console\n"+
  "terminal_output serial console\n"+
  "menuentry 'InsulaVM Debian Live' {\n"+
  " linux /live/vmlinuz boot=live components console=tty0 console=ttyS0,115200n8\n"+
  " initrd /live/initrd.img\n"+
  "}\n"
}

func RunTool(ctx context.Context,name string,args ...string)error{
 output,err:=exec.CommandContext(ctx,name,args...).CombinedOutput()
 if err!=nil{return fmt.Errorf("%s failed: %w: %s",name,err,strings.TrimSpace(string(output)))}
 return nil
}

func SelectKernel(root string)(string,string,error){
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

func CopyFile(src,dst string)error{
 input,err:=os.Open(src)
 if err!=nil{return err}
 defer input.Close()
 output,err:=os.OpenFile(dst,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err!=nil{return err}
 _,copyErr:=io.Copy(output,input)
 closeErr:=output.Close()
 return errors.Join(copyErr,closeErr)
}
