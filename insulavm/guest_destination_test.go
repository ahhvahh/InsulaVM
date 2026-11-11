package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/profile"
)

func TestPerProfileDestinationOverridesLegacyGlobal(t *testing.T) {
 engine:=testEngine(t,"guest:\n  destination: /tmp/legacy-workspace\n")
 src:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(src,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{name,destination string}{
  {"a","/tmp/a/work"},
  {"b","/tmp/b/work"},
  {"legacy",""},
 }{
  p:=Profile{Name:tc.name,MemoryMiB:512,VCPUs:1,DiskGB:2,
   Guest:profile.Guest{Destination:tc.destination}}
  if err:=engine.RegisterProfile(p,src);err!=nil{t.Fatal(err)}
  want:=tc.destination
  if want==""{want="/tmp/legacy-workspace"}
  got,err:=engine.GuestDestinationForProfile(tc.name)
  if err!=nil||got!=want{t.Fatalf("profile %s: %q, %v; want %q",tc.name,got,err,want)}
 }
}

func TestProfileDestinationUsedByRunImportsAndCommands(t *testing.T) {
 host,key,known:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 src:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(src,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 target:=filepath.Join(t.TempDir(),"profile guest work")
 if err:=engine.RegisterProfile(Profile{Name:"custom",MemoryMiB:512,VCPUs:1,DiskGB:2,
  Guest:profile.Guest{Destination:target}},src);err!=nil{t.Fatal(err)}
 input:=filepath.Join(t.TempDir(),"payload.txt")
 if err:=os.WriteFile(input,[]byte("per-profile"),0600);err!=nil{t.Fatal(err)}
 m:=&Manifest{Profile:"custom",Repository:Repository{Name:"checks"},Action:"test",
  Imports:[]ImportSpec{{Source:input}},Setup:[]string{"test -f payload.txt"}}
 result,err:=engine.Run(context.Background(),RunRequest{
  Manifest:m,Host:host,User:"test",PrivateKey:key,KnownHosts:known,
 })
 if err!=nil{t.Fatal(err)}
 if len(result.Imports)!=1||result.Imports[0].DestinationPath!=target{
  t.Fatalf("wrong import directory: %+v",result.Imports)
 }
 payload,err:=os.ReadFile(filepath.Join(target,"payload.txt"))
 if err!=nil||string(payload)!="per-profile"{t.Fatalf("unexpected payload %q err=%v",payload,err)}
}

func TestProfileDestinationUsedByManagedImportAndExec(t *testing.T) {
 host,key,known:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 src:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(src,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 destination:=filepath.Join(t.TempDir(),"managed workspace")
 if err:=engine.RegisterProfile(Profile{Name:"test",MemoryMiB:512,VCPUs:1,DiskGB:2,
  Guest:profile.Guest{Destination:destination}},src);err!=nil{t.Fatal(err)}
 id:=registeredLocalSSH(t,engine,host,key,known)
 input:=filepath.Join(t.TempDir(),"managed.txt")
 if err:=os.WriteFile(input,[]byte("test"),0600);err!=nil{t.Fatal(err)}
 imported,err:=engine.ImportToVM(context.Background(),VMImportRequest{VMID:id,SourcePath:input})
 if err!=nil{t.Fatal(err)}
 if imported.DestinationPath!=destination{t.Fatalf("import to %q instead of %q",imported.DestinationPath,destination)}
 _,err=engine.ExecVM(context.Background(),ExecRequest{VMID:id,Command:"test -f managed.txt"})
 if err!=nil{t.Fatalf("exec did not use profile guest workspace: %v",err)}
}

func TestUnmanagedImportDestinationRejectsUnsafePaths(t *testing.T){
 engine:=testEngine(t,"")
 for _,dst:=range []string{"relative","/","/tmp/bad\npath","/tmp/bad\x00path"}{
  _,err:=engine.ImportPath(context.Background(),ImportRequest{Host:"127.0.0.1:22",Destination:dst})
  if err==nil||!strings.Contains(err.Error(),"destination"){t.Fatalf("accepted %q: %v",dst,err)}
 }
}
