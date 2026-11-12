package insulavm

import (
 shelltransport "github.com/ahhvahh/InsulaVM/internal/shell"
 "context"
 "errors"
 "fmt"
 "net"
 "strconv"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/executor"
 "github.com/ahhvahh/InsulaVM/pkg/identity"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// ShellCommandRequest starts a managed guest shell when ShellID is empty.
// Subsequent commands use the same VMID and ShellID, preserving cwd and
// shell variables. A session only lives in the originating Engine process.
type ShellCommandRequest struct {
 VMID string
 ShellID string
 Command string
 CommandTimeout time.Duration
}

// ShellCommandResult separates a remote nonzero exit code from infrastructure
// failures. Output is bounded, merged stdout/stderr, and persisted in SQLite.
type ShellCommandResult struct {
 VMID string
 ShellID string
 CommandID string
 Output []byte
 ExitCode int
 Completed bool
}

// ExecuteShellCommand creates or reuses an authenticated SSH shell for one
// registered VM. A failed transport or cancellation disconnects that shell;
// the command and its partial output remain in the durable history.
func(e *Engine)ExecuteShellCommand(ctx context.Context,req ShellCommandRequest)(result *ShellCommandResult,runErr error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if req.VMID==""{return nil,errors.New("VMID is required")}
 if err:=shelltransport.ValidateCommand(req.Command);err!=nil{return nil,err}
 if req.CommandTimeout<0{return nil,errors.New("command timeout cannot be negative")}
 timeout:=req.CommandTimeout
 if timeout==0{timeout=30*time.Minute}
 if err:=ctx.Err();err!=nil{return nil,err}
 checked,err:=e.GetVMStatus(ctx,req.VMID)
 if err!=nil{return nil,err}
 record:=checked.Record
 if record.ID!=req.VMID||record.LibvirtUUID!=req.VMID{
  return nil,errors.New("VM registry/Libvirt identity mismatch")
 }
 if !checked.IsRunning()||record.Status!="running"{
  return nil,fmt.Errorf("VM %s is not running (SQLite=%s, Libvirt=%s)",req.VMID,record.Status,checked.LibvirtState)
 }
 if record.IP==""||record.SSHUser==""||record.SSHPort<1||record.SSHPort>65535{
  return nil,errors.New("VM has incomplete registered SSH connection")
 }
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return nil,err}
 defer store.Close()
 var shell *shelltransport.Session
 if req.ShellID!=""{
  e.shellMu.Lock()
  shell=e.shells[req.ShellID]
  e.shellMu.Unlock()
  if shell==nil{return nil,errors.New("shell ID is not active in this Engine process; persisted history does not recreate SSH connections")}
  if shell.VMID()!=req.VMID{return nil,errors.New("shell ID belongs to another VM")}
  if shell.IsClosed(){return nil,errors.New("shell has disconnected")}
 }else{
  id,idErr:=identity.UUIDv7()
  if idErr!=nil{return nil,idErr}
  workspace,workspaceErr:=e.GuestDestinationForProfile(record.Profile)
  if workspaceErr!=nil{return nil,workspaceErr}
  connectTimeout:=time.Duration(e.config.SSH.ConnectTimeoutSeconds)*time.Second
  if connectTimeout<=0{connectTimeout=30*time.Second}
  setupCtx,setupCancel:=context.WithTimeout(ctx,connectTimeout)
  defer setupCancel()
  client,dialErr:=executor.DialContext(setupCtx,executor.DialConfig{
   Address:net.JoinHostPort(record.IP,strconv.Itoa(record.SSHPort)),
   User:record.SSHUser,PrivateKeyPath:record.PrivateKeyPath,
   KnownHostsPath:record.KnownHostsPath,Timeout:connectTimeout,
   HostKeyAlgorithms:[]string{"ssh-ed25519"},
  })
  if dialErr!=nil{return nil,dialErr}
  if err:=ensureGuestWorkspace(setupCtx,client,workspace);err!=nil{_ = client.Close();return nil,err}
  shell,err=shelltransport.Open(setupCtx,client,id,req.VMID,workspace)
  if err!=nil{_ = client.Close();return nil,err}
  if err:=store.InsertShellSession(ctx,id,req.VMID);err!=nil{shell.Close();return nil,err}
  e.shellMu.Lock()
  if e.shellsClosed {
   e.shellMu.Unlock()
   shell.Close()
   _=setShellDisconnected(store,id)
   return nil,errors.New("engine shell manager is closed")
  }
  if e.shells==nil{e.shells=make(map[string]*shelltransport.Session)}
  e.shells[id]=shell
  e.shellMu.Unlock()
 }
 result=&ShellCommandResult{VMID:req.VMID,ShellID:shell.ID(),ExitCode:-1}
 commandID,err:=identity.UUIDv7()
 if err!=nil{return result,err}
 result.CommandID=commandID
 // Only one command may use a shell's stdout reader and stdin at a time.
 runCtx,cancel:=context.WithTimeout(ctx,timeout)
 defer cancel()
 select{
 case <-runCtx.Done():return result,runCtx.Err()
 case <-shell.Busy():
 }
 defer shell.Release()
 if shell.IsClosed(){return result,errors.New("shell disconnected")}
 if err:=store.BeginShellCommand(runCtx,registry.ShellCommandEntry{
  ID:commandID,ShellID:shell.ID(),VMID:req.VMID,Command:req.Command,
 });err!=nil{return result,err}
 stopCancel:=context.AfterFunc(runCtx,func(){shell.Close()})
 output,code,commandErr:=shelltransport.Exchange(runCtx,shell,req.Command)
 stopCancel()
 if runCtx.Err()!=nil{commandErr=runCtx.Err()}
 result.Output=append([]byte(nil),output...)
 if commandErr==nil{
  result.ExitCode=code
  result.Completed=true
 }
 persistCtx,persistCancel:=context.WithTimeout(context.Background(),10*time.Second)
 defer persistCancel()
 var codePtr *int
 if commandErr==nil{codePtr=&code}
 saveErr:=store.FinishShellCommand(persistCtx,commandID,output,codePtr,commandErr)
 if commandErr!=nil{
  shell.Close()
  e.shellMu.Lock()
  if e.shells[shell.ID()]==shell{delete(e.shells,shell.ID())}
  e.shellMu.Unlock()
  closeErr:=setShellDisconnected(store,shell.ID())
  return result,errors.Join(commandErr,saveErr,closeErr)
 }
 if saveErr!=nil{
  shell.Close()
  e.shellMu.Lock()
  if e.shells[shell.ID()]==shell{delete(e.shells,shell.ID())}
  e.shellMu.Unlock()
  closeErr:=setShellDisconnected(store,shell.ID())
  return result,errors.Join(saveErr,closeErr)
 }
 return result,nil
}

