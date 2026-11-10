package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestPreserveFailedSSHStartupForDiagnostics(t *testing.T){
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.MkdirAll(bin,0700);err!=nil{t.Fatal(err)}
 scripts:=map[string]string{
  "qemu-img":"#!/bin/sh\nexit 0\n",
  "virsh":`#!/bin/sh
case " $* " in
 *" create "*) echo "Domain started" ;;
 *" domstate "*) echo "running" ;;
 *" domifaddr "*) echo "vnet0 52:54:00:11:22:33 ipv4 192.0.2.10/24 test" ;;
 *" destroy "*) echo "DANGER: destroyed" >&2; exit 99 ;;
esac
exit 0
`,
  "genisoimage":`#!/bin/sh
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-output" ]; then
  shift
  printf 'seed' > "$1"
  exit 0
 fi
 shift
done
exit 1
`,
 }
 for name,script:=range scripts{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte(script),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 dir:=filepath.Join(root,"profile")
 if err:=os.Mkdir(dir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"base.qcow2"),[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 cfg:=&config.Config{
  Storage:config.Storage{Runs:filepath.Join(root,"runs"),Database:filepath.Join(root,"state","insula.db")},
  Virtualization:config.Virtualization{LibvirtURI:"qemu:///system",Network:"default"},
  SSH:config.SSH{User:"insula",SudoMode:"passwordless"},
 }
 p:=&profile.Profile{Name:"debian13",Version:"v001",Image:"base.qcow2",Directory:dir,MemoryMiB:2048,VCPUs:2,DiskGB:12}
 var events []string
 _,err:=StartWithStartupOptions(context.Background(),cfg,p,nil,
  StartupOptions{SSHReadyTimeout:80*time.Millisecond,PreserveOnFailure:true},
  func(ev Progress){events=append(events,ev.Stage)})
 if err==nil||!strings.Contains(err.Error(),"tempo limite aguardando SSH"){t.Fatalf("missing SSH timeout: %v",err)}
 db,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 records,err:=db.List(context.Background())
 if err!=nil||len(records)!=1{t.Fatalf("missing failed record: %v %+v",err,records)}
 if records[0].Status!="failed"{t.Fatalf("wrong state: %+v",records[0])}
 if _,err:=os.Stat(records[0].InstanceDir);err!=nil{t.Fatalf("VM diagnostics deleted: %v",err)}
 if _,err:=os.Stat(records[0].PrivateKeyPath);err!=nil{t.Fatalf("SSH credentials deleted: %v",err)}
 if !strings.Contains(strings.Join(events,","),"preserved"){t.Fatalf("preservation not reported: %v",events)}
}
