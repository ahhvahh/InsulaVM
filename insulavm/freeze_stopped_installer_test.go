package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestFreezeStoppedInstallerWithoutGuestCleanup(t *testing.T){
 root:=t.TempDir()
 c:=config.Config{}
 c.Storage.Runs=filepath.Join(root,"runs")
 c.Storage.Profiles=filepath.Join(root,"profiles")
 c.Storage.Database=filepath.Join(root,"insula.db")
 c.Virtualization.LibvirtURI="qemu:///system"
 c.Guest.Destination="/tmp/workspace"
 const id="019a5d11-1234-7123-8123-123456789abc"
 name:="insula-alpine-virt-"+id
 instanceDir:=filepath.Join(c.Storage.Runs,"insula-vm-saved")
 if err:=os.MkdirAll(instanceDir,0700);err!=nil{t.Fatal(err)}
 disk:=filepath.Join(instanceDir,"installer.qcow2")
 if err:=os.WriteFile(disk,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 installerDir:=filepath.Join(c.Storage.Profiles,"alpine-virt")
 if err:=os.MkdirAll(installerDir,0700);err!=nil{t.Fatal(err)}
 yaml:=`name: alpine-virt
os: alpine
hardware:
  cpu:
    quantity: 1
  ram:
    mib: 500
disk:
  start_iso: /opt/alpine.iso
  size_gb: 8
network:
  enabled: true
`
 if err:=os.WriteFile(filepath.Join(installerDir,"profile.yaml"),[]byte(yaml),0600);err!=nil{t.Fatal(err)}
 xml:=`<domain type="kvm"><name>`+name+`</name><uuid>`+id+`</uuid><devices><disk type="file" device="disk"><source file="`+disk+`"/><target dev="vda" bus="virtio"/></disk></devices></domain>`
 if err:=os.WriteFile(filepath.Join(instanceDir,"domain.xml"),[]byte(xml),0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(c.Storage.Database)
 if err!=nil{t.Fatal(err)}
 if err:=store.Insert(context.Background(),registry.Entry{ID:id,LibvirtUUID:id,Name:name,Profile:"alpine-virt",InstanceDir:instanceDir});err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(context.Background(),id,"");err!=nil{t.Fatal(err)}
 _=store.Close()
 marker:=filepath.Join(root,"defined")
 if err:=os.WriteFile(marker,[]byte("yes"),0600);err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 virsh:=`#!/bin/sh
set -eu
case "$3" in
 list) if [ -f "$MOCK_DEFINED" ]; then echo "$MOCK_UUID"; fi ;;
 domstate) echo 'shut off' ;;
 dumpxml) cat "$MOCK_DOMAIN_XML" ;;
 undefine) rm -f "$MOCK_DEFINED" ;;
 *) echo "unexpected virsh $3" >&2; exit 42 ;;
esac
`
 qemu:=`#!/bin/sh
set -eu
case "$1" in
 convert)
  shift
  while [ "$#" -gt 2 ]; do shift; done
  cp "$1" "$2" ;;
 info) printf '{"format":"qcow2"}\n' ;;
 *) exit 43 ;;
esac
`
 for file,script:=range map[string]string{"virsh":virsh,"qemu-img":qemu}{
  if err:=os.WriteFile(filepath.Join(bin,file),[]byte(script),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("MOCK_DEFINED",marker)
 t.Setenv("MOCK_UUID",id)
 t.Setenv("MOCK_DOMAIN_XML",filepath.Join(instanceDir,"domain.xml"))
 engine:=&Engine{config:c,configDir:root}
 _,err=engine.FreezeVM(context.Background(),VMFreezeRequest{VMID:id,Name:"abioma-test"})
 if err==nil||!strings.Contains(err.Error(),"-recover-stopped"){
  t.Fatalf("must require recovery opt-in: %v",err)
 }
 if _,err:=os.Stat(disk);err!=nil{t.Fatalf("disk lost before authorized recovery: %v",err)}
 result,err:=engine.FreezeVM(context.Background(),VMFreezeRequest{VMID:id,Name:"abioma-test",AllowStopped:true})
 if err!=nil{t.Fatal(err)}
 if !result.SourceStopped||!result.SourceCleaned||result.ForcedShutdown{
  t.Fatalf("wrong recovery state: %+v",result)
 }
 saved,err:=engine.GetProfile("abioma-test")
 if err!=nil{t.Fatal(err)}
 if saved.BootMode!="direct"{t.Fatalf("expected boot_mode=direct: %+v",saved)}
 if _,err:=os.Stat(result.ImagePath);err!=nil{t.Fatalf("missing reusable QCOW2: %v",err)}
 if _,err:=os.Stat(disk);!os.IsNotExist(err){t.Fatalf("original disk still present: %v",err)}
 store,err=registry.Open(c.Storage.Database)
 if err!=nil{t.Fatal(err)}
 entry,err:=store.Get(context.Background(),id)
 _=store.Close()
 if err!=nil||entry.Status!="destroyed"{t.Fatalf("source not destroyed: %+v %v",entry,err)}
}
