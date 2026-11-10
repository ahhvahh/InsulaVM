package vm

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

func TestStatusLeaseWithRealVirshAdapter(t *testing.T){
    fakeVirshForStatus(t,`#!/bin/sh
case "$3" in
  domifaddr) printf '%s\n' 'vnet0 ipv4 192.168.122.44/24 guest' ;;
  domstate) printf 'blocked\n' ;;
  list) printf '%s\n' '019a5d11-1234-7123-8123-123456789abc' ;;
  *) exit 11 ;;
esac
`)
    ip,err:=LeaseIPv4(context.Background(),"qemu:///system",runningID)
    if err!=nil||ip!="192.168.122.44"{t.Fatalf("wrong lease %q err=%v",ip,err)}
    state,err:=ActiveDomainState(context.Background(),"qemu:///system",runningID)
    if err!=nil||state!="blocked"{t.Fatalf("blocked VM should be active: %q %v",state,err)}
    infos,err:=InspectRegistered(context.Background(),"qemu:///system",[]registry.Entry{
        {ID:"vm",LibvirtUUID:runningID,Status:"running"},
    })
    if err!=nil||len(infos)!=1||!infos[0].IsRunning(){t.Fatalf("blocked domain not recognized: %+v %v",infos,err)}
}

func TestStatusValidatesLibvirtFailures(t *testing.T){
    if _,err:=LeaseIPv4(nil,"qemu:///system","domain");err==nil{t.Fatal("nil context accepted")}
    if _,err:=LeaseIPv4(context.Background(),"qemu:///system"," ");err==nil{t.Fatal("blank domain accepted")}
    if _,err:=ActiveDomainState(nil,"qemu:///system",runningID);err==nil{t.Fatal("nil context accepted")}
    if _,err:=InspectRegistered(nil,"qemu:///system",nil);err==nil{t.Fatal("nil context accepted")}
    cancelled,cancel:=context.WithCancel(context.Background())
    cancel()
    if _,err:=InspectRegistered(cancelled,"qemu:///system",[]registry.Entry{{LibvirtUUID:runningID}});!errors.Is(err,context.Canceled){
        t.Fatalf("cancelled status returned %v",err)
    }
    fakeVirshForStatus(t,`case "$3" in
domifaddr) printf 'vnet0 ipv6 fe80::1/64 guest\n' ;;
domstate) printf '   \n' ;;
list) printf '%s\n' '019a5d11-1234-7123-8123-123456789abc' ;;
*) exit 3 ;;
esac`)
    if _,err:=LeaseIPv4(context.Background(),"qemu:///system","vm");err==nil||!strings.Contains(err.Error(),"no IPv4"){
        t.Fatalf("IPv6 lease was incorrectly accepted: %v",err)
    }
    infos,err:=InspectRegistered(context.Background(),"qemu:///system",[]registry.Entry{{LibvirtUUID:runningID,Status:"running"}})
    if err!=nil||len(infos)!=1||infos[0].LibvirtState!="unknown"{t.Fatalf("empty state should be unknown: %+v %v",infos,err)}
    if _,err:=ActiveDomainState(context.Background(),"qemu:///system",runningID);err==nil{t.Fatal("empty state should stop startup")}
}

func TestStatusRejectsVirshCommandErrors(t *testing.T){
    fakeVirshForStatus(t,`case "$3" in
list) printf '%s\n' '019a5d11-1234-7123-8123-123456789abc' ;;
*) echo 'backend rejected request' >&2; exit 11 ;;
esac`)
    if _,err:=LeaseIPv4(context.Background(),"qemu:///system","vm");err==nil||!strings.Contains(err.Error(),"backend rejected"){
        t.Fatalf("missing DHCP error detail: %v",err)
    }
    if _,err:=ActiveDomainState(context.Background(),"qemu:///system",runningID);err==nil||!strings.Contains(err.Error(),"backend rejected"){
        t.Fatalf("missing state error detail: %v",err)
    }
    infos,err:=InspectRegistered(context.Background(),"qemu:///system",[]registry.Entry{
        {ID:runningID,LibvirtUUID:runningID,Status:"running"},
    })
    if err!=nil||len(infos)!=1||infos[0].LibvirtState!="unknown"||!strings.Contains(infos[0].Warning,"backend rejected"){
        t.Fatalf("domstate failure should remain unknown: %+v %v",infos,err)
    }
}

