package insulavm

import (
 "context"
 "errors"
 "fmt"
 "io"
 "net"
 "strconv"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/executor"
 "github.com/ahhvahh/InsulaVM/pkg/storage"
)

// ExecRequest executes a trusted shell command inside a VM recorded in SQLite.
// The default identity is the non-root SSH user installed by Cloud-Init.
// Set Sudo to execute the command through "sudo -n sh -c", without prompting.
type ExecRequest struct{
 VMID string
 Command string
 Workspace string
 Sudo bool
 Output io.Writer
 CommandTimeout time.Duration
}
type ExecResult struct{
 VMID string
 LogDirectory string
 Elevated bool
}

// ExecVM executes one command inside an existing managed VM by UUIDv7.
// It logs the command and output, and never calls SSH as root.
func(e *Engine)ExecVM(ctx context.Context,req ExecRequest)(*ExecResult,error){
 if ctx==nil{return nil,errors.New("context is required")}
 if req.VMID==""||strings.TrimSpace(req.Command)==""||strings.ContainsRune(req.Command,0){
  return nil,errors.New("VMID and non-empty command without NUL are required")
 }
 vm,err:=e.GetVM(ctx,req.VMID)
 if err!=nil{return nil,err}
 if vm.Status!="running"{return nil,fmt.Errorf("VM %s is not running (state %s)",vm.ID,vm.Status)}
 if req.Sudo&&vm.SudoMode!="passwordless"{
  return nil,errors.New("sudo is not enabled for this VM; set ssh.sudo_mode=passwordless when creating the instance")
 }
 if vm.IP==""{return nil,errors.New("VM has no registered IP")}
 workspace:=req.Workspace
 if workspace==""{workspace=e.config.Guest.Destination}
 if !strings.HasPrefix(workspace,"/")||strings.ContainsAny(workspace,"\x00\r\n"){
  return nil,errors.New("workspace must be an absolute guest path")
 }
 timeout:=req.CommandTimeout
 if timeout<0{return nil,errors.New("command timeout must not be negative")}
 if timeout==0{timeout=30*time.Minute}
 connectTimeout:=time.Duration(e.config.SSH.ConnectTimeoutSeconds)*time.Second
 client,err:=dialWithRetry(ctx,executor.DialConfig{
  Address:net.JoinHostPort(vm.IP,strconv.Itoa(vm.SSHPort)),
  User:vm.SSHUser,
  PrivateKeyPath:vm.PrivateKeyPath,
  KnownHostsPath:vm.KnownHostsPath,
  Timeout:connectTimeout,
 },connectTimeout)
 if err!=nil{return nil,err}
 defer client.Close()
 if req.Workspace=="" {
  if err:=ensureGuestWorkspace(ctx,client,workspace);err!=nil{return nil,err}
 }
 logRun,err:=storage.NewRun(e.config.Storage.Logs,"vm-"+vm.ID,"exec",time.Now())
 if err!=nil{return nil,err}
 result:=&ExecResult{VMID:vm.ID,LogDirectory:logRun.Directory,Elevated:req.Sudo}
 log,err:=logRun.OpenJob("command")
 if err!=nil{return result,err}
 command:=req.Command
 if req.Sudo{
  command="sudo -n sh -c "+quoteGuest(command)
 }
 runner:=&executor.Executor{Client:client,Workspace:workspace,Console:req.Output,CommandTimeout:timeout}
 err=runner.Run(ctx,"command",[]string{command},false,log)
 return result,errors.Join(err,log.Close())
}
func quoteGuest(s string)string{return "'"+strings.ReplaceAll(s,"'","'\"'\"'")+"'"}
