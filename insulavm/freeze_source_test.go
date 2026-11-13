package insulavm

import (
 "os"
 "path/filepath"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/config"
)

func TestResolveFreezeSourceInstallerAndCloudInit(t *testing.T){
 root:=t.TempDir()
 create:=func(name,content string){
  t.Helper()
  folder:=filepath.Join(root,name)
  if err:=os.MkdirAll(folder,0700);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(folder,"profile.yaml"),[]byte(content),0600);err!=nil{t.Fatal(err)}
 }
 create("alpine-virt",`name: alpine-virt
os: alpine
hardware:
  cpu:
    quantity: 1
  ram:
    mib: 500
disk:
  start_iso: /isos/alpine.iso
  size_gb: 8
workspace:
  destination: /srv/tests
network:
  enabled: true
`)
 create("debian-prepared",`name: debian-prepared
os: debian
memory_mib: 1024
vcpus: 1
disk_gb: 8
version: v001
image: versions/v001/base.qcow2
`)
 engine:=&Engine{config:config.Config{Storage:config.Storage{Profiles:root}}}
 tests:=[]struct{
  name string
  disk string
  installer bool
  bootMode string
 }{
  {"alpine-virt","installer.qcow2",true,"direct"},
  {"debian-prepared","overlay.qcow2",false,""},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   p,disk,installer,err:=engine.resolveFreezeSource(tc.name)
   if err!=nil{t.Fatal(err)}
   if disk!=tc.disk||installer!=tc.installer||p.BootMode!=tc.bootMode{
    t.Fatalf("profile=%+v disk=%q installer=%t",p,disk,installer)
   }
   if tc.installer{
    if p.MemoryMiB!=500||p.VCPUs!=1||p.DiskGB!=8||p.Workspace.Destination!="/srv/tests"{
     t.Fatalf("installer hardware or workspace lost: %+v",p)
    }
   }
  })
 }
 if _,_,_,err:=engine.resolveFreezeSource("missing");err==nil{t.Fatal("missing profile accepted")}
}
