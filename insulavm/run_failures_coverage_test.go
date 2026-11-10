package insulavm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestEngineRunReportsSafeFailureAndRetainsLogs(t *testing.T){
 engine:=testEngine(t,"")
 manifest:=func()*Manifest{return &Manifest{
  Repository:Repository{Name:"project"},Action:"ci",
  Setup:[]string{"echo ready"},
 }}
 tests:=[]struct{
  name string
  ctx context.Context
  req RunRequest
  partial bool
  want string
 }{
  {"nil context",nil,RunRequest{Manifest:manifest()} ,false,"context"},
  {"manifest missing",context.Background(),RunRequest{},false,"exactly one"},
  {"conflicting manifest sources",context.Background(),RunRequest{Manifest:manifest(),ManifestPath:"other.yaml"},false,"exactly one"},
  {"negative timeout",context.Background(),RunRequest{Manifest:manifest(),CommandTimeout:-time.Second},false,"timeout"},
  {"nonexistent manifest file",context.Background(),RunRequest{ManifestPath:filepath.Join(t.TempDir(),"missing.yaml")},false,"no such file"},
  {"no host no profile",context.Background(),RunRequest{Manifest:manifest()},true,"no profile"},
  {"unavailable SSH credentials",context.Background(),RunRequest{Manifest:manifest(),Host:"127.0.0.1:22"},true,"known_hosts"},
  {"invalid profile",context.Background(),RunRequest{
   Manifest:&Manifest{Repository:Repository{Name:"project"},Action:"ci",Profile:"missing",Setup:[]string{"echo ok"}},
   Host:"127.0.0.1:22"},true,"resolve profile"},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   result,err:=engine.Run(tc.ctx,tc.req)
   if err==nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("wanted error containing %q; got %+v / %v",tc.want,result,err)
   }
   if tc.partial{
    if result==nil||result.FinishedAt.Before(result.StartedAt){
     t.Fatalf("partial execution metadata not preserved: %+v",result)
    }
   }else if result!=nil{
    t.Fatalf("unexpected execution state on invalid input: %+v",result)
   }
  })
 }
}

func TestEngineRunManagedProfileOverridesGuestSSHUserWithoutStartingVM(t *testing.T){
 address,key,known:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 source:=filepath.Join(t.TempDir(),"installed.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
 if err:=engine.RegisterProfile(Profile{Name:"cloneable",MemoryMiB:1024,VCPUs:1,DiskGB:8},source);err!=nil{t.Fatal(err)}
 manifest:=&Manifest{
  Repository:Repository{Name:"repo"},Action:"build",
  Profile:"cloneable",Workspace:t.TempDir(),
  Setup:[]string{"echo safe-external-host"},
 }
 result,err:=engine.Run(context.Background(),RunRequest{
  Manifest:manifest,Host:address,
  User:"test",PrivateKey:key,KnownHosts:known,
  CommandTimeout:5*time.Second,
 })
 if err!=nil{t.Fatalf("existing SSH host with profile metadata failed: %v",err)}
 if result==nil||result.VMID!=""||result.Profile!="cloneable"||result.SSHUser!="test"{
  t.Fatalf("profile execution incorrectly launched a KVM VM: %+v",result)
 }
 if len(result.CompletedJobs)!=1||result.CompletedJobs[0]!="setup"{
  t.Fatalf("profile setup command was not executed: %+v",result)
 }
}

func TestEngineRunManifestPathAndAllowFailureViaRealSSH(t *testing.T){
 host,key,known:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 workspace:=t.TempDir()
 manifest:=filepath.Join(t.TempDir(),"insula.yaml")
 src:="repository:\n  name: project\naction: ci\nworkspace: "+workspace+
  "\nsetup:\n  - echo before\npipelines:\n  - name: verify\n    jobs:\n"+
  "      - name: allow-failure\n        commands:\n          - exit 13\n          - echo after\n"+
  "        allow_failure: true\n"
 if err:=os.WriteFile(manifest,[]byte(src),0600);err!=nil{t.Fatal(err)}
 result,err:=engine.Run(context.Background(),RunRequest{
  ManifestPath:manifest,Host:host,User:"test",PrivateKey:key,
  KnownHosts:known,CommandTimeout:5*time.Second,
 })
 if err!=nil{t.Fatalf("manifest-path allow_failure broke: %v",err)}
 if len(result.CompletedJobs)!=2||result.CompletedJobs[1]!="allow-failure"{
  t.Fatalf("allowed nonzero command wrongly blocked job: %+v",result)
 }
 if _,err:=os.Stat(filepath.Join(result.LogDirectory,"allow-failure.log"));err!=nil{
  t.Fatalf("persistent job log missing: %v",err)
 }
}

func TestEnginePublicVMStartGuardsAndStatus(t *testing.T){
 engine:=testEngine(t,"")
 if _,err:=engine.StartVM(nil,"profile");err==nil{t.Fatal("nil context accepted")}
 if _,err:=engine.StartVMWithProgress(nil,"profile",nil);err==nil{t.Fatal("nil progress context accepted")}
 if _,err:=engine.StartVM(context.Background(),"missing");err==nil{t.Fatal("missing profile started")}
 if _,err:=engine.StartVMWithProgress(context.Background(),"missing",nil);err==nil{
  t.Fatal("progress start accepted unregistered profile")
 }
 if _,err:=engine.ListVMStatus(nil);err==nil{t.Fatal("nil VM status context accepted")}
 if _,err:=engine.GetVMStatus(context.Background(),"missing");err==nil{t.Fatal("unknown UUID accepted")}
 cancelled,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=engine.ListVMs(cancelled);!errors.Is(err,context.Canceled){
  t.Fatalf("cancelled VM history read did not return cancellation: %v",err)
 }
}
