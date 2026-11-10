package vm

import (
 "context"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
)

func TestQEMUAccessUsesNarrowFileAndDirectoryACLs(t *testing.T){
 root:=t.TempDir()
 storage:=filepath.Join(root,"nested","private")
 if err:=os.MkdirAll(storage,0700);err!=nil{t.Fatal(err)}
 base:=filepath.Join(storage,"base.qcow2")
 overlay:=filepath.Join(storage,"overlay.qcow2")
 seed:=filepath.Join(storage,"seed.iso")
 for _,file:=range []string{base,overlay,seed}{
  if err:=os.WriteFile(file,[]byte("must-not-change"),0600);err!=nil{t.Fatal(err)}
 }
 bin:=t.TempDir()
 log:=filepath.Join(t.TempDir(),"setfacl.log")
 script:="#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$MOCK_ACL_LOG\"\n"
 if err:=os.WriteFile(filepath.Join(bin,"setfacl"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("MOCK_ACL_LOG",log)
 uid:=os.Geteuid()+1
 if err:=grantQEMUStorageAccess(context.Background(),uid,base,overlay,seed);err!=nil{
  t.Fatalf("per-file QEMU ACL setup failed: %v",err)
 }
 raw,err:=os.ReadFile(log)
 if err!=nil{t.Fatal(err)}
 trace:=string(raw)
 user:="u:"+strconv.Itoa(uid)+":"
 for _,wanted:=range []string{user+"--x",user+"r--",user+"rw-"}{
  if !strings.Contains(trace,wanted){t.Errorf("missing scoped ACL %q in %s",wanted,trace)}
 }
 if strings.Contains(trace,user+"rwx")||strings.Contains(trace,"a::rwx"){
  t.Fatalf("ACL unexpectedly granted full access: %s",trace)
 }
 for _,file:=range []string{base,overlay,seed}{
  contents,err:=os.ReadFile(file)
  if err!=nil||string(contents)!="must-not-change"{
   t.Fatalf("immutable storage changed: %q %v",contents,err)
  }
  info,err:=os.Stat(file)
  if err!=nil||info.Mode().Perm()!=0600{t.Fatalf("host permissions widened: %+v %v",info,err)}
 }
}

func TestQEMUAccessErrorsKeepBaseDisksUntouched(t *testing.T){
 root:=t.TempDir()
 base:=filepath.Join(root,"base.qcow2")
 overlay:=filepath.Join(root,"overlay.qcow2")
 seed:=filepath.Join(root,"seed.iso")
 for _,path:=range []string{base,overlay,seed}{
  if err:=os.WriteFile(path,[]byte("unmodified-image"),0600);err!=nil{t.Fatal(err)}
 }
 bin:=t.TempDir()
 script:="#!/bin/sh\necho 'setfacl refused this UID' >&2\nexit 39\n"
 if err:=os.WriteFile(filepath.Join(bin,"setfacl"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 uid:=os.Geteuid()+1
 if err:=grantQEMUStorageAccess(context.Background(),uid,base,overlay,seed);err==nil||
  !strings.Contains(err.Error(),"setfacl refused this UID"){
  t.Fatalf("ACL failure was ignored: %v",err)
 }
 for _,path:=range []string{base,overlay,seed}{
  data,err:=os.ReadFile(path)
  if err!=nil||string(data)!="unmodified-image"{t.Fatalf("QEMU file was modified despite ACL failure: %q %v",data,err)}
 }
}

func TestQEMUAccessDoesNotAllowDeviceOrBrokenSymlink(t *testing.T){
 root:=t.TempDir()
 base:=filepath.Join(root,"base.qcow2")
 if err:=os.WriteFile(base,[]byte("base"),0600);err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(bin,"setfacl"),[]byte("#!/bin/sh\nexit 0\n"),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 devLink:=filepath.Join(root,"device.qcow2")
 if err:=os.Symlink("/dev/null",devLink);err!=nil{t.Fatal(err)}
 uid:=os.Geteuid()+1
 if err:=grantQEMUStorageAccess(context.Background(),uid,base,devLink,base);err==nil||
  !strings.Contains(err.Error(),"not regular"){
  t.Fatalf("device file was accepted as QCOW2: %v",err)
 }
 broken:=filepath.Join(root,"broken.qcow2")
 if err:=os.Symlink(filepath.Join(root,"missing"),broken);err!=nil{t.Fatal(err)}
 if err:=grantQEMUStorageAccess(context.Background(),uid,base,broken,base);err==nil{
  t.Fatal("dangling backing-image symlink was accepted")
 }
}
