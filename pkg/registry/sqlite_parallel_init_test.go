package registry

import (
 "context"
 "fmt"
 "path/filepath"
 "sync"
 "testing"
 "time"
)

// Opening a single registry from independent CLI/Go callers must not fail
// while WAL and schema initialization race across connections.
func TestConcurrentRegistryOpenAndUniqueInsert(t *testing.T){
 path:=filepath.Join(t.TempDir(),"shared.db")
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
 defer cancel()
 const count=32
 var wg sync.WaitGroup
 outcomes:=make(chan error,count)
 for i:=0;i<count;i++{
  wg.Add(1)
  go func(index int){
   defer wg.Done()
   store,err:=Open(path)
   if err!=nil{outcomes<-fmt.Errorf("open %d: %w",index,err);return}
   defer store.Close()
   id:=fmt.Sprintf("019a5d11-1234-7123-8123-%012x",index)
   if err:=store.Insert(ctx,Entry{
    ID:id,LibvirtUUID:id,Name:"insula-"+id,Profile:"base",
   });err!=nil{outcomes<-fmt.Errorf("insert %d: %w",index,err);return}
   outcomes<-nil
  }(i)
 }
 wg.Wait()
 close(outcomes)
 for err:=range outcomes{if err!=nil{t.Errorf("concurrent SQLite initialization failed: %v",err)}}
 verify,err:=Open(path)
 if err!=nil{t.Fatal(err)}
 defer verify.Close()
 rows,err:=verify.List(ctx)
 if err!=nil{t.Fatal(err)}
 if len(rows)!=count{t.Fatalf("registered IDs=%d expected=%d",len(rows),count)}
}
