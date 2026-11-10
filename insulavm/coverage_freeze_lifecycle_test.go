package insulavm

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/ahhvahh/InsulaVM/pkg/registry"
)

const coverageFrozenVMID = "019a5d11-1234-7123-8123-123456789abc"

// stoppedProfileFixture prepares an actual private SQLite registry, immutable
// profile disk and stopped VM overlay. Command adapters isolate host tooling;
// boot realism stays with the dedicated KVM integration workflows.
func stoppedProfileFixture(t *testing.T) (*Engine, string, string, string) {
    t.Helper()
    e:=testEngine(t,"")
    root:=t.TempDir()
    base:=filepath.Join(root,"source.qcow2")
    if err:=os.WriteFile(base,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
    if err:=e.RegisterProfile(Profile{Name:"installed",MemoryMiB:1024,VCPUs:1,DiskGB:8},base);err!=nil{t.Fatal(err)}
    dir:=filepath.Join(e.config.Storage.Runs,"insula-vm-"+coverageFrozenVMID)
    if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
    overlay:=filepath.Join(dir,"overlay.qcow2")
    if err:=os.WriteFile(overlay,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
    store,err:=registry.Open(e.config.Storage.Database)
    if err!=nil{t.Fatal(err)}
    entry:=registry.Entry{
        ID:coverageFrozenVMID,LibvirtUUID:coverageFrozenVMID,Name:"insula-installed",
        Profile:"installed",MAC:"52:54:00:aa:bb:cc",
        SSHUser:"insula",SudoMode:"disabled",InstanceDir:dir,
    }
    if err:=store.Insert(context.Background(),entry);err!=nil{t.Fatal(err)}
    if err:=store.SetRunning(context.Background(),entry.ID,"192.0.2.25");err!=nil{t.Fatal(err)}
    if err:=store.Close();err!=nil{t.Fatal(err)}
    bin:=t.TempDir()
    scripts:=map[string]string{
        "virsh": "case \"$3\" in\n"+
            "list) exit 0 ;;\n"+
            "*) echo unexpected-virsh-command >&2; exit 9 ;;\n"+
            "esac",
        "qemu-img": "case \"$1\" in\n"+
            "convert) for last; do :; done; printf 'QFI\\373standalone' > \"$last\" ;;\n"+
            "info) printf '{\"format\":\"qcow2\"}\\n' ;;\n"+
            "*) exit 17 ;;\n"+
            "esac",
        "guestmount": "for last; do :; done\n"+
            "mkdir -p \"$last/boot\"\n"+
            "printf 'kernel' > \"$last/boot/vmlinuz-6.12-unit\"\n"+
            "printf 'initrd' > \"$last/boot/initrd.img-6.12-unit\"",
        "guestunmount": "exit 0",
        "lsinitramfs": "printf 'scripts/live/boot\\n'",
        "mksquashfs": "printf 'squashfs' > \"$2\"",
        "grub-mkrescue": "test \"$1\" = '-o'\n"+
            "test -s \"$3/live/filesystem.squashfs\"\n"+
            "printf 'bootable-iso-fixture' > \"$2\"",
    }
    for name,script:=range scripts{
        writeISOTool(t,bin,name,script)
    }
    t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
    return e,dir,overlay,bin
}

func TestFreezeStoppedManagedVMProducesStandaloneQCOW2AndCleansSource(t *testing.T){
    e,dir,overlay,_:=stoppedProfileFixture(t)
    original,err:=os.ReadFile(overlay)
    if err!=nil{t.Fatal(err)}
    var stages []string
    result,err:=e.FreezeVM(context.Background(),VMFreezeRequest{
        VMID:coverageFrozenVMID,Name:"frozen-safely",
        SkipGuestCleanup:true,AllowStopped:true,
        OnProgress:func(p VMFreezeProgress){stages=append(stages,p.Stage)},
    })
    if err!=nil{t.Fatalf("stopped-overlay recovery failed: %v",err)}
    if result==nil||!result.SourceStopped||!result.SourceCleaned||result.ForcedShutdown||result.ImagePath==""{
        t.Fatalf("unsafe or incomplete freeze result: %+v",result)
    }
    frozen,err:=e.GetProfile("frozen-safely")
    if err!=nil{t.Fatal(err)}
    saved,err:=frozen.ImagePath()
    if err!=nil||saved!=result.ImagePath{t.Fatalf("independent QCOW2 missing: %s %v",saved,err)}
    if frozen.SSH.PrivateKey!=""||frozen.SSH.User!="insula"{t.Fatalf("SSH secrets leaked to frozen profile: %+v",frozen.SSH)}
    if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("temporary source was not removed: %v",err)}
    profileSource,err:=e.GetProfile("installed")
    if err!=nil{t.Fatal(err)}
    base,err:=profileSource.ImagePath()
    if err!=nil{t.Fatal(err)}
    baseData,err:=os.ReadFile(base)
    if err!=nil||string(baseData)!=string(original){t.Fatalf("source image modified: %v",err)}
    record,err:=e.GetVM(context.Background(),coverageFrozenVMID)
    if err!=nil||record.Status!="destroyed"{t.Fatalf("source VM state not recorded: %+v %v",record,err)}
    for _,stage:=range []string{"clean","shutdown","convert","registered","cleanup","ready"}{
        found:=false
        for _,actual:=range stages{if actual==stage{found=true}}
        if !found{t.Errorf("missing freeze progress stage %s: %v",stage,stages)}
    }
}

func TestFreezeStoppedVMISOExportWithNoGuestReinjection(t *testing.T){
    e,dir,_,_:=stoppedProfileFixture(t)
    destination:=filepath.Join(t.TempDir(),"frozen.iso")
    exported,err:=e.ExportVMToLiveISO(context.Background(),VMExportISORequest{
        VMID:coverageFrozenVMID,Name:"live-ready",
        OutputISOPath:destination,
        PrepareLiveBoot:false,SkipGuestCleanup:true,AllowStopped:true,
    })
    if err!=nil{t.Fatalf("freeze+ISO export failed: %v",err)}
    if exported==nil||exported.Freeze==nil||!exported.Freeze.SourceCleaned||exported.ISOPath!=destination{
        t.Fatalf("incomplete ISO export: %+v",exported)
    }
    data,err:=os.ReadFile(destination)
    if err!=nil||string(data)!="bootable-iso-fixture"{t.Fatalf("wrong exported ISO: %q %v",data,err)}
    if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("source VM was not cleaned: %v",err)}
    if _,err:=e.GetProfile("live-ready");err!=nil{t.Fatalf("frozen QCOW2 profile missing: %v",err)}
    if _,err:=e.ExportProfileLiveISO(context.Background(),"live-ready");err!=nil{
        t.Fatalf("independent profile ISO export failed: %v",err)
    }
}

