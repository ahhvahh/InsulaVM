package registry

import (
 "context"
 "database/sql"
 "path/filepath"
 "testing"
 "time"
)

func TestSQLiteLifecycleAndReopen(t *testing.T){
 path:=filepath.Join(t.TempDir(),"state","insula.db")
 ctx:=context.Background()
 s,err:=Open(path)
 if err!=nil{t.Fatal(err)}
 e:=Entry{
  ID:"019a5d11-1234-7123-8123-123456789abc",
  LibvirtUUID:"019a5d11-1234-7123-8123-123456789abc",
  Name:"insula-debian",
  Profile:"debian-minimal",
  MAC:"52:54:00:aa:bb:cc",
  HostFingerprint:"SHA256:abc",
  SudoMode:"passwordless",
  SSHUser:"insula",
  PrivateKeyPath:"/private/key",
  KnownHostsPath:"/private/known_hosts",
  InstanceDir:"/tmp/insula-vm-test",
 }
 if err:=s.Insert(ctx,e);err!=nil{t.Fatal(err)}
 if err:=s.Insert(ctx,e);err==nil{t.Fatal("duplicate UUID accepted")}
 row,err:=s.Get(ctx,e.ID)
 if err!=nil{t.Fatal(err)}
 if row.MAC!=e.MAC||row.Profile!=e.Profile||row.Status!="creating"||row.HostFingerprint!=e.HostFingerprint{
  t.Fatalf("unexpected record: %+v",row)
 }
 if err:=s.SetRunning(ctx,e.ID,"192.168.122.42");err!=nil{t.Fatal(err)}
 after,err:=s.Get(ctx,e.ID)
 if err!=nil||after.Status!="running"||after.IP!="192.168.122.42"{
  t.Fatalf("VM state not persisted: %+v, %v",after,err)
 }
 if err:=s.Close();err!=nil{t.Fatal(err)}
 reopened,err:=Open(path)
 if err!=nil{t.Fatal(err)}
 defer reopened.Close()
 list,err:=reopened.List(ctx)
 if err!=nil||len(list)!=1||list[0].IP!="192.168.122.42"{
  t.Fatalf("database did not persist between processes: %+v, %v",list,err)
 }
 if err:=reopened.SetDestroyed(ctx,e.ID);err!=nil{t.Fatal(err)}
 stopped,err:=reopened.Get(ctx,e.ID)
 if err!=nil||stopped.Status!="destroyed"||stopped.FinishedAt==""{
  t.Fatalf("missing history: %+v, %v",stopped,err)
 }
 if _,err:=reopened.Get(ctx,"not-there");err==nil{t.Fatal("missing VM accepted")}
}

func TestConcurrentReadWrite(t *testing.T){
 s,err:=Open(filepath.Join(t.TempDir(),"insula.db"))
 if err!=nil{t.Fatal(err)}
 defer s.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
 defer cancel()
 if err:=s.Insert(ctx,Entry{
  ID:"id",LibvirtUUID:"id",Name:"insula-x",Profile:"p",
  MAC:"52:54:00:01:02:03",SSHUser:"insula",InstanceDir:"/tmp/x",
 });err!=nil{t.Fatal(err)}
 done:=make(chan error,10)
 for i:=0;i<10;i++{go func(){
  _,err:=s.List(ctx)
  done<-err
 }()}
 if err:=s.SetRunning(ctx,"id","192.0.2.11");err!=nil{t.Fatal(err)}
 for i:=0;i<10;i++{if err:=<-done;err!=nil{t.Fatal(err)}}
}

func TestMigrateEarlierRegistrySchema(t *testing.T){
 file:=filepath.Join(t.TempDir(),"old.db")
 db,err:=sql.Open("sqlite",file)
 if err!=nil{t.Fatal(err)}
 _,err=db.Exec("CREATE TABLE vm_instances (id TEXT PRIMARY KEY,libvirt_uuid TEXT NOT NULL UNIQUE,name TEXT NOT NULL UNIQUE,profile TEXT NOT NULL,ip TEXT NOT NULL DEFAULT '',mac TEXT NOT NULL,ssh_user TEXT NOT NULL,private_key_path TEXT NOT NULL,known_hosts_path TEXT NOT NULL,instance_dir TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,finished_at TEXT NOT NULL DEFAULT '')")
 if err!=nil{t.Fatal(err)}
 if err:=db.Close();err!=nil{t.Fatal(err)}
 store,err:=Open(file)
 if err!=nil{t.Fatalf("migration failed: %v",err)}
 defer store.Close()
 if err:=store.Insert(context.Background(),Entry{ID:"legacy-upgraded",LibvirtUUID:"legacy-upgraded",Name:"insula-old",Profile:"legacy",SSHUser:"insula",MAC:"52:54:00:01:02:03",InstanceDir:"/tmp/legacy"});err!=nil{
  t.Fatalf("upgraded registry cannot insert: %v",err)
 }
 entry,err:=store.Get(context.Background(),"legacy-upgraded")
 if err!=nil||entry.SSHPort!=22{
  t.Fatalf("SSH default port not migrated: %+v, %v",entry,err)
 }
}
