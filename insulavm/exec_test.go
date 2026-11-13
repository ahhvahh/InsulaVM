package insulavm

import (
 "bytes"
 "context"
 "os"
 "net"
 "strconv"
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
 fakeRunningLibvirt(t,id)
 _,portText,err:=net.SplitHostPort(host)
 if err!=nil{t.Fatal(err)}
 port,err:=strconv.Atoi(portText)
 if err!=nil{t.Fatal(err)}
 vm:=registry.Entry{
  ID:id,LibvirtUUID:id,Name:"insula-test",Profile:"test",MAC:"52:54:00:00:00:01",
  SSHUser:"test",SSHPort:port,PrivateKeyPath:key,KnownHostsPath:hosts,InstanceDir:t.TempDir(),SudoMode:"disabled",
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
 if result.LogDirectory!=""{t.Fatalf("SSH default should not persist logs: %q",result.LogDirectory)}
 if _,err:=os.Stat(engine.config.Storage.Logs);!os.IsNotExist(err){
  t.Fatalf("SSH default unexpectedly created logs: %v",err)
 }
 withLog,err:=engine.ExecVM(ctx,ExecRequest{
  VMID:id,Command:"echo hello-from-ssh",Workspace:t.TempDir(),Output:&console,Log:true,
 })
 if err!=nil{t.Fatal(err)}
 if withLog.LogDirectory==""{t.Fatal("opt-in SSH log path missing")}
 data,err:=os.ReadFile(withLog.LogDirectory+"/command.log")
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(data),"hello-from-ssh"){t.Fatal("opt-in SSH output not persisted")}
 if _,err:=engine.ExecVM(ctx,ExecRequest{VMID:id,Command:"id -u",Sudo:true});err==nil{
  t.Fatal("elevated command allowed on sudo-disabled VM")
 }
}
