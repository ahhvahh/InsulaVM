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

func TestImportToManagedVMUsesPinnedRealSSH(t *testing.T){
    host,key,known:=runTestSSHServer(t)
    e:=testEngine(t,"")
    e.config.Guest.Destination=filepath.Join(t.TempDir(),"guest destination")
    id:=registeredLocalSSH(t,e,host,key,known)
    input:=filepath.Join(t.TempDir(),"source.txt")
    if err:=os.WriteFile(input,[]byte("managed-file-payload"),0600);err!=nil{t.Fatal(err)}
    var stages []string
    result,err:=e.ImportToVM(context.Background(),VMImportRequest{
        VMID:id,SourcePath:input,
        OnProgress:func(p ImportProgress){stages=append(stages,p.Stage)},
    })
    if err!=nil||result==nil||!result.Imported||result.Files!=1{
        t.Fatalf("managed SSH import not confirmed: %+v %v",result,err)
    }
    data,err:=os.ReadFile(filepath.Join(e.config.Guest.Destination,"source.txt"))
    if err!=nil||string(data)!="managed-file-payload"{
        t.Fatalf("incorrect guest file: %q %v",data,err)
    }
    if !strings.Contains(strings.Join(stages,","),"completed"){
        t.Fatalf("transfer progress not finished: %v",stages)
    }
}

func TestManagedImportRequestRejectsBadIDsAndContexts(t *testing.T){
    e:=testEngine(t,"")
    if _,err:=e.ImportToVM(nil,VMImportRequest{VMID:"id"});err==nil{t.Fatal("nil context accepted")}
    if _,err:=e.ImportToVM(context.Background(),VMImportRequest{});err==nil{t.Fatal("no VMID accepted")}
    if _,err:=e.ImportToVM(context.Background(),VMImportRequest{VMID:"missing"});err==nil{t.Fatal("unknown UUID accepted")}
    if _,err:=e.ImportPath(context.Background(),ImportRequest{
        SourcePath:"/tmp/source",Host:"invalid-host",User:"test",PrivateKey:"/missing-key",KnownHosts:"/missing-hosts",
    });err==nil{t.Fatal("missing SSH private key was accepted")}
}

func TestRunReadsManifestFileAndValidatesLibraryOverrides(t *testing.T){
    host,key,known:=runTestSSHServer(t)
    e:=testEngine(t,"")
    destination:=filepath.Join(t.TempDir(),"workspace")
    e.config.Guest.Destination=destination
    manifestPath:=filepath.Join(t.TempDir(),"insula.yaml")
    manifest:="repository:\n  name: go-fixture\n"+
        "action: ci\nworkspace: "+destination+"\n"+
        "setup:\n  - echo prepared\n"+
        "pipelines:\n  - name: checks\n    jobs:\n"+
        "      - name: run\n        commands:\n          - echo verified\n"
    if err:=os.WriteFile(manifestPath,[]byte(manifest),0600);err!=nil{t.Fatal(err)}
    result,err:=e.Run(context.Background(),RunRequest{
        ManifestPath:manifestPath,Host:host,User:"go-user",PrivateKey:key,
        KnownHosts:known,CommandTimeout:5*time.Second,
    })
    if err!=nil||result==nil||len(result.CompletedJobs)!=2{
        t.Fatalf("manifest source run failed: %+v %v",result,err)
    }
    if result.SSHUser!="go-user"{t.Fatalf("SSH override not reflected: %+v",result)}
    _,err=e.Run(context.Background(),RunRequest{ManifestPath:filepath.Join(t.TempDir(),"missing.yaml"),Host:host})
    if err==nil{t.Fatal("missing manifest did not fail")}
    _,err=e.Run(context.Background(),RunRequest{
        ManifestPath:manifestPath,Host:host,CommandTimeout:-time.Second,
    })
    if err==nil||!strings.Contains(err.Error(),"negative"){t.Fatalf("negative timeout accepted: %v",err)}
    _,err=e.Run(context.Background(),RunRequest{
        ManifestPath:manifestPath,Host:host,
    })
    if err==nil||!strings.Contains(err.Error(),"SSH user, private key"){t.Fatalf("missing SSH identity accepted: %v",err)}
}

func TestLiveISODiagnosticsRetainHostEvidenceWhenGuestUnavailable(t *testing.T){
    host,key,known:=runTestSSHServer(t)
    e:=testEngine(t,"")
    id:=registeredLocalSSH(t,e,host,key,known)
    result,err:=e.DiagnoseLiveISO(context.Background(),id)
    if err!=nil||result==nil||result.VMID!=id||result.Profile!="test"{
        t.Fatalf("managed diagnostic failed: %+v %v",result,err)
    }
    if _,err:=os.Stat(result.SnapshotFile);err!=nil{t.Fatalf("host Libvirt snapshot missing: %v",err)}
    if _,err:=e.DiagnoseLiveISO(context.Background(),"missing");err==nil{
        t.Fatal("diagnostic of unknown VM was accepted")
    }
    if _,err:=e.DiagnoseLiveISO(nil,id);err==nil{
        t.Fatal("nil context diagnostics accepted")
    }
}

func TestStartVMFromISOChecksProfileAndPreparedMedia(t *testing.T){
    e:=testEngine(t,"")
    if _,err:=e.StartVMFromISO(nil,LiveISOStartOptions{});err==nil{t.Fatal("nil context accepted")}
    if _,err:=e.StartVMFromISO(context.Background(),LiveISOStartOptions{});err==nil{
        t.Fatal("missing source profile accepted")
    }
    source:=filepath.Join(t.TempDir(),"installed.qcow2")
    if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
    if err:=e.RegisterProfile(Profile{Name:"prepared",MemoryMiB:512,VCPUs:1},source);err!=nil{t.Fatal(err)}
    _,err:=e.StartVMFromISO(context.Background(),LiveISOStartOptions{
        Profile:"prepared",ISOPath:filepath.Join(t.TempDir(),"missing.iso"),
    })
    if err==nil||!strings.Contains(err.Error(),"Live ISO unavailable"){
        t.Fatalf("missing ISO did not fail safely: %v",err)
    }
    _,err=e.StartVMWithOptions(context.Background(),VMStartOptions{Profile:"prepared",SSHReadyTimeout:-time.Second},nil)
    if err==nil||!strings.Contains(err.Error(),"SSHReadyTimeout"){t.Fatalf("negative SSH readiness allowed: %v",err)}
    _,err=e.StartVMWithOptions(context.Background(),VMStartOptions{Profile:"prepared",SSHReadyTimeout:2*time.Hour},nil)
    if err==nil||!strings.Contains(err.Error(),"SSHReadyTimeout"){t.Fatalf("excessive SSH readiness allowed: %v",err)}
    canceled,cancel:=context.WithCancel(context.Background())
    cancel()
    _,err=e.StartVMWithOptions(canceled,VMStartOptions{Profile:"prepared"},nil)
    if !errors.Is(err,context.Canceled){t.Fatalf("canceled guest creation accepted: %v",err)}
}
