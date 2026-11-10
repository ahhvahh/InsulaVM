package vm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// The library can give the caller an ID before the VM has DHCP or SSH.
// At "registered", that ID must already be durable and unique in SQLite.
func TestStartProgressPublishesReservedIDBeforeSSH(t *testing.T){
 cfg,_:=isolatedLiveISOHost(t)
 source:=t.TempDir()
 image:=filepath.Join(source,"base.qcow2")
 if err:=os.WriteFile(image,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 p:=&profile.Profile{Name:"cloud",Version:"v001",Directory:source,Image:"base.qcow2",MemoryMiB:512,VCPUs:1,DiskGB:8}
 bin:=t.TempDir()
 scripts:=map[string]string{
  "qemu-img":`for last; do :; done
printf 'QFI\373overlay' > "$last"`,
  "virsh":`case "$3" in
 create) exit 0 ;;
 destroy) exit 0 ;;
 list) exit 0 ;;
 domstate) echo running ;;
 domifaddr) echo "vnet0 52:54:00:11:22:33 ipv4 192.0.2.44/24 guest" ;;
 *) exit 17 ;;
esac`,
 }
 for name,script:=range scripts{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte("#!/bin/sh\nset -eu\n"+script+"\n"),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 ctx,cancel:=context.WithTimeout(context.Background(),1500*time.Millisecond)
 defer cancel()
 var registeredID string
 var gotReady bool
 result,err:=StartWithStartupOptions(ctx,cfg,p,nil,StartupOptions{PreserveOnFailure:true},func(event Progress){
  if event.Stage=="registered" {
   if event.ID=="" {t.Fatal("registration callback has no VM ID")}
   registeredID=event.ID
   store,storeErr:=registry.Open(cfg.Storage.Database)
   if storeErr!=nil{t.Fatal(storeErr)}
   row,getErr:=store.Get(context.Background(),event.ID)
   _=store.Close()
   if getErr!=nil||row.ID!=event.ID||row.Status!="creating" {
    t.Fatalf("callback emitted ID before durable reservation: %+v %v",row,getErr)
   }
  }
  if event.Stage=="ready"{gotReady=true}
 })
 if result!=nil||!errors.Is(err,context.DeadlineExceeded){
  t.Fatalf("expected SSH deadline after registration: result=%+v err=%v",result,err)
 }
 if registeredID==""||gotReady {t.Fatalf("early ID missing or falsely ready: id=%q ready=%v",registeredID,gotReady)}
 store,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 entry,err:=store.Get(context.Background(),registeredID)
 if err!=nil||entry.Status!="failed"{t.Fatalf("failed VM should retain ID for diagnosis: %+v %v",entry,err)}
}
