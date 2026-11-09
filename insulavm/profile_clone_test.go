package insulavm

import (
 "context"
 "fmt"
 "os"
 "path/filepath"
 "testing"
)

func TestCloneProfileCreatesIndependentDisk(t *testing.T){
 root:=t.TempDir()
 original:=filepath.Join(root,"input.qcow2")
 // Only the QCOW2 magic is needed: registration/copy does not boot the VM.
 if err:=os.WriteFile(original,[]byte{'Q','F','I',0xfb,1,2,3,4},0600);err!=nil{t.Fatal(err)}
 cfg:=filepath.Join(root,"config.yaml")
 if err:=os.WriteFile(cfg,[]byte(fmt.Sprintf("storage:\n  root: %q\nssh:\n  sudo_mode: passwordless\n",filepath.Join(root,"state"))),0600);err!=nil{t.Fatal(err)}
 engine,err:=New(cfg)
 if err!=nil{t.Fatal(err)}
 if err:=engine.RegisterProfile(Profile{Name:"original",OS:"debian",MemoryMiB:1024,VCPUs:1,DiskGB:8},original);err!=nil{t.Fatal(err)}
 cloned,err:=engine.CloneProfile(context.Background(),"original","copied")
 if err!=nil{t.Fatal(err)}
 if cloned.Name!="copied"||cloned.Directory==""{t.Fatalf("invalid cloned profile: %+v",cloned)}
 first,err:=engine.GetProfile("original")
 if err!=nil{t.Fatal(err)}
 originalPath,err:=first.ImagePath()
 if err!=nil{t.Fatal(err)}
 clonePath,err:=cloned.ImagePath()
 if err!=nil{t.Fatal(err)}
 if originalPath==clonePath{t.Fatal("clone must have an independent QCOW2 pathname")}
 cloneContent,err:=os.ReadFile(clonePath)
 if err!=nil{t.Fatal(err)}
 originalContent,err:=os.ReadFile(originalPath)
 if err!=nil{t.Fatal(err)}
 if string(cloneContent)!=string(originalContent){t.Fatal("clone has different contents")}
 if _,err:=engine.CloneProfile(context.Background(),"original","copied");err==nil{t.Fatal("must refuse an existing clone name")}
}
