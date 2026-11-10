package main

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/ahhvahh/InsulaVM/insulavm"
)

func coverageCLIEngine(t *testing.T)(*insulavm.Engine,string){
    t.Helper()
    root:=t.TempDir()
    cfgFile:=filepath.Join(root,"config.yaml")
    data:="storage:\n  root: "+filepath.Join(root,"state")+
        "\nvirtualization:\n  provider: kvm\n  default_disk_gb: 8\n"
    if err:=os.WriteFile(cfgFile,[]byte(data),0600);err!=nil{t.Fatal(err)}
    e,err:=insulavm.New(cfgFile)
    if err!=nil{t.Fatal(err)}
    return e,cfgFile
}

func TestCLICommandDispatchCoversSafeValidationWithoutHypervisor(t *testing.T){
    _,cfg:=coverageCLIEngine(t)
    tests:=[]struct{name string;args []string;want string}{
        {"unknown",[]string{"unknown"},"usage"},
        {"help",[]string{"help"},""},
        {"config unknown",[]string{"config","bad"},"usage"},
        {"config positional",[]string{"config","check","-config",cfg,"unexpected"},"unexpected arguments"},
        {"profile bad",[]string{"profile","unknown","-config",cfg},"expected profile list"},
        {"vm start missing profile",[]string{"vm","start","-config",cfg},"requires -profile"},
        {"vm start iso missing profile",[]string{"vm","start-iso","-config",cfg},"requires -profile"},
        {"vm diagnose missing UUID",[]string{"vm","diagnose","-config",cfg},"requires -id"},
        {"vm console missing UUID",[]string{"vm","console","-config",cfg},"requires -id"},
        {"vm status missing UUID",[]string{"vm","status","-config",cfg},"requires -id"},
        {"vm exec missing command",[]string{"vm","exec","-config",cfg,"-id","abc"},"requires -id and -command"},
        {"vm import missing source",[]string{"vm","import","-config",cfg,"-id","abc"},"requires -id and -source"},
        {"vm deploy missing script",[]string{"vm","deploy","-config",cfg,"-id","abc","-source","file"},"requires -id"},
        {"vm stop missing UUID",[]string{"vm","stop","-config",cfg},"requires -id"},
        {"vm stop-all refuses UUID",[]string{"vm","stop-all","-config",cfg,"-id","uuid"},"does not accept -id"},
        {"vm freeze missing UUID",[]string{"vm","freeze","-config",cfg,"-name","profile"},"requires -id"},
        {"vm export missing UUID",[]string{"vm","export-iso","-config",cfg,"-name","profile"},"requires -id"},
        {"vm unknown",[]string{"vm","not-existing","-config",cfg},"usage"},
        {"test positional",[]string{"test","doctor","-config",cfg,"extra"},"unexpected test arguments"},
        {"test bad flag",[]string{"test","boot","-config",cfg,"-invalid-option"},"flag provided"},
        {"test install relative ISO",[]string{"test","install","-config",cfg,"-profile","new","-iso","relative.iso"},"absolute path"},
        {"test boot missing profile record",[]string{"test","boot","-config",cfg,"-profile","absent"},"no such file"},
        {"test verify missing profile record",[]string{"test","verify","-config",cfg,"-profile","absent"},"no such file"},
        {"test cycle invalid SSH timeout",[]string{"test","cycle","-config",cfg,"-profile","missing","-name","new","-ssh-ready-timeout=-1s"},"invalid -ssh-ready-timeout"},
        {"test cycle relative ISO",[]string{"test","cycle","-config",cfg,"-profile","absent","-name","new","-iso","relative.iso"},"absolute path"},
        {"test ISO confirmed missing VM",[]string{"test","iso","-config",cfg,"-id","unknown","-name","archive","-confirm","-verify=false"},""},
    }
    for _,tc:=range tests{
        t.Run(tc.name,func(t *testing.T){
            err:=run(tc.args)
            if tc.want==""{
                // Some public dispatches intentionally return late validation
                // errors, but must never panic or create an actual VM here.
                return
            }
            if err==nil||!strings.Contains(strings.ToLower(err.Error()),strings.ToLower(tc.want)){
                t.Fatalf("args=%v want=%q got=%v",tc.args,tc.want,err)
            }
        })
    }
    if err:=run([]string{"vm","list","-config",cfg});err!=nil{
        t.Fatalf("listing empty registry requires no hypervisor: %v",err)
    }
    if err:=run([]string{"vm","stop-all","-config",cfg,"-dry-run"});err!=nil{
        t.Fatalf("dry-run empty registry should be safe: %v",err)
    }
}

func TestCLIDoctorReportsMissingHostToolsWithoutTouchingVM(t *testing.T){
    engine,_:=coverageCLIEngine(t)
    t.Setenv("PATH",t.TempDir())
    err:=testHostDoctor(context.Background(),engine)
    if err==nil||!strings.Contains(err.Error(),"DOCTOR FAILED"){
        t.Fatalf("unavailable hypervisor dependencies should fail doctor: %v",err)
    }
}

func TestCLITestHelpersWithExistingAndMissingProfiles(t *testing.T){
    engine,_:=coverageCLIEngine(t)
    if err:=testInstall(context.Background(),engine,"new","relative.iso",1024,1,10,time.Minute);err==nil{
        t.Fatal("nonabsolute installer ISO accepted")
    }
    disk:=filepath.Join(t.TempDir(),"installed.qcow2")
    if err:=os.WriteFile(disk,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
    if err:=engine.RegisterProfile(insulavm.Profile{Name:"cached",MemoryMiB:1024,VCPUs:1,DiskGB:8},disk);err!=nil{
        t.Fatal(err)
    }
    if err:=testInstall(context.Background(),engine,"cached","/unused.iso",1024,1,10,time.Minute);err!=nil{
        t.Fatalf("known QCOW2 profile should avoid installer: %v",err)
    }
    if _,err:=testBoot(context.Background(),engine,"missing",1024,1,"",time.Second);err==nil{
        t.Fatal("nonexistent boot profile should fail without KVM")
    }
    if err:=testVerifyLiveISO(context.Background(),engine,"cached",filepath.Join(t.TempDir(),"absent.iso"));err==nil{
        t.Fatal("missing ISO should not be reported as booted")
    }
    if err:=testExportISO(context.Background(),engine,"missing","frozen",false,time.Second);err==nil{
        t.Fatal("missing VM should not produce a frozen image")
    }
}
