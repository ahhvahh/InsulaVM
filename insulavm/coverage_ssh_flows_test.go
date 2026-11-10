package insulavm

import (
    "bytes"
    "context"
    "errors"
    "net"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "testing"
    "time"

    "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestStandaloneImportPathThroughRealSSH(t *testing.T){
    host,key,known:=runTestSSHServer(t)
    engine:=testEngine(t,"")
    destination:=filepath.Join(t.TempDir(),"guest's workspace")
    engine.config.Guest.Destination=destination
    source:=filepath.Join(t.TempDir(),"payload.txt")
    if err:=os.WriteFile(source,[]byte("transfer-test-InsulaVM"),0600);err!=nil{t.Fatal(err)}
    var output bytes.Buffer
    var stages []string
    result,err:=engine.ImportPath(context.Background(),ImportRequest{
        SourcePath:source,Host:host,User:"transfer-test",PrivateKey:key,KnownHosts:known,
        Output:&output,OnProgress:func(p ImportProgress){stages=append(stages,p.Stage)},
    })
    if err!=nil{t.Fatalf("SSH import failed: %v",err)}
    if result==nil||!result.Imported||result.Files!=1||result.DestinationPath!=destination{
        t.Fatalf("invalid import confirmation: %+v",result)
    }
    received,err:=os.ReadFile(filepath.Join(destination,"payload.txt"))
    if err!=nil||string(received)!="transfer-test-InsulaVM"{t.Fatalf("file was not imported: %q %v",received,err)}
    for _,stage:=range []string{"validating","compressing","compressed","uploading","verifying","extracting","completed"}{
        found:=false
        for _,actual:=range stages{if actual==stage{found=true}}
        if !found{t.Errorf("missing progress stage %q: %v",stage,stages)}
    }
    if !strings.Contains(output.String(),"[InsulaVM][import:completed]"){
        t.Fatalf("no confirmation in output: %s",output.String())
    }
}

func TestEngineRunImportsAndCommandsThroughRealSSH(t *testing.T){
    host,key,known:=runTestSSHServer(t)
    engine:=testEngine(t,"")
    destination:=filepath.Join(t.TempDir(),"root with spaces")
    engine.config.Guest.Destination=destination
    source:=filepath.Join(t.TempDir(),"package.txt")
    if err:=os.WriteFile(source,[]byte("imported-in-pipeline"),0600);err!=nil{t.Fatal(err)}
    m:=&Manifest{
        Repository:Repository{Name:"source-project"},Action:"test-pipeline",
        Workspace:destination,
        Imports:[]ImportSpec{{Source:source}},
        Setup:[]string{"test -f package.txt"},
        Pipelines:[]Pipeline{{Name:"validation",Jobs:[]Job{
            {Name:"verify",Commands:[]string{"grep -q imported-in-pipeline package.txt","echo done"}},
        }}},
    }
    var output bytes.Buffer
    var stages []string
    result,err:=engine.Run(context.Background(),RunRequest{
        Manifest:m,Host:host,User:"test",PrivateKey:key,KnownHosts:known,
        Output:&output,CommandTimeout:15*time.Second,
        OnImportProgress:func(p ImportProgress){stages=append(stages,p.Stage)},
    })
    if err!=nil{t.Fatalf("run+import through SSH failed: %v",err)}
    if result==nil||len(result.Imports)!=1||!result.Imports[0].Imported{
        t.Fatalf("missing import result: %+v",result)
    }
    if len(result.CompletedJobs)!=2||result.CompletedJobs[0]!="setup"||result.CompletedJobs[1]!="verify"{
        t.Fatalf("missing job confirmation: %+v",result)
    }
    if !strings.Contains(strings.Join(stages,","),"completed"){
        t.Fatalf("no completion signal: %v",stages)
    }
    if !strings.Contains(output.String(),"[verify] done"){t.Fatalf("streaming output missing: %s",output.String())}
}

func registeredLocalSSH(t *testing.T,e *Engine,host,key,known string)string{
    t.Helper()
    ip,portString,err:=net.SplitHostPort(host)
    if err!=nil{t.Fatal(err)}
    port,err:=strconv.Atoi(portString)
    if err!=nil{t.Fatal(err)}
    const id="019a5d11-1234-7123-8123-123456789abc"
    store,err:=registry.Open(e.config.Storage.Database)
    if err!=nil{t.Fatal(err)}
    defer store.Close()
    err=store.Insert(context.Background(),registry.Entry{
        ID:id,LibvirtUUID:id,Name:"insula-test-"+id[:8],Profile:"test",
        MAC:"52:54:00:00:11:22",SudoMode:"disabled",
        SSHUser:"test",SSHPort:port,PrivateKeyPath:key,KnownHostsPath:known,
        InstanceDir:filepath.Join(e.config.Storage.Runs,"insula-vm-"+id),
    })
    if err!=nil{t.Fatal(err)}
    if err:=store.SetRunning(context.Background(),id,ip);err!=nil{t.Fatal(err)}
    fakeRunningLibvirt(t,id)
    return id
}

