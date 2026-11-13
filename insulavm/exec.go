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
 ErrorOutput io.Writer
 // Log enables one persistent command.log under storage.logs/vm-UUID/exec.
 // Default false: stdout/stderr are delivered without per-command logs.
 Log bool
}
type ExecResult struct{
 VMID string
 // LogDirectory is set only when ExecRequest.Log is true, for SSH or QGA.
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
// Output is sent to the caller; persistent per-command logs are opt-in via
// ExecRequest.Log. QGA returns native bytes; legacy SSH decorates job lines.
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
 result:=&ExecResult{VMID:vm.ID,Elevated:req.Sudo}
 var log executor.Recorder=discardExecRecorder{}
 var closeLog func()error=func()error{return nil}
 if req.Log{
  logRun,err:=storage.NewRun(e.config.Storage.Logs,"vm-"+vm.ID,"exec",time.Now())
  if err!=nil{return result,err}
  result.LogDirectory=logRun.Directory
  job,err:=logRun.OpenJob("command")
  if err!=nil{return result,err}
  log=job
  closeLog=job.Close
 }
 command:=req.Command
 if req.Sudo{
  command="sudo -n sh -c "+quoteGuest(command)
 }
 runner:=&executor.Executor{Client:client,Workspace:workspace,Console:req.Output,CommandTimeout:timeout}
 err=runner.Run(ctx,"command",[]string{command},false,log)
 return result,errors.Join(err,closeLog())
}
// discardExecRecorder keeps the SSH executor's existing output behavior
// without creating log files when persistence was not requested.
type discardExecRecorder struct{}
func(discardExecRecorder)Request(string)error{return nil}
func(discardExecRecorder)Response(string)error{return nil}
func(discardExecRecorder)Flush()error{return nil}

func quoteGuest(s string) string { return shelltransport.QuoteGuest(s) }
