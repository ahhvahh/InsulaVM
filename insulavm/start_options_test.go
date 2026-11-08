package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "testing"
)

func TestVMStartOptionsReuseProfileWithoutMutation(t *testing.T){
 engine:=testEngine(t,"")
 source:=filepath.Join(t.TempDir(),"debian.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 if err:=engine.RegisterProfile(Profile{
  Name:"debian-reusable",MemoryMiB:1024,VCPUs:1,
 },source);err!=nil{t.Fatal(err)}

 original,err:=engine.resolveVMStartProfile(VMStartOptions{Profile:"debian-reusable"})
 if err!=nil{t.Fatal(err)}
 if original.MemoryMiB!=1024||original.VCPUs!=1{t.Fatalf("wrong defaults: %+v",original)}

 a,err:=engine.resolveVMStartProfile(VMStartOptions{Profile:"debian-reusable",MemoryMiB:2048,VCPUs:2})
 if err!=nil{t.Fatal(err)}
 b,err:=engine.resolveVMStartProfile(VMStartOptions{Profile:"debian-reusable",MemoryMiB:4096,VCPUs:4})
 if err!=nil{t.Fatal(err)}
 if a.MemoryMiB!=2048||a.VCPUs!=2||b.MemoryMiB!=4096||b.VCPUs!=4{
  t.Fatalf("overrides not applied independently: a=%+v b=%+v",a,b)
 }
 if a.Image!=b.Image||a.Directory!=b.Directory{
  t.Fatal("reused profile must keep same backing image")
 }
 stored,err:=engine.GetProfile("debian-reusable")
 if err!=nil{t.Fatal(err)}
 if stored.MemoryMiB!=1024||stored.VCPUs!=1{
  t.Fatalf("profile was mutated on disk: %+v",stored)
 }
}

func TestVMStartOptionsRejectInvalidSettings(t *testing.T){
 engine:=testEngine(t,"")
 cases:=[]VMStartOptions{
  {},
  {Profile:"not-registered"},
  {Profile:"not-registered",MemoryMiB:-1},
  {Profile:"not-registered",VCPUs:-1},
 }
 for _,tc:=range cases{
  if _,err:=engine.resolveVMStartProfile(tc);err==nil{
   t.Fatalf("accepted invalid VM settings: %+v",tc)
  }
 }
 source:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 if err:=engine.RegisterProfile(Profile{Name:"limits",MemoryMiB:512,VCPUs:1},source);err!=nil{t.Fatal(err)}
 for _,tc:=range []VMStartOptions{
  {Profile:"limits",MemoryMiB:64},
  {Profile:"limits",VCPUs:257},
 }{
  if _,err:=engine.resolveVMStartProfile(tc);err==nil{
   t.Fatalf("accepted out-of-range VM settings: %+v",tc)
  }
 }
}

func TestVMStartOptionsContextPolicyRequiresCancelableContext(t *testing.T){
 engine:=testEngine(t,"")
 _,err:=engine.StartVMWithOptions(context.Background(),VMStartOptions{
  Profile:"not-registered",StopOnContextDone:true,
 },nil)
 if err==nil{t.Fatal("accepted a non-cancelable context for automatic cleanup")}
 if _,err:=engine.StartVMWithOptions(nil,VMStartOptions{},nil);err==nil{
  t.Fatal("accepted nil context")
 }
}

func TestVMStartInstallationScriptValidation(t *testing.T){
 dir:=t.TempDir()
 good:=filepath.Join(dir,"setup.sh")
 if err:=os.WriteFile(good,[]byte("#!/bin/bash\nset -e\nprintf ready\n"),0600);err!=nil{t.Fatal(err)}
 data,err:=readVMStartupScript(good)
 if err!=nil||len(data)==0{t.Fatalf("valid setup script failed: %v",err)}
 if _,err:=readVMStartupScript("relative/setup.sh");err==nil{t.Fatal("accepted relative script")}
 link:=filepath.Join(dir,"link.sh")
 if err:=os.Symlink(good,link);err!=nil{t.Fatal(err)}
 if _,err:=readVMStartupScript(link);err==nil{t.Fatal("accepted symlink script")}
 empty:=filepath.Join(dir,"empty.sh")
 if err:=os.WriteFile(empty,nil,0600);err!=nil{t.Fatal(err)}
 if _,err:=readVMStartupScript(empty);err==nil{t.Fatal("accepted empty script")}
 nul:=filepath.Join(dir,"nul.sh")
 if err:=os.WriteFile(nul,[]byte{0},0600);err!=nil{t.Fatal(err)}
 if _,err:=readVMStartupScript(nul);err==nil{t.Fatal("accepted NUL in startup script")}
 big:=filepath.Join(dir,"big.sh")
 if err:=os.WriteFile(big,make([]byte,256*1024+1),0600);err!=nil{t.Fatal(err)}
 if _,err:=readVMStartupScript(big);err==nil{t.Fatal("accepted oversized startup script")}
}
