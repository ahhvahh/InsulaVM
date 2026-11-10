package vm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "sync/atomic"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestUnusedVMIDRejectsHistoricalCollisionAndRegenerates(t *testing.T){
 ctx:=context.Background()
 database:=filepath.Join(t.TempDir(),"vm.db")
 db,err:=registry.Open(database)
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 used:="019a5d11-1234-7123-8123-123456789abc"
 newID:="019a5d11-1234-7123-8123-123456789abd"
 if err:=db.Insert(ctx,registry.Entry{ID:used,LibvirtUUID:used,Name:"old-vm",Profile:"base"});err!=nil{t.Fatal(err)}
 if err:=db.SetDestroyed(ctx,used);err!=nil{t.Fatal(err)}
 count:=0
 got,err:=unusedVMID(ctx,database,func()(string,error){
  count++
  if count==1{return used,nil}
  return newID,nil
 })
 if err!=nil||got!=newID||count!=2{t.Fatalf("unexpected ID selection: got=%s calls=%d err=%v",got,count,err)}
 row,err:=db.Get(ctx,used)
 if err!=nil||row.Status!="destroyed"{t.Fatalf("historical VM changed: %+v err=%v",row,err)}
}

func TestUnusedVMIDValidationAndBoundedRetries(t *testing.T){
 ctx:=context.Background()
 dbPath:=filepath.Join(t.TempDir(),"vm.db")
 s,err:=registry.Open(dbPath)
 if err!=nil{t.Fatal(err)}
 used:="019a5d11-1234-7123-8123-123456789abc"
 if err:=s.Insert(ctx,registry.Entry{ID:used,LibvirtUUID:used,Name:"used",Profile:"base"});err!=nil{t.Fatal(err)}
 _=s.Close()
 calls:=0
 for _,tc:=range []struct{
  name string
  ctx context.Context
  generator func()(string,error)
  want string
 }{
  {"nil context",nil,func()(string,error){return used,nil},"context"},
  {"nil generator",ctx,nil,"generator"},
  {"empty id",ctx,func()(string,error){return "",nil},"empty"},
  {"random failure",ctx,func()(string,error){return "",errors.New("entropy failed")},"entropy failed"},
  {"all ids in use",ctx,func()(string,error){calls++;return used,nil},"after 8 attempts"},
 }{
  t.Run(tc.name,func(t *testing.T){
   _,err:=unusedVMID(tc.ctx,dbPath,tc.generator)
   if err==nil||!strings.Contains(err.Error(),tc.want){t.Fatalf("want %s got %v",tc.want,err)}
  })
 }
 if calls!=maxVMIDAttempts{t.Fatalf("expected %d retries got %d",maxVMIDAttempts,calls)}
 canceled,cancel:=context.WithCancel(ctx)
 cancel()
 if _,err:=unusedVMID(canceled,dbPath,identity.UUIDv7);!errors.Is(err,context.Canceled){
  t.Fatalf("cancelled allocator returned %v",err)
 }
 _,err=unusedVMID(ctx,filepath.Join(t.TempDir(),"blocked","file.db"),identity.UUIDv7)
 if err!=nil{t.Fatalf("fresh registry unavailable: %v",err)}
}

func TestConcurrentVMIDReservationsNeverDuplicate(t *testing.T){
 path:=filepath.Join(t.TempDir(),"vm.db")
 const workers=32
 var wg sync.WaitGroup
 seen:=sync.Map{}
 errs:=make(chan error,workers)
 var success atomic.Int64
 for i:=0;i<workers;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   id,err:=unusedVMID(context.Background(),path,identity.UUIDv7)
   if err!=nil{errs<-err;return}
   store,err:=registry.Open(path)
   if err!=nil{errs<-err;return}
   defer store.Close()
   if err:=store.Insert(context.Background(),registry.Entry{
    ID:id,LibvirtUUID:id,Name:"insula-"+id,Profile:"base",
   });err!=nil{errs<-err;return}
   if _,loaded:=seen.LoadOrStore(id,true);loaded{errs<-errors.New("duplicate ID accepted");return}
   success.Add(1)
  }()
 }
 wg.Wait()
 close(errs)
 for err:=range errs{t.Errorf("VM allocation failed: %v",err)}
 if success.Load()!=workers{t.Fatalf("created=%d expected=%d",success.Load(),workers)}
 store,err:=registry.Open(path)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 records,err:=store.List(context.Background())
 if err!=nil||len(records)!=workers{t.Fatalf("history=%d error=%v",len(records),err)}
}

// Existing rows must remain protected even when two concurrent allocators
// try to commit the same candidate ID after an optimistic pre-check.
func TestAtomicRegistryRejectsConcurrentSameID(t *testing.T){
 file:=filepath.Join(t.TempDir(),"vm.db")
 id:="019a5d11-1234-7123-8123-123456789abc"
 var wg sync.WaitGroup
 var successful atomic.Int32
 errs:=make(chan error,2)
 for i:=0;i<2;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   store,err:=registry.Open(file)
   if err!=nil{errs<-err;return}
   defer store.Close()
   err=store.Insert(context.Background(),registry.Entry{ID:id,LibvirtUUID:id,Name:"shared-name",Profile:"base"})
   if err==nil{successful.Add(1)}
  }()
 }
 wg.Wait()
 close(errs)
 for err:=range errs{t.Fatal(err)}
 if successful.Load()!=1{t.Fatalf("same ID was committed %d times, expected exactly one",successful.Load())}
 store,err:=registry.Open(file)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 list,err:=store.List(context.Background())
 if err!=nil||len(list)!=1{t.Fatalf("bad registry state: entries=%d error=%v",len(list),err)}
}

func TestUnusedVMIDRejectsInaccessibleRegistry(t *testing.T){
 root:=t.TempDir()
 file:=filepath.Join(root,"not-a-directory")
 if err:=os.WriteFile(file,[]byte("bad"),0600);err!=nil{t.Fatal(err)}
 _,err:=unusedVMID(context.Background(),filepath.Join(file,"state.db"),identity.UUIDv7)
 if err==nil{t.Fatal("invalid registry location was accepted")}
}
