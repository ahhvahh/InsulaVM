package registry

import (
 "context"
 "database/sql"
 "errors"
 "path/filepath"
 "testing"
)

func TestRegistryRestartClearsStaleNetworkStateAndPurge(t *testing.T){
 ctx:=context.Background()
 store,err:=Open(filepath.Join(t.TempDir(),"insulavm.db"));if err!=nil{t.Fatal(err)}
 defer store.Close()
 const id="019a5d11-1234-7123-8123-123456789abc"
 entry:=Entry{ID:id,LibvirtUUID:id,Name:"insula-test",Profile:"alpine",MAC:"52:54:00:11:22:33",InstanceDir:"/runs/insula-vm-test"}
 if err:=store.Insert(ctx,entry);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(ctx,id,"192.0.2.20");err!=nil{t.Fatal(err)}
 if err:=store.SetRestarted(ctx,id);err!=nil{t.Fatal(err)}
 got,err:=store.Get(ctx,id)
 if err!=nil||got.IP!=""||got.Status!="running"||got.FinishedAt!=""{t.Fatalf("wrong restart row: %+v %v",got,err)}
 if _,err:=store.db.ExecContext(ctx,"INSERT INTO shell_sessions(id,vm_id,status,created_at,updated_at) VALUES('shell-test',?,'closed','t','t')",id);err!=nil{t.Fatal(err)}
 if _,err:=store.db.ExecContext(ctx,"INSERT INTO shell_commands(id,shell_id,vm_id,command,status,started_at) VALUES('cmd-test','shell-test',?,'ls','completed','t')",id);err!=nil{t.Fatal(err)}
 if err:=store.Delete(ctx,id);err!=nil{t.Fatal(err)}
 if _,err:=store.Get(ctx,id);!errors.Is(err,sql.ErrNoRows){t.Fatalf("row not deleted: %v",err)}
 for _,table:=range []string{"vm_instances","shell_sessions","shell_commands"}{
  var count int
  if err:=store.db.QueryRowContext(ctx,"SELECT count(*) FROM "+table).Scan(&count);err!=nil{t.Fatal(err)}
  if count!=0{t.Fatalf("%s not purged: %d",table,count)}
 }
 if err:=store.Delete(ctx,id);err==nil{t.Fatal("double purge should fail")}
 if used,err:=store.IDPreviouslyUsed(ctx,id);err!=nil||!used{t.Fatalf("deleted UUID must remain reserved: %t %v",used,err)}
 if err:=store.Insert(ctx,entry);err==nil{t.Fatal("reused UUID after purge")}
 if err:=store.SetRestarted(ctx,id);err==nil{t.Fatal("resumed deleted row")}
}

func TestRegistryRejectsNilContextForDestructiveOperations(t *testing.T){
 store,err:=Open(filepath.Join(t.TempDir(),"insula.db"));if err!=nil{t.Fatal(err)}
 defer store.Close()
 if err:=store.SetRestarted(nil,"id");err==nil{t.Fatal("nil context accepted for restart")}
 if err:=store.Delete(nil,"id");err==nil{t.Fatal("nil context accepted for delete")}
}
