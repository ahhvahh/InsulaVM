package insulavm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func lifecycleFixture(t *testing.T)(*Engine,string,string){
 t.Helper()
 e:=testEngine(t,"")
 const id="019a5d11-1234-7123-8123-123456789abc"
 dir:=filepath.Join(e.config.Storage.Runs,"insula-vm-operator-cleanup")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"installer.qcow2"),[]byte("original-disk"),0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(e.config.Storage.Database);if err!=nil{t.Fatal(err)}
 row:=registry.Entry{ID:id,LibvirtUUID:id,Name:"insula-alpine-virt-"+id,Profile:"alpine-virt",MAC:"52:54:00:11:22:33",InstanceDir:dir}
 if err:=store.Insert(context.Background(),row);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(context.Background(),id,"");err!=nil{t.Fatal(err)}
 _=store.Close()
 bin:=t.TempDir()
 state:=filepath.Join(bin,"active")
 script:=`#!/bin/sh
case "$3" in
 list) if [ -f "$MOCK_ACTIVE" ]; then echo "$MOCK_UUID"; fi ;;
 domstate) echo running ;;
 destroy) rm -f "$MOCK_ACTIVE" ;;
 *) echo "unexpected virsh $3" >&2; exit 91 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("MOCK_ACTIVE",state)
 t.Setenv("MOCK_UUID",id)
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 return e,id,dir
}

func TestDeleteVMRemovesDiskAndSQLiteButKeepsHistoricalLogs(t *testing.T){
 for _,active:=range []bool{false,true}{
  name:="already-offline";if active{name="running"}
  t.Run(name,func(t *testing.T){
   e,id,dir:=lifecycleFixture(t)
   if active {
    if err:=os.WriteFile(os.Getenv("MOCK_ACTIVE"),[]byte("1"),0600);err!=nil{t.Fatal(err)}
   }
   logs:=filepath.Join(e.config.Storage.Logs,"test.log")
   if err:=os.MkdirAll(filepath.Dir(logs),0700);err!=nil{t.Fatal(err)}
   if err:=os.WriteFile(logs,[]byte("evidence"),0600);err!=nil{t.Fatal(err)}
   if err:=e.DeleteVM(context.Background(),id);err!=nil{t.Fatal(err)}
   if _,err:=os.Stat(dir);!errors.Is(err,os.ErrNotExist){t.Fatalf("disk not removed: %v",err)}
   if _,err:=e.GetVM(context.Background(),id);err==nil{t.Fatal("registry row still exists")}
   if _,err:=os.Stat(logs);err!=nil{t.Fatalf("historical logs removed: %v",err)}
  })
 }
}

func TestDeleteProfileRequiresVMRecordsPurgedFirst(t *testing.T){
 e,id,_:=lifecycleFixture(t)
 dir:=filepath.Join(e.config.Storage.Profiles,"alpine-virt")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 metadata:="name: alpine-virt\nos: alpine\nhardware:\n  cpu:\n    quantity: 1\n  ram:\n    mib: 500\ndisk:\n  start_iso: ./iso/alpine.iso\n  size_gb: 8\nnetwork:\n  enabled: false\n"
 if err:=os.WriteFile(filepath.Join(dir,"profile.yaml"),[]byte(metadata),0600);err!=nil{t.Fatal(err)}
 err:=e.DeleteProfile(context.Background(),"alpine-virt")
 if err==nil||!strings.Contains(err.Error(),"referenced"){t.Fatalf("profile with registered VM was removed: %v",err)}
 if _,err:=os.Stat(dir);err!=nil{t.Fatal(err)}
 if err:=e.DeleteVM(context.Background(),id);err!=nil{t.Fatal(err)}
 if err:=e.DeleteProfile(context.Background(),"alpine-virt");err!=nil{t.Fatal(err)}
 if _,err:=os.Stat(dir);!errors.Is(err,os.ErrNotExist){t.Fatalf("profile directory still present: %v",err)}
}

func TestDeleteVMAndProfileRejectUnknownOrCanceledRequests(t *testing.T){
 e,_,_:=lifecycleFixture(t)
 if err:=e.DeleteVM(context.Background(),"unknown");err==nil{t.Fatal("unknown VM deleted")}
 if err:=e.DeleteProfile(context.Background(),"../elsewhere");err==nil{t.Fatal("unsafe profile name accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=e.DeleteProfile(ctx,"alpine-virt");!errors.Is(err,context.Canceled){t.Fatalf("cancellation ignored: %v",err)}
}
