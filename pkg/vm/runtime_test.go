package vm

import (
 "encoding/xml"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/config"
 "path/filepath"
 "os"
 "context"
 "strings"
 "testing"
)

func TestParseIPv4Lease(t *testing.T){
 table:="Name       MAC address          Protocol Address         Hostname\n"+
  "vnet0      52:54:00:12:34:56  ipv4     192.168.122.27/24  test\n"
 if got:=parseIPv4(table);got!="192.168.122.27"{t.Fatalf("got %q",got)}
 if got:=parseIPv4("no IPv4 lease");got!=""{t.Fatalf("unexpected IP %q",got)}
}

func TestDomainXMLAttributesAreEscaped(t *testing.T){
 result:=domainXML("vm",512,1,"/tmp/guest's.qcow2","default","52:54:00:aa:bb:cc")
 if !strings.Contains(result,"guest&#39;s.qcow2") && !strings.Contains(result,"guest&apos;s.qcow2"){t.Fatalf("unescaped attribute: %s",result)}
 if !strings.Contains(result,"<memory unit='MiB'>512</memory>"){t.Fatal("RAM not present")}
 if !strings.Contains(result,"<vcpu>1</vcpu>"){t.Fatal("vCPU not present")}
}

func TestCloudInitSeedIsAttachedToDomain(t *testing.T){
 spec:=domainXMLWithSeed("019a5d11-1234-7123-8123-123456789abc",
  "insula-secure",1024,1,"/tmp/root-disk.qcow2","/tmp/seed.iso","default","52:54:00:ab:cd:ef")
 for _,want:=range []string{
  "<uuid>019a5d11-1234-7123-8123-123456789abc</uuid>",
  "<disk type='file' device='cdrom'>",
  "<source file='/tmp/seed.iso'/>",
  "<readonly/>",
  "52:54:00:ab:cd:ef",
 }{
  if !strings.Contains(spec,want){t.Fatalf("missing %s in XML: %s",want,spec)}
 }
}

// TestStartVirshFailurePreservesCauseAndCleansRegistry reproduces a real-world
// scenario: Libvirt rejects a domain after its SQLite row was inserted.
// Startup must return the original diagnostic rather than panic in the deferred
// cleanup when the named "instance" result becomes nil.
func TestStartVirshFailurePreservesCauseAndCleansRegistry(t *testing.T){
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.Mkdir(bin,0700);err!=nil{t.Fatal(err)}
 scripts:=map[string]string{
  "qemu-img":"#!/bin/sh\nexit 0\n",
  "virsh":"#!/bin/sh\nprintf 'simulated libvirt create failure: permission denied\\n' >&2\nexit 71\n",
  "genisoimage":"#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = '-output' ]; then\n    shift\n    printf 'seed image' > \"$1\"\n    exit 0\n  fi\n  shift\ndone\nexit 1\n",
 }
 for name,script:=range scripts{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte(script),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 profileDir:=filepath.Join(root,"profile")
 if err:=os.Mkdir(profileDir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(profileDir,"base.qcow2"),[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 cfg:=&config.Config{
  Storage:config.Storage{Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"state","insula.db")},
  Virtualization:config.Virtualization{LibvirtURI:"qemu:///system",Network:"default"},
  SSH:config.SSH{User:"insula",SudoMode:"disabled"},
 }
 template:=&profile.Profile{
  Name:"debian13",Version:"v001",Image:"base.qcow2",
  Directory:profileDir,MemoryMiB:1024,VCPUs:1,DiskGB:8,
 }
 instance,err:=Start(context.Background(),cfg,template)
 if err==nil||instance!=nil{
  t.Fatalf("expected failed VM start with nil instance; got instance=%+v, err=%v",instance,err)
 }
 if !strings.Contains(err.Error(),"simulated libvirt create failure: permission denied"){
  t.Fatalf("original libvirt diagnostic was lost: %v",err)
 }
 store,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 records,err:=store.List(context.Background())
 if err!=nil{t.Fatal(err)}
 if len(records)!=1||records[0].Status!="failed"{
  t.Fatalf("expected failed VM registry entry; got %+v",records)
 }
 if _,err:=os.Stat(records[0].InstanceDir);!os.IsNotExist(err){
  t.Fatalf("failed VM runtime directory should have been removed: %v",err)
 }
}

// Also protect failures occurring before the VM has been registered in SQLite.
func TestStartEarlyFailureDoesNotPanic(t *testing.T){
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.Mkdir(bin,0700);err!=nil{t.Fatal(err)}
 for _,name:=range []string{"qemu-img","virsh"}{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte("#!/bin/sh\nexit 0\n"),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 profileDir:=filepath.Join(root,"profile")
 if err:=os.Mkdir(profileDir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(profileDir,"base.qcow2"),[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 cfg:=&config.Config{
  Storage:config.Storage{Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"state","insula.db")},
  Virtualization:config.Virtualization{LibvirtURI:"qemu:///system",Network:"default"},
  SSH:config.SSH{User:"root",SudoMode:"disabled"},
 }
 template:=&profile.Profile{Name:"debian13",Version:"v001",Image:"base.qcow2",Directory:profileDir,MemoryMiB:1024,VCPUs:1,DiskGB:8}
 instance,err:=Start(context.Background(),cfg,template)
 if instance!=nil||err==nil||!strings.Contains(err.Error(),"invalid non-root SSH user"){
  t.Fatalf("expected informative early Cloud-Init error; instance=%+v err=%v",instance,err)
 }
}


func TestDomainXMLProtectsImmutableBackingImage(t *testing.T){
 const original="/srv/images/base's&copy.qcow2"
 spec:=domainXMLWithBacking("019a5d11-1234-7123-8123-123456789abc",
  "insula-debian",2048,2,"/tmp/overlay.qcow2",original,"/tmp/seed.iso",
  "default","52:54:00:aa:bb:cc")
 var parsed interface{}
 if err:=xml.Unmarshal([]byte(spec),&parsed);err!=nil{
  t.Fatalf("invalid Libvirt XML: %v: %s",err,spec)
 }
 for _,expected:=range []string{
  "<backingStore type='file'><format type='qcow2'/>",
  "<seclabel model='dac' relabel='no'/>",
  "<backingStore/></backingStore>",
  "<target dev='vda' bus='virtio'/>",
  "<source file='/tmp/overlay.qcow2'/>",
 }{
  if !strings.Contains(spec,expected){t.Errorf("missing %q from XML: %s",expected,spec)}
 }
 if strings.Contains(spec,original){t.Errorf("QCOW2 base path is not XML escaped: %s",spec)}
 if !strings.Contains(spec,"base&apos;s&amp;copy.qcow2") &&
    !strings.Contains(spec,"base&#39;s&amp;copy.qcow2"){
  t.Errorf("missing escaped QCOW2 base path: %s",spec)
 }
 if strings.Count(spec,"<seclabel model='dac' relabel='no'/>")!=1{
  t.Fatal("only the immutable backing image should skip libvirt DAC relabel")
 }
}
