package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestManagedVMExecRefusesUnauthorizedCommandsBeforeSSH(t *testing.T){
 host,key,known:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 id:=registeredLocalSSH(t,engine,host,key,known)
 cases:=[]struct{name string;request ExecRequest;want string}{
  {"unavailable sudo",ExecRequest{VMID:id,Command:"id",Sudo:true},"sudo is not enabled"},
  {"relative workspace",ExecRequest{VMID:id,Command:"echo hi",Workspace:"relative"},"absolute guest path"},
  {"newline workspace",ExecRequest{VMID:id,Command:"echo hi",Workspace:"/tmp/bad\npath"},"absolute guest path"},
  {"negative timeout",ExecRequest{VMID:id,Command:"echo hi",Workspace:"/tmp",CommandTimeout:-time.Second},"timeout"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   result,err:=engine.ExecVM(context.Background(),tc.request)
   if err==nil||result!=nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("unsafe command %s was allowed: %+v %v",tc.name,result,err)
   }
  })
 }
}

func TestDiagnoseLiveISOPreservesHostEvidenceForRunningVM(t *testing.T){
 host,key,known:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 id:=registeredLocalSSH(t,engine,host,key,known)
 snapshot,err:=engine.DiagnoseLiveISO(context.Background(),id)
 if err!=nil{t.Fatalf("failed host-side Libvirt diagnostic: %v",err)}
 if snapshot==nil||snapshot.VMID!=id||snapshot.LibvirtState!="running"||snapshot.Profile!="test"{
  t.Fatalf("incomplete ISO diagnostic: %+v",snapshot)
 }
 content,err:=os.ReadFile(snapshot.SnapshotFile)
 if err!=nil||!strings.Contains(string(content),"domstate"){
  t.Fatalf("diagnostic snapshot missing Libvirt status: %q %v",content,err)
 }
 if filepath.Base(snapshot.LogsDirectory)!=id{t.Fatalf("diagnostic stored under unexpected UUID: %s",snapshot.LogsDirectory)}
}

func TestManagedVMExecFailsWhenDomainAbsentEvenIfSQLiteRunning(t *testing.T){
 host,key,known:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 id:=registeredLocalSSH(t,engine,host,key,known)
 bin:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte("#!/bin/sh\ncase \"$3\" in list) exit 0;; *) exit 92;; esac\n"),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 result,err:=engine.ExecVM(context.Background(),ExecRequest{VMID:id,Command:"echo no-ssh"})
 if err==nil||result!=nil||!strings.Contains(err.Error(),"not running"){
  t.Fatalf("stale SQLite status incorrectly enabled execution: %+v %v",result,err)
 }
}

func TestPublicStartVMOptionsRejectUntrustedInputsBeforeKVM(t *testing.T){
 engine:=testEngine(t,"")
 source:=filepath.Join(t.TempDir(),"installed.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 if err:=engine.RegisterProfile(Profile{Name:"prepared",MemoryMiB:1024,VCPUs:1,DiskGB:8},source);err!=nil{t.Fatal(err)}
 script:=filepath.Join(t.TempDir(),"startup.sh")
 if err:=os.WriteFile(script,[]byte("#!/bin/bash\ntrue\n"),0600);err!=nil{t.Fatal(err)}
 cases:=[]struct{name string;opts VMStartOptions;want string}{
  {"unknown profile",VMStartOptions{Profile:"missing"},"load VM profile"},
  {"empty profile",VMStartOptions{},"profile"},
  {"invalid memory",VMStartOptions{Profile:"prepared",MemoryMiB:-1},"MemoryMiB"},
  {"invalid VCPU",VMStartOptions{Profile:"prepared",VCPUs:-1},"VCPUs"},
  {"below profile limit",VMStartOptions{Profile:"prepared",MemoryMiB:64},"instance options"},
  {"oversized SSH deadline",VMStartOptions{Profile:"prepared",SSHReadyTimeout:2*time.Hour},"SSHReadyTimeout"},
  {"uncancellable cleanup",VMStartOptions{Profile:"prepared",StopOnContextDone:true},"cancelable context"},
  {"relative script",VMStartOptions{Profile:"prepared",StartupScriptPath:"relative.sh"},"absolute"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=engine.StartVMWithOptions(context.Background(),tc.opts,nil);err==nil||
    !strings.Contains(err.Error(),tc.want){
    t.Fatalf("untrusted VM options accepted: %s %v",tc.name,err)
   }
  })
 }
 cancelled,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=engine.StartVMWithOptions(cancelled,VMStartOptions{Profile:"prepared"},nil);err==nil{
  t.Fatal("cancelled startup accepted")
 }
 if _,err:=engine.StartVMFromISO(context.Background(),LiveISOStartOptions{Profile:"prepared",ISOPath:"/no/such.iso"});err==nil{
  t.Fatal("missing ISO passed validation")
 }
}
