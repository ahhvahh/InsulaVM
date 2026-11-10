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
    "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// The real workflow verifies boot on KVM. These tests separately verify every
// host-side stage, cleanup and forensic preservation using command adapters.
func isolatedLiveISOHost(t *testing.T)(*config.Config,string){
    t.Helper()
    root:=t.TempDir()
    bin:=filepath.Join(root,"bin")
    if err:=os.Mkdir(bin,0700);err!=nil{t.Fatal(err)}
    scripts:=map[string]string{
        "qemu-img":`case "$1" in create) printf 'QFI\\373scratch' > "$4" ;; *) exit 12;; esac`,
        "genisoimage":`while test "$#" -gt 0; do
  if test "$1" = "-output"; then shift; printf 'cidata' > "$1"; exit 0; fi
  shift
done
exit 16`,
        "virsh":`case "$3" in
 create) echo created ;;
 destroy) echo removed ;;
 domstate) echo running ;;
 domifaddr) echo "" ;;
 list) echo "" ;;
 *) echo mocked ;;
esac`,
    }
    for name,script:=range scripts{
        content:="#!/bin/sh\nset -eu\n"+script+"\n"
        if err:=os.WriteFile(filepath.Join(bin,name),[]byte(content),0700);err!=nil{t.Fatal(err)}
    }
    t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
    cfg:=&config.Config{
        Storage:config.Storage{
            Runs:filepath.Join(root,"runs"),
            Database:filepath.Join(root,"state","insula.db"),
            Logs:filepath.Join(root,"logs"),
        },
        Virtualization:config.Virtualization{LibvirtURI:"qemu:///system",Network:"default"},
        SSH:config.SSH{User:"insula",SudoMode:"disabled"},
    }
    iso:=filepath.Join(root,"prepared-live.iso")
    if err:=os.WriteFile(iso,[]byte("ISO fixture for host-side orchestration"),0600);err!=nil{t.Fatal(err)}
    return cfg,iso
}

func TestLiveISOHostPreservesDiagnosticsOnMissingDHCP(t *testing.T){
    cfg,iso:=isolatedLiveISOHost(t)
    var stages []string
    started,err:=StartLiveISOWithOptions(context.Background(),cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{
        ReadinessTimeout:50*time.Millisecond,
        PreserveOnFailure:true,CaptureSerial:true,
        OnProgress:func(stage,message string){stages=append(stages,stage)},
    })
    if started!=nil||err==nil||!strings.Contains(err.Error(),"diagnostic Live ISO VM preserved"){
        t.Fatalf("failed boot not preserved correctly: instance=%+v err=%v",started,err)
    }
    for _,want:=range []string{"preparing","seed","serial","domain","boot","dhcp-wait","failed"}{
        ok:=false
        for _,got:=range stages{if got==want{ok=true}}
        if !ok{t.Errorf("expected progress stage %q; got=%v",want,stages)}
    }
    store,openErr:=registry.Open(cfg.Storage.Database)
    if openErr!=nil{t.Fatal(openErr)}
    entries,listErr:=store.List(context.Background())
    _=store.Close()
    if listErr!=nil||len(entries)!=1||entries[0].Status!="failed"{
        t.Fatalf("missing failed VM history: %+v %v",entries,listErr)
    }
    if _,statErr:=os.Stat(entries[0].InstanceDir);statErr!=nil{
        t.Fatalf("preserve-on-failure removed VM overlay: %v",statErr)
    }
    logDir:=filepath.Join(cfg.Storage.Logs,"live-iso",entries[0].ID)
    for _,file:=range []string{"events.log","diagnostic.log","serial.log"}{
        if _,statErr:=os.Stat(filepath.Join(logDir,file));statErr!=nil{
            t.Errorf("host diagnostic file %s not preserved: %v",file,statErr)
        }
    }
    xmlBytes,readErr:=os.ReadFile(filepath.Join(entries[0].InstanceDir,"domain.xml"))
    if readErr!=nil||!strings.Contains(string(xmlBytes),"serial.log"){
        t.Fatalf("serial capture not in Libvirt XML: %s %v",xmlBytes,readErr)
    }
}

func TestLiveISOHostCleansTransientStorageOnFailedReadiness(t *testing.T){
    cfg,iso:=isolatedLiveISOHost(t)
    instance,err:=StartLiveISOWithOptions(context.Background(),cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{
        ReadinessTimeout:50*time.Millisecond,PreserveOnFailure:false,
    })
    if err==nil||instance!=nil||!strings.Contains(err.Error(),"DHCP"){
        t.Fatalf("unknown boot state accepted: %+v %v",instance,err)
    }
    store,openErr:=registry.Open(cfg.Storage.Database)
    if openErr!=nil{t.Fatal(openErr)}
    entries,listErr:=store.List(context.Background())
    _=store.Close()
    if listErr!=nil||len(entries)!=1||entries[0].Status!="failed"{
        t.Fatalf("failed boot not recorded: %+v %v",entries,listErr)
    }
    if _,statErr:=os.Stat(entries[0].InstanceDir);!os.IsNotExist(statErr){
        t.Fatalf("temporary VM files were not cleaned: %v",statErr)
    }
    for _,file:=range []string{"events.log","diagnostic.log"}{
        if _,statErr:=os.Stat(filepath.Join(cfg.Storage.Logs,"live-iso",entries[0].ID,file));statErr!=nil{
            t.Errorf("diagnostic %s removed: %v",file,statErr)
        }
    }
}

func TestLiveISOHostRejectsUntrustedStartupInputs(t *testing.T){
    cfg,iso:=isolatedLiveISOHost(t)
    if _,err:=StartLiveISO(nil,cfg,"prepared",iso,512,1,nil);err==nil{
        t.Fatal("nil context accepted")
    }
    if _,err:=StartLiveISO(context.Background(),nil,"prepared",iso,512,1,nil);err==nil{
        t.Fatal("nil host config accepted")
    }
    for _,opts:=range []struct{name,iso string;ram,cpus int;timeout time.Duration}{
        {"missing ISO",filepath.Join(t.TempDir(),"missing.iso"),512,1,time.Second},
        {"bad memory",iso,64,1,time.Second},
        {"no CPUs",iso,512,0,time.Second},
        {"negative timeout",iso,512,1,-time.Second},
    }{
        t.Run(opts.name,func(t *testing.T){
            _,err:=StartLiveISOWithOptions(context.Background(),cfg,"profile",opts.iso,opts.ram,opts.cpus,nil,LiveISOStartupOptions{
                ReadinessTimeout:opts.timeout,
            })
            if err==nil{t.Fatal("invalid host ISO startup input accepted")}
        })
    }
    cancelled,cancel:=context.WithCancel(context.Background())
    cancel()
    if _,err:=StartLiveISOWithOptions(cancelled,cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{});!errors.Is(err,context.Canceled){
        t.Fatalf("cancelled Live ISO accepted: %v",err)
    }
}
