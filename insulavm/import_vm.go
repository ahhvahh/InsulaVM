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
 checked,err:=e.GetVMStatus(ctx,request.VMID)
 if err!=nil{return nil,err}
 record:=checked.Record
 if !checked.IsRunning(){
  return nil,fmt.Errorf("VM %s is not running in Libvirt (actual state: %s; SQLite state: %s)",record.ID,checked.LibvirtState,record.Status)
 }
 if record.Status!="running"{
  return nil,fmt.Errorf("VM %s is active in Libvirt but SQLite state is %s; inspect with 'insula vm status -id %s' before importing",record.ID,record.Status,record.ID)
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
