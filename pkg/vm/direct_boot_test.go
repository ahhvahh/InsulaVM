package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestStartDirectBootDoesNotRequireCloudInitOrSSH(t *testing.T) {
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.Mkdir(bin,0700);err!=nil{t.Fatal(err)}
 qemu:=`#!/bin/sh
set -eu
case "$1" in
 create)
  for arg in "$@"; do last="$arg"; done
  printf 'QFI\373' > "$last"
  ;;
 *) exit 11 ;;
esac
`
 virsh:=`#!/bin/sh
case "$3" in
 create) echo "Domain created" ;;
 domstate) echo running ;;
 destroy) exit 0 ;;
 *) echo "Unexpected virsh command: $3" >&2; exit 13 ;;
esac
`
 for name,script:=range map[string]string{"qemu-img":qemu,"virsh":virsh}{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte(script),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 baseDir:=filepath.Join(root,"profile")
 if err:=os.Mkdir(baseDir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(baseDir,"base.qcow2"),[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 cfg:=&config.Config{
  Storage:config.Storage{Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"state","insula.db")},
  Virtualization:config.Virtualization{LibvirtURI:"qemu:///system",Network:"default"},
  SSH:config.SSH{User:"insula",SudoMode:"passwordless"},
 }
 p:=&profile.Profile{Name:"abioma-tests",OS:"alpine",MemoryMiB:500,VCPUs:1,DiskGB:8,
  Version:"v001",Image:"base.qcow2",Directory:baseDir,BootMode:"direct"}
 instance,err:=Start(context.Background(),cfg,p)
 if err!=nil{t.Fatal(err)}
 if instance==nil||instance.SSHUser!=""||instance.IPAddress!=""||instance.SudoMode!="disabled"{
  t.Fatalf("direct boot must not claim SSH/IP/sudo availability: %+v",instance)
 }
 for _,name:=range []string{"client_ed25519","known_hosts","seed.iso"}{
  if _,err:=os.Stat(filepath.Join(instance.Directory,name));!os.IsNotExist(err){
   t.Fatalf("direct boot unexpectedly created %s: %v",name,err)
  }
 }
 spec,err:=os.ReadFile(filepath.Join(instance.Directory,"domain.xml"))
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(spec),"device='cdrom'")||!strings.Contains(string(spec),"org.qemu.guest_agent.0"){
  t.Fatalf("direct boot domain must use disk and QGA without seed ISO: %s",spec)
 }
 store,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 rec,err:=store.Get(context.Background(),instance.ID)
 _=store.Close()
 if err!=nil{t.Fatal(err)}
 if rec.Status!="running"||rec.SudoMode!="disabled"||rec.IP!=""{
  t.Fatalf("incorrect direct-boot registry state: %+v",rec)
 }
 if err:=instance.Cleanup(context.Background());err!=nil{t.Fatal(err)}
}

func TestDirectBootRejectsCloudInitStartupScript(t *testing.T){
 _,err:=StartWithStartupScript(context.Background(),&config.Config{},
  &profile.Profile{BootMode:"direct"},[]byte("#!/bin/sh\nexit 0\n"),nil)
 if err==nil||!strings.Contains(err.Error(),"do not support Cloud-Init startup scripts"){
  t.Fatalf("direct boot silently accepted cloud-init script: %v",err)
 }
}