func TestRegisteredEngineVMStatusExecAndSSHDiagnostics(t *testing.T){
    host,key,known:=runTestSSHServer(t)
    engine:=testEngine(t,"")
    engine.config.Guest.Destination=filepath.Join(t.TempDir(),"guest-workspace")
    id:=registeredLocalSSH(t,engine,host,key,known)
    record,err:=engine.GetVM(context.Background(),id)
    if err!=nil||record.ID!=id||record.Status!="running"{t.Fatalf("bad VM registry record: %+v %v",record,err)}
    history,err:=engine.ListVMs(context.Background())
    if err!=nil||len(history)!=1{t.Fatalf("VM history unavailable: %+v %v",history,err)}
    status,err:=engine.GetVMStatus(context.Background(),id)
    if err!=nil||status.LibvirtState!="running"{t.Fatalf("bad Libvirt status: %+v %v",status,err)}
    all,err:=engine.ListVMStatus(context.Background())
    if err!=nil||len(all)!=1||!all[0].IsRunning(){t.Fatalf("running VM not listed: %+v %v",all,err)}
    var output bytes.Buffer
    command,err:=engine.ExecVM(context.Background(),ExecRequest{
        VMID:id,Command:"printf verified-execution",Output:&output,
    })
    if err!=nil||command==nil||command.LogDirectory==""{t.Fatalf("ExecVM failed: %+v %v",command,err)}
    if !strings.Contains(output.String(),"verified-execution"){
        t.Fatalf("SSH stdout was not streamed: %q",output.String())
    }
    diagnostic,err:=engine.DiagnoseVMSSH(context.Background(),id,10*time.Second)
    if err!=nil||diagnostic==nil||!diagnostic.Authenticated||diagnostic.VMID!=id||diagnostic.IP!="127.0.0.1"{
        t.Fatalf("authenticated diagnostic missing: %+v %v",diagnostic,err)
    }
    if diagnostic.SudoReady{t.Fatal("sudo readiness asserted without passwordless mode")}
}

func TestEngineSSHOperationValidationAndCancellation(t *testing.T){
    engine:=testEngine(t,"")
    if _,err:=engine.ImportPath(nil,ImportRequest{});err==nil{t.Fatal("nil context allowed")}
    if _,err:=engine.ImportPath(context.Background(),ImportRequest{});err==nil{t.Fatal("missing host allowed")}
    if _,err:=engine.ImportPath(context.Background(),ImportRequest{Host:"127.0.0.1:1"});err==nil{
        t.Fatal("missing auth information accepted")
    }
    if _,err:=engine.ExecVM(nil,ExecRequest{VMID:"id",Command:"id"});err==nil{t.Fatal("nil ExecVM context allowed")}
    for _,command:=range []string{""," ","echo\x00bad"}{
        if _,err:=engine.ExecVM(context.Background(),ExecRequest{VMID:"id",Command:command});err==nil{
            t.Fatalf("invalid command accepted %q",command)
        }
    }
    if _,err:=engine.DiagnoseVMSSH(nil,"id",time.Second);err==nil{t.Fatal("nil diagnostic context accepted")}
    if _,err:=engine.DiagnoseVMSSH(context.Background(),"",-time.Second);err==nil{t.Fatal("blank diagnostic ID accepted")}
    if _,err:=engine.DiagnoseVMSSH(context.Background(),"id",2*time.Minute);err==nil{t.Fatal("oversized timeout accepted")}
    if _,err:=engine.StartVMFromISO(nil,LiveISOStartOptions{});err==nil{t.Fatal("nil ISO context accepted")}
    if _,err:=engine.StartVMFromISO(context.Background(),LiveISOStartOptions{});err==nil{t.Fatal("missing ISO profile accepted")}
    if _,err:=engine.DiagnoseLiveISO(context.Background(),"unknown");err==nil{t.Fatal("missing VM was accepted")}
    if _,err:=engine.ListVMs(nil);err==nil{t.Fatal("nil ListVMs context allowed")}
    if _,err:=engine.GetVM(nil,"id");err==nil{t.Fatal("nil GetVM context allowed")}
    cancelled,cancel:=context.WithCancel(context.Background())
    cancel()
    if _,err:=engine.Run(cancelled,RunRequest{});err==nil{t.Fatal("bad manifest allowed on cancelled context")}
    if err:=engine.StopVM(nil,"id");err==nil{t.Fatal("nil StopVM context allowed")}
    if !errors.Is(context.Canceled,context.Canceled){t.Fatal("unreachable")}
}
