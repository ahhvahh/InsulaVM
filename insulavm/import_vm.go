package insulavm

import (
 "context"
 "errors"
 "fmt"
 "io"
 "net"
 "strconv"
)

// VMImportRequest imports a host path into a managed, running VM by UUIDv7.
// SSH address, identity and host-key pin are resolved from the SQLite registry.
type VMImportRequest struct {
 VMID string
 SourcePath string
 Output io.Writer
 OnProgress func(ImportProgress)
}

func(e *Engine)ImportToVM(ctx context.Context,request VMImportRequest)(*ImportResult,error){
 if ctx==nil{return nil,errors.New("context is required")}
 if request.VMID==""{return nil,errors.New("VMID is required")}
 record,err:=e.GetVM(ctx,request.VMID)
 if err!=nil{return nil,err}
 if record.Status!="running"{
  return nil,fmt.Errorf("VM %s is not running (status %s)",record.ID,record.Status)
 }
 if record.IP==""||record.SSHPort<1||record.SSHPort>65535{
  return nil,errors.New("VM has no valid SSH address in registry")
 }
 return e.ImportPath(ctx,ImportRequest{
  SourcePath:request.SourcePath,
  Host:net.JoinHostPort(record.IP,strconv.Itoa(record.SSHPort)),
  User:record.SSHUser,
  PrivateKey:record.PrivateKeyPath,
  KnownHosts:record.KnownHostsPath,
  Output:request.Output,
  OnProgress:request.OnProgress,
 })
}
