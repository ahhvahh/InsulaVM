package vm

import (
 "context"
 "errors"
 "fmt"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

const maxVMIDAttempts = 8

// unusedVMID checks recorded and permanently deleted VM IDs (including tombstones)
// before provisioning a new instance. The caller MUST still reserve the ID
// with registry.Store.Insert, whose PRIMARY KEY is the atomic concurrency gate.
func unusedVMID(ctx context.Context, registryPath string, next func() (string,error)) (string,error) {
 if ctx==nil {return "",errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return "",err}
 if next==nil{return "",errors.New("VM ID generator is required")}
 store,err:=registry.Open(registryPath)
 if err!=nil{return "",fmt.Errorf("open VM ID registry: %w",err)}
 defer store.Close()
 for i:=0;i<maxVMIDAttempts;i++ {
  if err:=ctx.Err();err!=nil{return "",err}
  id,err:=next()
  if err!=nil{return "",fmt.Errorf("generate UUIDv7: %w",err)}
  if strings.TrimSpace(id)==""{return "",errors.New("generated VM ID is empty")}
  used,err:=store.IDPreviouslyUsed(ctx,id)
  if err!=nil{return "",fmt.Errorf("check VM ID %s in registry: %w",id,err)}
  if used{continue}
  return id,nil
 }
 return "",fmt.Errorf("could not allocate an unused VM ID after %d attempts",maxVMIDAttempts)
}
