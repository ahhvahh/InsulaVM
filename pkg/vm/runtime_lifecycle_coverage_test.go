package vm

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/ahhvahh/InsulaVM/pkg/config"
    "github.com/ahhvahh/InsulaVM/pkg/profile"
    "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestVMStartProgressReachesSSHAndPreservesRootCause(t *testing.T){
    cfg,_:=isolatedLiveISOHost(t)
    source:=t.TempDir()
    image:=filepath.Join(source,"base.qcow2")
    if err:=os.WriteFile(image,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
    p:=&profile.Profile{
        Name:"cloud",Version:"v001",Directory:source,Image:"base.qcow2",
        MemoryMiB:512,VCPUs:1,DiskGB:8,
    }
    // The qemu-img adapter for the ordinary VM's overlay uses the final
    // argument, unlike the Live ISO scratch create syntax.
    bin:=t.TempDir()
    scripts:=map[string]string{
        "qemu-img":`for last; do :; done
printf 'QFI\\373overlay' > "$last"`,
        "virsh":`case "$3" in
create) exit 0 ;;
destroy) exit 0 ;;
list) echo "" ;;
domstate) echo running ;;
domifaddr) echo "vnet0 52:54:00:11:22:33 ipv4 192.0.2.44/24 guest" ;;
*) exit 17 ;;
esac`,
    }
    for name,script:=range scripts{
        if err:=os.WriteFile(filepath.Join(bin,name),[]byte("#!/bin/sh\nset -eu\n"+script+"\n"),0700);err!=nil{t.Fatal(err)}
    }
    t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
    ctx,cancel:=context.WithTimeout(context.Background(),180*time.Millisecond)
    defer cancel()
    var stages []string
    instance,err:=StartWithStartupOptions(ctx,cfg,p,nil,StartupOptions{PreserveOnFailure:true},func(event Progress){
        stages=append(stages,event.Stage)
    })
    if instance!=nil||err==nil||!errors.Is(err,context.DeadlineExceeded){
        t.Fatalf("SSH readiness deadline should preserve original cause: %+v %v",instance,err)
    }
    for _,stage:=range []string{"profile","profile-ok","preparing","cloud-init","disk","registered","running","dhcp","ip","ssh","failed"}{
        found:=false
        for _,got:=range stages{if got==stage{found=true}}
        if !found{t.Errorf("missing VM boot state %s in %v",stage,stages)}
    }
    store,openErr:=registry.Open(cfg.Storage.Database)
    if openErr!=nil{t.Fatal(openErr)}
    rows,listErr:=store.List(context.Background())
    _=store.Close()
    if listErr!=nil||len(rows)!=1||rows[0].Status!="failed"{
        t.Fatalf("failed VM status was not recorded: %+v %v",rows,listErr)
    }
    if _,statErr:=os.Stat(rows[0].InstanceDir);statErr!=nil{
        t.Fatalf("preserved VM overlay missing: %v",statErr)
    }
}

func TestStopByIDDisposesOnlyRegisteredVMStorage(t *testing.T){
    root:=t.TempDir()
    cfg:=&config.Config{
        Storage:config.Storage{
            Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"db","insula.db"),
        },
        Virtualization:config.Virtualization{LibvirtURI:"qemu:///system"},
    }
    const id="019a5d11-1234-7123-8123-123456789abc"
    dir:=filepath.Join(cfg.Storage.Runs,"insula-vm-"+id)
    if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(filepath.Join(dir,"overlay.qcow2"),[]byte("sample"),0600);err!=nil{t.Fatal(err)}
    store,err:=registry.Open(cfg.Storage.Database)
    if err!=nil{t.Fatal(err)}
    entry:=registry.Entry{
        ID:id,LibvirtUUID:id,Name:"insula-cloud",Profile:"cloud",MAC:"52:54:00:aa:bb:cc",
        SSHUser:"insula",SudoMode:"disabled",InstanceDir:dir,
    }
    if err:=store.Insert(context.Background(),entry);err!=nil{t.Fatal(err)}
    if err:=store.SetRunning(context.Background(),id,"192.168.122.23");err!=nil{t.Fatal(err)}
    _=store.Close()
    fakeVirshForStatus(t,`case "$3" in
list) echo '019a5d11-1234-7123-8123-123456789abc' ;;
domstate) echo running ;;
destroy) echo destroyed ;;
*) exit 7 ;;
esac`)
    if err:=StopByID(context.Background(),cfg,id);err!=nil{t.Fatalf("registered VM cleanup failed: %v",err)}
    if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("VM directory survived: %v",err)}
    store,err=registry.Open(cfg.Storage.Database)
    if err!=nil{t.Fatal(err)}
    updated,err:=store.Get(context.Background(),id)
    _=store.Close()
    if err!=nil||updated.Status!="destroyed"{t.Fatalf("VM history must record removal: %+v %v",updated,err)}
}

func TestStopByIDRejectsUnknownOrUnsafeDomain(t *testing.T){
    root:=t.TempDir()
    cfg:=&config.Config{
        Storage:config.Storage{Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"db","insula.db")},
        Virtualization:config.Virtualization{LibvirtURI:"qemu:///system"},
    }
    if err:=StopByID(nil,cfg,"missing");err==nil{t.Fatal("nil context accepted")}
    if err:=StopByID(context.Background(),cfg,"missing");err==nil{t.Fatal("unknown VM accepted")}
    const id="019a5d11-1234-7123-8123-123456789abc"
    outside:=filepath.Join(t.TempDir(),"important")
    if err:=os.MkdirAll(outside,0700);err!=nil{t.Fatal(err)}
    store,err:=registry.Open(cfg.Storage.Database)
    if err!=nil{t.Fatal(err)}
    if err:=store.Insert(context.Background(),registry.Entry{
        ID:id,LibvirtUUID:id,Name:"vm",Profile:"cloud",MAC:"52:54:00:00:00:12",
        SSHUser:"insula",SudoMode:"disabled",InstanceDir:outside,
    });err!=nil{t.Fatal(err)}
    if err:=store.SetRunning(context.Background(),id,"192.168.122.24");err!=nil{t.Fatal(err)}
    _=store.Close()
    fakeVirshForStatus(t,`case "$3" in
list) echo '019a5d11-1234-7123-8123-123456789abc' ;;
domstate) echo running ;;
destroy) exit 99 ;;
*) exit 4 ;;
esac`)
    err=StopByID(context.Background(),cfg,id)
    if err==nil||!strings.Contains(err.Error(),"unsafe registered VM directory"){
        t.Fatalf("unsafe registered directory not rejected: %v",err)
    }
    if _,err:=os.Stat(outside);err!=nil{t.Fatalf("foreign directory was touched: %v",err)}
}
