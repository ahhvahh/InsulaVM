package registry

import (
    "context"
    "path/filepath"
    "strings"
    "testing"
)

func TestRegistryFailedVMHistoryIsRetained(t *testing.T){
    store,err:=Open(filepath.Join(t.TempDir(),"private","insula.db"))
    if err!=nil{t.Fatal(err)}
    defer store.Close()
    ctx:=context.Background()
    entry:=Entry{
        ID:"019a5d11-1234-7123-8123-123456789abc",
        LibvirtUUID:"019a5d11-1234-7123-8123-123456789abc",
        Name:"insula-failed",Profile:"cloud",SSHUser:"insula",
        MAC:"52:54:00:11:22:33",InstanceDir:"/tmp/insula-vm-failed",
    }
    if err:=store.Insert(ctx,entry);err!=nil{t.Fatal(err)}
    if err:=store.SetFailed(ctx,entry.ID);err!=nil{t.Fatal(err)}
    saved,err:=store.Get(ctx,entry.ID)
    if err!=nil||saved.Status!="failed"||saved.FinishedAt==""{
        t.Fatalf("failed VM evidence lost: %+v %v",saved,err)
    }
    if err:=store.SetDestroyed(ctx,entry.ID);err!=nil{t.Fatal(err)}
    saved,err=store.Get(ctx,entry.ID)
    if err!=nil||saved.Status!="destroyed"||saved.ID!=entry.ID{
        t.Fatalf("destroyed VM history lost: %+v %v",saved,err)
    }
    for _,step:=range []struct{name string;fn func(context.Context)error}{
        {"set-failed",func(ctx context.Context)error{return store.SetFailed(ctx,entry.ID)}},
        {"set-destroyed",func(ctx context.Context)error{return store.SetDestroyed(ctx,entry.ID)}},
        {"set-running",func(ctx context.Context)error{return store.SetRunning(ctx,entry.ID,"192.0.2.3")}},
    }{
        if err:=step.fn(nil);err==nil||!strings.Contains(err.Error(),"context"){
            t.Errorf("%s accepted nil context: %v",step.name,err)
        }
    }
    if err:=store.SetFailed(ctx,"missing");err==nil||!strings.Contains(err.Error(),"not found"){
        t.Fatalf("missing VM accepted for SetFailed: %v",err)
    }
    if _,err:=store.Get(nil,entry.ID);err==nil{t.Fatal("Get accepted nil context")}
    if _,err:=store.List(nil);err==nil{t.Fatal("List accepted nil context")}
    if err:=store.Insert(nil,entry);err==nil{t.Fatal("Insert accepted nil context")}
}

func TestSQLiteStoreRejectsInvalidPath(t *testing.T){
    if _,err:=Open("");err==nil{t.Fatal("blank database path accepted")}
    dir:=t.TempDir()
    if _,err:=Open(dir);err==nil{t.Fatal("directory used as SQLite database")}
}
