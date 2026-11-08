package insulavm

import (
 "os"
 "path/filepath"
 "testing"
)

func TestSelectLiveKernel(t *testing.T){
 root:=t.TempDir()
 boot:=filepath.Join(root,"boot")
 if err:=os.MkdirAll(boot,0700);err!=nil{t.Fatal(err)}
 for _,name:=range []string{"vmlinuz-6.12.0","initrd.img-6.12.0","vmlinuz-6.10.0"}{
  if err:=os.WriteFile(filepath.Join(boot,name),[]byte("test"),0600);err!=nil{t.Fatal(err)}
 }
 k,i,err:=selectLiveKernel(root)
 if err!=nil{t.Fatal(err)}
 if filepath.Base(k)!="vmlinuz-6.12.0"||filepath.Base(i)!="initrd.img-6.12.0"{t.Fatalf("kernel=%s initrd=%s",k,i)}
}
func TestSelectLiveKernelMissingInitrd(t *testing.T){
 root:=t.TempDir()
 if err:=os.MkdirAll(filepath.Join(root,"boot"),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"boot","vmlinuz-1"),[]byte("x"),0600);err!=nil{t.Fatal(err)}
 _,_,err:=selectLiveKernel(root)
 if err==nil{t.Fatal("expected error when no matching initramfs exists")}
}
