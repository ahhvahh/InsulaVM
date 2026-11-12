package liveiso

import (
 "os"
 "path/filepath"
 "testing"
 "strings"
)

func TestSelectLiveKernel(t *testing.T){
 root:=t.TempDir()
 boot:=filepath.Join(root,"boot")
 if err:=os.MkdirAll(boot,0700);err!=nil{t.Fatal(err)}
 for _,name:=range []string{"vmlinuz-6.12.0","initrd.img-6.12.0","vmlinuz-6.10.0"}{
  if err:=os.WriteFile(filepath.Join(boot,name),[]byte("test"),0600);err!=nil{t.Fatal(err)}
 }
 k,i,err:=SelectKernel(root)
 if err!=nil{t.Fatal(err)}
 if filepath.Base(k)!="vmlinuz-6.12.0"||filepath.Base(i)!="initrd.img-6.12.0"{t.Fatalf("kernel=%s initrd=%s",k,i)}
}
func TestSelectLiveKernelMissingInitrd(t *testing.T){
 root:=t.TempDir()
 if err:=os.MkdirAll(filepath.Join(root,"boot"),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"boot","vmlinuz-1"),[]byte("x"),0600);err!=nil{t.Fatal(err)}
 _,_,err:=SelectKernel(root)
 if err==nil{t.Fatal("expected error when no matching initramfs exists")}
}

func TestLiveISOGrubSerialAndKernelConsole(t *testing.T){
 cfg:=GrubConfig()
 for _,entry:=range []string{
  "serial --unit=0 --speed=115200",
  "terminal_input serial console",
  "terminal_output serial console",
  "set timeout=2",
  "linux /live/vmlinuz boot=live components console=tty0 console=ttyS0,115200n8",
  "initrd /live/initrd.img",
 }{
  if !strings.Contains(cfg,entry){t.Errorf("missing %q from ISO GRUB config:\n%s",entry,cfg)}
 }
 if strings.Index(cfg,"terminal_output serial console")>strings.Index(cfg,"menuentry "){
  t.Fatal("GRUB serial terminal must be configured before menuentry")
 }
}