func TestQEMUAccessGuardsAndPermissions(t *testing.T){
    if err:=GrantQEMUInstallAccess(nil,0,"/missing.iso","/missing.qcow2");err!=nil{
        t.Fatalf("zero UID is explicit no-op: %v",err)
    }
    for _,uid:=range []int{-1,2147483648}{
        if err:=GrantQEMUInstallAccess(context.Background(),uid,"/missing.iso","/missing.qcow2");err==nil{
            t.Fatalf("invalid uid %d was accepted",uid)
        }
    }
    if err:=GrantQEMUInstallAccess(nil,123,"/missing.iso","/missing.qcow2");err==nil{
        t.Fatal("nonroot ACL request with nil context was accepted")
    }
    for _,tc:=range []struct{mode os.FileMode;permission string;allowed bool}{
        {0600,"r--",true},{0400,"rw-",false},{0200,"rw-",false},{0600,"rw-",true},
        {0000,"r--",false},{0000,"---",true},
    }{
        if got:=fileHasOwnerPermission(tc.mode,tc.permission);got!=tc.allowed{
            t.Fatalf("mode %04o perm=%s accepted=%t wanted=%t",tc.mode,tc.permission,got,tc.allowed)
        }
    }
    base:=filepath.Join(t.TempDir(),"file.qcow2")
    if err:=os.WriteFile(base,[]byte("sample"),0600);err!=nil{t.Fatal(err)}
    stat,err:=os.Stat(base)
    if err!=nil{t.Fatal(err)}
    if !ownedBy(stat,os.Geteuid()){t.Fatal("local fixture ownership was not recognized")}
    if err:=GrantQEMUInstallAccess(context.Background(),os.Geteuid(),base,base);err!=nil{
        t.Fatalf("already owned disk should not need extra permissions: %v",err)
    }
    if err:=GrantQEMUInstallAccess(context.Background(),os.Geteuid(),base,filepath.Join(t.TempDir(),"missing.qcow2"));err==nil{
        t.Fatal("missing QCOW2 file was accepted")
    }
}

func TestQEMUACLFailsWithoutSetfacl(t *testing.T){
    if os.Geteuid()==0{t.Skip("test requires an ordinary user")}
    bin:=t.TempDir()
    t.Setenv("PATH",bin)
    if err:=GrantQEMUInstallAccess(context.Background(),os.Geteuid(),"/missing.iso","/missing.qcow2");err==nil||
        !strings.Contains(err.Error(),"setfacl"){t.Fatalf("ACL dependency not reported: %v",err)}
}

func TestShutdownWithACPIRequiresKnownRunningDomain(t *testing.T){
    if err:=ShutdownWithMode(context.Background(),"qemu:///system",runningID,time.Second,"invalid");err==nil{
        t.Fatal("unsupported shutdown mode accepted")
    }
    if err:=ShutdownWithMode(nil,"qemu:///system",runningID,time.Second,"acpi");err==nil{
        t.Fatal("nil context accepted")
    }
    if err:=ShutdownWithMode(context.Background(),"qemu:///system",runningID,-time.Second,"acpi");err==nil{
        t.Fatal("nonpositive grace period accepted")
    }
    fakeVirshForStatus(t,`case "$3" in
list) echo '019a5d11-1234-7123-8123-123456789abc' ;;
domstate) echo 'paused' ;;
*) exit 2 ;;
esac`)
    err:=ShutdownWithMode(context.Background(),"qemu:///system",runningID,time.Second,"acpi")
    if err==nil||!strings.Contains(err.Error(),"must be running"){t.Fatalf("paused domain accepted for ACPI: %v",err)}
}

func TestShutdownWithACPIDoesNotForceGuest(t *testing.T){
    root:=t.TempDir()
    state:=filepath.Join(root,"running")
    log:=filepath.Join(root,"operations")
    if err:=os.WriteFile(state,[]byte("on"),0600);err!=nil{t.Fatal(err)}
    fakeVirshForStatus(t,`#!/bin/sh
case "$3" in
list) if test -f "$MOCK_RUNNING"; then echo "$MOCK_UUID"; fi ;;
domstate) printf 'running\n' ;;
shutdown) printf '%s\n' "$@" >> "$MOCK_COMMANDS"; rm -f "$MOCK_RUNNING" ;;
destroy) printf 'FORCED\n' >> "$MOCK_COMMANDS"; exit 99 ;;
*) exit 4 ;;
esac`)
    t.Setenv("MOCK_RUNNING",state)
    t.Setenv("MOCK_UUID",runningID)
    t.Setenv("MOCK_COMMANDS",log)
    if err:=ShutdownWithMode(context.Background(),"qemu:///system",runningID,2*time.Second,"acpi");err!=nil{
        t.Fatalf("graceful ACPI shutdown failed: %v",err)
    }
    trace,err:=os.ReadFile(log)
    if err!=nil{t.Fatal(err)}
    if !strings.Contains(string(trace),"--mode\nacpi")||strings.Contains(string(trace),"FORCED"){
        t.Fatalf("unexpected Libvirt shutdown arguments: %s",trace)
    }
}
