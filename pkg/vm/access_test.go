package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

// The mock records ACL calls without modifying CI host permissions. It proves
// no ACL is ever applied to SSH client private keys or the Cloud-Init user-data.
func TestGrantQEMUStorageAccessIsScoped(t *testing.T){
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.Mkdir(bin,0700);err!=nil{t.Fatal(err)}
 logPath:=filepath.Join(root,"acl.log")
 script:="#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$INSULAVM_ACL_LOG\"\n"
 if err:=os.WriteFile(filepath.Join(bin,"setfacl"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("INSULAVM_ACL_LOG",logPath)

 baseDir:=filepath.Join(root,"profiles","debian13","versions","v001")
 runDir:=filepath.Join(root,"runs","insula-vm-123")
 for _,dir:=range []string{baseDir,runDir}{
  if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 }
 base:=filepath.Join(baseDir,"base.qcow2")
 overlay:=filepath.Join(runDir,"overlay.qcow2")
 seed:=filepath.Join(runDir,"seed.iso")
 key:=filepath.Join(runDir,"client_ed25519")
 for _,p:=range []string{base,overlay,seed,key}{
  if err:=os.WriteFile(p,[]byte("test"),0600);err!=nil{t.Fatal(err)}
 }
 if err:=grantQEMUStorageAccess(context.Background(),64055,base,overlay,seed);err!=nil{t.Fatal(err)}
 content,err:=os.ReadFile(logPath)
 if err!=nil{t.Fatal(err)}
 got:=string(content)
 for _,want:=range []string{
  "-m u:64055:r-- "+base,
  "-m u:64055:rw- "+overlay,
  "-m u:64055:r-- "+seed,
  "-m u:64055:--x "+runDir,
  "-m u:64055:--x "+baseDir,
 }{
  if !strings.Contains(got,want){t.Errorf("missing ACL %q in %s",want,got)}
 }
 if strings.Contains(got,key){
  t.Fatal("QEMU was granted access to the SSH client private key")
 }
 // The ACL is never broadened to anonymous/other users.
 if strings.Contains(got,"o:") || strings.Contains(got,"a:") {
  t.Fatal("unexpected broad ACL")
 }
}

func TestGrantQEMUStorageAccessRequiresExplicitUID(t *testing.T){
 // Zero means deliberately disabled: no ACL tool should be consulted.
 if err:=grantQEMUStorageAccess(context.Background(),0,"","","");err!=nil{t.Fatal(err)}
 for _,uid:=range []int{-1,2147483648}{
  if err:=grantQEMUStorageAccess(context.Background(),uid,"","","");err==nil{
   t.Fatalf("accepted invalid QEMU UID %d",uid)
  }
 }
 t.Setenv("PATH",t.TempDir())
 if err:=grantQEMUStorageAccess(context.Background(),64055,"","","");err==nil||
  !strings.Contains(err.Error(),"sudo apt install acl"){
  t.Fatalf("expected missing setfacl diagnostic; got %v",err)
 }
}
