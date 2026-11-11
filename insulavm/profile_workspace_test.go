package insulavm

import (
 "os"
 "path/filepath"
 "testing"
)

func TestResolveVMWorkspacePrefersProfileAndFallsBack(t *testing.T){
 engine:=testEngine(t,"")
 legacy:=engine.Configuration().Guest.Destination
 source:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{name,destination,guest,want string}{
  {"with-profile-workspace","/tmp/profile-workspace","","/tmp/profile-workspace"},
  {"without-profile-workspace","","",legacy},
  {"with-guest-destination","","/tmp/guest-workspace","/tmp/guest-workspace"},
  {"workspace-takes-precedence","/tmp/profile-workspace","/tmp/guest-workspace","/tmp/profile-workspace"},
 }{
  t.Run(tc.name,func(t *testing.T){
   p:=Profile{Name:tc.name,MemoryMiB:1024,VCPUs:1,DiskGB:8}
   p.Workspace.Destination=tc.destination
   p.Guest.Destination=tc.guest
   if err:=engine.RegisterProfile(p,source);err!=nil{t.Fatal(err)}
   got,err:=engine.ResolveVMWorkspace(tc.name)
   if err!=nil{t.Fatal(err)}
   if got!=tc.want{t.Fatalf("workspace=%q, expected %q",got,tc.want)}
  })
 }
 for _,name:=range []string{"","not-registered"}{
  got,err:=engine.ResolveVMWorkspace(name)
  if err!=nil||got!=legacy{t.Fatalf("legacy fallback %q: got %q, err %v",name,got,err)}
 }
}