func TestFreezeConversionFailurePreservesRecoveryOverlay(t *testing.T){
    e,dir,overlay,bin:=stoppedProfileFixture(t)
    writeISOTool(t,bin,"qemu-img","case \"$1\" in convert) echo conversion-failed >&2; exit 62 ;; *) exit 63 ;; esac")
    result,err:=e.FreezeVM(context.Background(),VMFreezeRequest{
        VMID:coverageFrozenVMID,Name:"will-not-publish",
        AllowStopped:true,SkipGuestCleanup:true,
    })
    if err==nil||!strings.Contains(err.Error(),"conversion-failed"){
        t.Fatalf("converter error lost: %+v %v",result,err)
    }
    if result==nil||!result.SourceStopped||result.SourceCleaned{
        t.Fatalf("failed freeze reported success: %+v",result)
    }
    if _,err:=os.Stat(overlay);err!=nil{t.Fatalf("source overlay was deleted: %v",err)}
    if _,err:=os.Stat(dir);err!=nil{t.Fatalf("source VM files were deleted: %v",err)}
    if _,err:=e.GetProfile("will-not-publish");err==nil{
        t.Fatal("failed QCOW2 convert published a profile")
    }
}

func TestFreezeValidationRejectsUnsafeRecovery(t *testing.T){
    e,_,_,_:=stoppedProfileFixture(t)
    for _,tc:=range []struct{name string;request VMFreezeRequest;want string}{
        {"empty vm",VMFreezeRequest{Name:"out"},"VM ID"},
        {"invalid name",VMFreezeRequest{VMID:coverageFrozenVMID,Name:"../unsafe"},"invalid freeze name"},
        {"output without export",VMFreezeRequest{VMID:coverageFrozenVMID,Name:"out",OutputISOPath:"/tmp/out.iso"},"requires ExportISO"},
        {"negative shutdown timeout",VMFreezeRequest{VMID:coverageFrozenVMID,Name:"out",ShutdownTimeout:-time.Second},"shutdown timeout"},
        {"not explicitly recovered",VMFreezeRequest{VMID:coverageFrozenVMID,Name:"out"},"explicitly recoverable"},
        {"requires cleanup waiver",VMFreezeRequest{VMID:coverageFrozenVMID,Name:"out",AllowStopped:true},"explicitly recoverable"},
    }{
        t.Run(tc.name,func(t *testing.T){
            _,err:=e.FreezeVM(context.Background(),tc.request)
            if err==nil||!strings.Contains(err.Error(),tc.want){t.Fatalf("wanted %q, got %v",tc.want,err)}
        })
    }
    if _,err:=e.FreezeVM(nil,VMFreezeRequest{});err==nil{t.Fatal("nil context accepted")}
    ctx,cancel:=context.WithCancel(context.Background())
    cancel()
    if _,err:=e.FreezeVM(ctx,VMFreezeRequest{});!errors.Is(err,context.Canceled){
        t.Fatalf("cancelled freeze accepted: %v",err)
    }
    for _,req:=range []VMExportISORequest{
        {},{VMID:coverageFrozenVMID,Name:"unsafe",CommandTimeout:-time.Second},
        {VMID:coverageFrozenVMID,Name:"../unsafe"},
        {VMID:coverageFrozenVMID,Name:"out",PrepareLiveBoot:true,AllowStopped:true},
    }{
        if _,err:=e.ExportVMToLiveISO(context.Background(),req);err==nil{
            t.Fatalf("unsafe ISO export accepted: %+v",req)
        }
    }
}
