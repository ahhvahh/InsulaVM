package profile

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func qcow2(t *testing.T,path string){
 t.Helper()
 if err:=os.WriteFile(path,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
}

func TestRegisterLoadAndList(t *testing.T){
 root:=t.TempDir()
 source:=filepath.Join(t.TempDir(),"installed.qcow2")
 qcow2(t,source)
 p:=Profile{Name:"debian-go",OS:"debian",MemoryMiB:1024,VCPUs:2,DiskGB:8}
 if err:=Register(root,p,source);err!=nil{t.Fatal(err)}
 names,err:=List(root)
 if err!=nil{t.Fatal(err)}
 if len(names)!=1||names[0]!="debian-go"{t.Fatalf("profiles: %v",names)}
 got,err:=Read(root,"debian-go")
 if err!=nil{t.Fatal(err)}
 if got.Version!="v001"||got.MemoryMiB!=1024 {t.Fatalf("metadata: %+v",got)}
 if _,err:=got.ImagePath();err!=nil{t.Fatal(err)}
 if err:=Register(root,p,source);err==nil{t.Fatal("overwriting profile should fail")}
}

func TestProfileValidationAndEscaping(t *testing.T){
 root:=t.TempDir()
 source:=filepath.Join(root,"source.qcow2")
 qcow2(t,source)
 p:=Profile{Name:"bad",MemoryMiB:1024,VCPUs:1,DiskGB:8,
  Version:"v001",Image:"../../outside.qcow2"}
 if err:=Register(filepath.Join(root,"profiles"),p,source);err==nil{
  t.Fatal("traversal accepted")
 }
 if _,err:=os.Stat(filepath.Join(root,"outside.qcow2"));err==nil{t.Fatal("outside image created")}
 p=Profile{Name:"good",MemoryMiB:128,VCPUs:1,DiskGB:2,
  Version:"v001",Image:"versions/v001/base.qcow2",Directory:root}
 if err:=p.Validate();err!=nil{t.Fatal(err)}
 if err:=os.MkdirAll(filepath.Join(root,"versions","v001"),0700);err!=nil{t.Fatal(err)}
 other:=filepath.Join(t.TempDir(),"external.qcow2")
 qcow2(t,other)
 if err:=os.Symlink(other,filepath.Join(root,p.Image));err!=nil{t.Fatal(err)}
 if _,err:=p.ImagePath();err==nil||!strings.Contains(err.Error(),"escapes"){
  t.Fatalf("expected symlink traversal protection, got %v",err)
 }
}