func setShellDisconnected(store *registry.Store,id string)error{
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
 defer cancel()
 err:=store.SetShellSessionStatus(ctx,id,"disconnected")
 if err==nil{return nil}
 // An explicit Close or VM shutdown may have won the race.
 info,readErr:=store.GetShellSession(ctx,id)
 if readErr==nil&&(info.Status=="closed"||info.Status=="disconnected"){return nil}
 return errors.Join(err,readErr)
}

// ShellCommandHistory retrieves command/response records across Engine
// restarts. It does not reattach an SSH connection.
func(e *Engine)ShellCommandHistory(ctx context.Context,shellID string)([]registry.ShellCommandEntry,error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if strings.TrimSpace(shellID)==""{return nil,errors.New("shell ID is required")}
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return nil,err}
 defer store.Close()
 if _,err:=store.GetShellSession(ctx,shellID);err!=nil{return nil,err}
 return store.ListShellCommands(ctx,shellID)
}

// ShellSessionInfo retrieves persistent session metadata, which may outlive
// the process that held the underlying SSH connection.
func(e *Engine)ShellSessionInfo(ctx context.Context,shellID string)(registry.ShellSessionEntry,error){
 if e==nil{return registry.ShellSessionEntry{},errors.New("engine is required")}
 if ctx==nil{return registry.ShellSessionEntry{},errors.New("context is required")}
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return registry.ShellSessionEntry{},err}
 defer store.Close()
 return store.GetShellSession(ctx,shellID)
}

// ShellSessionsForVM lists all persisted session IDs for a registered VM,
// including completed sessions after a VM has been removed.
func(e *Engine)ShellSessionsForVM(ctx context.Context,vmID string)([]registry.ShellSessionEntry,error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if vmID==""{return nil,errors.New("VM ID is required")}
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return nil,err}
 defer store.Close()
 return store.ListShellSessions(ctx,vmID)
}

// CloseShellSession closes an active shell without stopping its VM.
func(e *Engine)CloseShellSession(ctx context.Context,shellID string)error{
 if e==nil{return errors.New("engine is required")}
 if ctx==nil{return errors.New("context is required")}
 if shellID==""{return errors.New("shell ID is required")}
 e.shellMu.Lock()
 shell:=e.shells[shellID]
 if shell!=nil{delete(e.shells,shellID)}
 e.shellMu.Unlock()
 if shell==nil{return errors.New("shell is not active in this Engine process")}
 shell.Close()
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return err}
 defer store.Close()
 return store.SetShellSessionStatus(ctx,shellID,"closed")
}

func(e *Engine)closeShellsForVM(vmID string)error{
 if e==nil{return nil}
 e.shellMu.Lock()
 ids:=[]string{}
 for id,s:=range e.shells{if s.VMID()==vmID{ids=append(ids,id)}}
 e.shellMu.Unlock()
 var errs []error
 for _,id:=range ids{
  ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
  errs=append(errs,e.CloseShellSession(ctx,id))
  cancel()
 }
 return errors.Join(errs...)
}

// Close releases shells held by this Engine. It does not stop any guest VM.
func(e *Engine)Close()error{
 if e==nil{return nil}
 e.shellMu.Lock()
 e.shellsClosed=true
 ids:=[]string{}
 for id:=range e.shells{ids=append(ids,id)}
 e.shellMu.Unlock()
 var errs []error
 for _,id:=range ids{
  ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
  errs=append(errs,e.CloseShellSession(ctx,id))
  cancel()
 }
 return errors.Join(errs...)
}
