package main

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/ahhvahh/InsulaVM/pkg/registry"
    "github.com/ahhvahh/InsulaVM/insulavm"
)

const coverageCLIUUID = "019a5d11-1234-7123-8123-123456789abc"

func registeredCLIContext(t *testing.T) (string,string,string) {
    t.Helper()
    e,cfg:=coverageCLIEngine(t)
    original:=filepath.Join(t.TempDir(),"installed.qcow2")
    if err:=os.WriteFile(original,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
    if err:=e.RegisterProfile(structProfileForCLI(),original);err!=nil{t.Fatal(err)}
    directory:=filepath.Join(e.Configuration().Storage.Runs,"insula-vm-"+coverageCLIUUID)
    if err:=os.MkdirAll(directory,0700);err!=nil{t.Fatal(err)}
    store,err:=registry.Open(e.Configuration().Storage.Database)
    if err!=nil{t.Fatal(err)}
    record:=registry.Entry{
        ID:coverageCLIUUID,LibvirtUUID:coverageCLIUUID,Name:"insula-cloud-managed",
        Profile:"cloud-managed",SSHUser:"insula",SudoMode:"disabled",
        IP:"192.0.2.30",MAC:"52:54:00:12:34:56",
        InstanceDir:directory,PrivateKeyPath:"/private/ssh.key",
        KnownHostsPath:"/private/known_hosts",SSHPort:22,
    }
    if err:=store.Insert(context.Background(),record);err!=nil{t.Fatal(err)}
    if err:=store.SetRunning(context.Background(),coverageCLIUUID,record.IP);err!=nil{t.Fatal(err)}
    if err:=store.Close();err!=nil{t.Fatal(err)}
    bin:=t.TempDir()
    virsh:="#!/bin/sh\ncase \"$3\" in\n"+
        "list) printf '%s\\n' '"+coverageCLIUUID+"' ;;\n"+
        "domstate) printf 'running\\n' ;;\n"+
        "shutdown) printf 'requested\\n' ;;\n"+
        "domifaddr) printf 'vnet0 52:54:00:12:34:56 ipv4 192.0.2.30/24 guest\\n' ;;\n"+
        "net-dhcp-leases) printf '52:54:00:12:34:56 192.0.2.30\\n' ;;\n"+
        "*) printf 'mocked libvirt query\\n' ;;\n"+
        "esac\n"
    if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(virsh),0700);err!=nil{t.Fatal(err)}
    t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
    return cfg,coverageCLIUUID,directory
}

func structProfileForCLI() insulavm.Profile {
    return insulavm.Profile{Name:"cloud-managed",MemoryMiB:1024,VCPUs:1,DiskGB:8}
}

func TestCLIManagedVMReadOnlyOperationsWithoutKVM(t *testing.T){
    cfg,id,dir:=registeredCLIContext(t)
    for _,args:=range [][]string{
        {"vm","list","-config",cfg},
        {"vm","status","-config",cfg,"-id",id},
        {"vm","diagnose","-config",cfg,"-id",id},
        {"vm","stop-all","-config",cfg,"-dry-run"},
        {"profile","list","-config",cfg},
        {"profile","show","-config",cfg,"-name","cloud-managed"},
    }{
        if err:=run(args);err!=nil{
            t.Errorf("CLI read-only command %v failed: %v",args,err)
        }
    }
    if _,err:=os.Stat(dir);err!=nil{t.Fatalf("read-only CLI changed VM storage: %v",err)}
}

func TestCLIRefusesUnsafeManagedVMChanges(t *testing.T){
    cfg,id,dir:=registeredCLIContext(t)
    for _,tc:=range []struct{name string;args []string;want string}{
        {"sudo unavailable",[]string{"vm","exec","-config",cfg,"-id",id,"-command","whoami","-sudo"},"sudo"},
        {"freeze without passwordless sudo",[]string{"vm","freeze","-config",cfg,"-id",id,"-name","frozen"},"passwordless"},
        {"ISO export without sudo",[]string{"vm","export-iso","-config",cfg,"-id",id,"-name","will-not-export","-output",filepath.Join(t.TempDir(),"planned.iso"),"-prepare-live-boot=false"},"passwordless"},
        {"stop unknown id",[]string{"vm","stop","-config",cfg,"-id","unknown"},"not found"},
        {"status unknown id",[]string{"vm","status","-config",cfg,"-id","unknown"},"not found"},
        {"start unknown profile",[]string{"vm","start","-config",cfg,"-profile","missing"},"no such file"},
    }{
        t.Run(tc.name,func(t *testing.T){
            err:=run(tc.args)
            if err==nil||!strings.Contains(strings.ToLower(err.Error()),strings.ToLower(tc.want)){
                t.Fatalf("%v: want %q, got %v",tc.args,tc.want,err)
            }
        })
    }
    if _,err:=os.Stat(dir);err!=nil{t.Fatalf("failed CLI operation removed running VM: %v",err)}
}
