package insulavm

import (
 shelltransport "github.com/ahhvahh/InsulaVM/internal/shell"
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
 // Output optionally receives native QGA stdout bytes after guest-exec ends.
 // For legacy SSH this remains the decorated job console output.
 // Nil is supported; QGA still fills ExecResult.Stdout.
 Output io.Writer
 CommandTimeout time.Duration
 // QGA selects local QEMU guest-agent execution instead of SSH; no IP needed.
 QGA bool
 // ErrorOutput optionally receives unmodified QGA stderr bytes.
 // QGA does not persist captured output in storage.logs.
 ErrorOutput io.Writer
}
type ExecResult struct{
 VMID string
 // LogDirectory is populated by legacy SSH exec; it is empty for QGA.
 LogDirectory string
 Elevated bool
 // QGA returns unmodified bytes and the guest's exit code; SSH remains legacy.
 Stdout []byte
 Stderr []byte
 ExitCode *int
 StdoutTruncated bool
 StderrTruncated bool
}

// ExecVM executes one command inside an existing managed VM by UUIDv7.
// Legacy SSH execution logs the command and output. QGA does not create a
// command log: results are returned as native bytes and/or to request writers.
func(e *Engine)ExecVM(ctx context.Context,req ExecRequest)(*ExecResult,error){
 if ctx==nil{return nil,errors.New("context is required")}
 if req.VMID==""||strings.TrimSpace(req.Command)==""||strings.ContainsRune(req.Command,0){
  return nil,errors.New("VMID and non-empty command without NUL are required")
 }
 checked,err:=e.GetVMStatus(ctx,req.VMID)
 if err!=nil{return nil,err}
 vm:=checked.Record
 if !checked.IsRunning(){
  return nil,fmt.Errorf("VM %s is not running in Libvirt (actual state: %s; SQLite state: %s)",vm.ID,checked.LibvirtState,vm.Status)
 }
 if vm.Status!="running"{
  return nil,fmt.Errorf("VM %s is active in Libvirt but SQLite state is %s; inspect with 'insula vm status -id %s' before executing",vm.ID,vm.Status,vm.ID)
 }
 // The same nonblocking lock covers SSH and QGA, CLI and library calls.
 // A concurrent exec against this VM is rejected instead of queued.
 releaseExecLock,err:=acquireVMExecLock(e.config.Storage.Database,vm.ID)
 if err!=nil{return nil,err}
 defer releaseExecLock()
 if req.QGA{return e.execVMQGA(ctx,vm.ID,req)}
 if req.Sudo&&vm.SudoMode!="passwordless"{
  return nil,errors.New("sudo is not enabled for this VM; set ssh.sudo_mode=passwordless when creating the instance")
 }
 if vm.IP==""{return nil,errors.New("VM has no registered IP")}
 workspace:=req.Workspace
 if workspace==""{
  workspace,err=e.ResolveVMWorkspace(vm.Profile)
  if err!=nil{return nil,fmt.Errorf("resolve VM workspace: %w",err)}
 }
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
  HostKeyAlgorithms:[]string{"ssh-ed25519"},
 },connectTimeout)
 if err!=nil{return nil,err}
 defer client.Close()
 if req.Workspace=="" {
  if err:=shelltransport.EnsureWorkspace(ctx,client,workspace);err!=nil{return nil,err}
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
func quoteGuest(s string) string { return shelltransport.QuoteGuest(s) }
