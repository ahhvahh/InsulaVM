package main

import (
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestVMShellArgsUsePinnedHostAndGuestDirectory(t *testing.T){
 record:=registry.Entry{
  IP:"192.168.122.107",SSHUser:"insula",SSHPort:22,
  PrivateKeyPath:"/srv/insula/runs/id/client_ed25519",
  KnownHostsPath:"/srv/insula/runs/id/known_hosts",
 }
 args,err:=vmShellArgs(record,"/tmp/insulavm/workspace","InsulaVM")
 if err!=nil{t.Fatal(err)}
 joined:=strings.Join(args," ")
 for _,want:=range []string{
  "-tt","-i /srv/insula/runs/id/client_ed25519",
  "UserKnownHostsFile=/srv/insula/runs/id/known_hosts",
  "StrictHostKeyChecking=yes","IdentitiesOnly=yes",
  "-p 22","insula@192.168.122.107",
  "cd -- '/tmp/insulavm/workspace/InsulaVM' && exec",
 }{
  if !strings.Contains(joined,want){t.Errorf("missing %q: %q",want,joined)}
 }
 if strings.Contains(joined,"StrictHostKeyChecking=no"){t.Fatal("unsafe SSH mode")}
}
func TestVMShellRejectsPathTraversalAndInvalidPort(t *testing.T){
 good:=registry.Entry{
  IP:"127.0.0.1",SSHUser:"insula",SSHPort:22,
  PrivateKeyPath:"/tmp/key",KnownHostsPath:"/tmp/known_hosts",
 }
 for _,dir:=range []string{"..","../etc","x/../../etc","/etc"}{
  if _,err:=vmShellArgs(good,"/tmp/workspace",dir);err==nil{
   t.Fatalf("accepted guest directory traversal %q",dir)
  }
 }
 bad:=good
 bad.SSHPort=0
 if _,err:=vmShellArgs(bad,"/tmp/workspace","");err==nil{t.Fatal("accepted missing SSH port")}
}
func TestVMShellQuotesGuestDirectory(t *testing.T){
 good:=registry.Entry{
  IP:"127.0.0.1",SSHUser:"insula",SSHPort:22,
  PrivateKeyPath:"/tmp/key",KnownHostsPath:"/tmp/known_hosts",
 }
 args,err:=vmShellArgs(good,"/tmp/insulavm/work space","it'works")
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(args[len(args)-1],"'it'\\''works'"){
  t.Fatalf("expected POSIX quoted directory in remote shell: %q",args[len(args)-1])
 }
}
