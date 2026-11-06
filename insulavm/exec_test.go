package insulavm

import (
 "bytes"
 "context"
 "os"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestExecVMByRegistryIDAndSudoPolicy(t *testing.T){
 host,key,hosts:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 store,err:=registry.Open(engine.config.Storage.Database)
 if err!=nil{t.Fatal(err)}
 ctx:=context.Background()
 id:="019a5d11-1234-7123-8123-123456789abc"
 vm:=registry.Entry{
  ID:id,LibvirtUUID:id,Name:"insula-test",Profile:"test",MAC:"52:54:00:00:00:01",
  SSHUser:"test",PrivateKeyPath:key,KnownHostsPath:hosts,InstanceDir:t.TempDir(),SudoMode:"disabled",
 }
 if err:=store.Insert(ctx,vm);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(ctx,id,strings.Split(host,":")[0]);err!=nil{t.Fatal(err)}
 _=store.Close()
 var console bytes.Buffer
 result,err:=engine.ExecVM(ctx,ExecRequest{
  VMID:id,Command:"echo hello-from-ssh",Workspace:t.TempDir(),Output:&console,
 })
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(console.String(),"hello-from-ssh"){
  t.Fatalf("missing command output %q",console.String())
 }
 data,err:=os.ReadFile(result.LogDirectory+"/command.log")
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(data),"hello-from-ssh"){t.Fatal("SSH output not persisted")}
 if _,err:=engine.ExecVM(ctx,ExecRequest{VMID:id,Command:"id -u",Sudo:true});err==nil{
  t.Fatal("elevated command allowed on sudo-disabled VM")
 }
}
