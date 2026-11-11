package insulavm

import (
 "bufio"
 "bytes"
 "context"
 "errors"
 "fmt"
 "io"
 "strconv"
 "strings"
 "sync"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
 "golang.org/x/crypto/ssh"
)

const maxShellOutputBytes = 4 << 20
const maxShellCommandBytes = 64 << 10

// managedShell owns one long-running remote POSIX shell, not one SSH channel
// per command. Command execution is serialized to preserve cwd/environment.
type managedShell struct {
 id string
 vmID string
 client *ssh.Client
 process *ssh.Session
 stdin io.WriteCloser
 stdout *bufio.Reader
 busy chan struct{}
 closeOnce sync.Once
 closed bool
 stateMu sync.Mutex
}

func (s *managedShell) isClosed() bool {
 s.stateMu.Lock()
 defer s.stateMu.Unlock()
 return s.closed
}

func (s *managedShell) close() {
 s.closeOnce.Do(func() {
  s.stateMu.Lock()
  s.closed=true
  s.stateMu.Unlock()
  _=s.stdin.Close()
  _=s.process.Close()
  _=s.client.Close()
 })
}

// shellExchange sends one script into an existing shell. The random binary
// sentinel is emitted by the *shell itself* after the command, with the last
// command's status. Commands may alter cwd or export variables for later calls.
// A caller must hold the shell's busy token for the whole exchange.
func shellExchange(ctx context.Context,s *managedShell,command string)([]byte,int,error){
 if err:=ctx.Err();err!=nil{return nil,-1,err}
 if s.isClosed(){return nil,-1,errors.New("shell is disconnected")}
 token,err:=identity.UUIDv7()
 if err!=nil{return nil,-1,err}
 marker:="INSULAVM:"+token+":"
 script:=command+"\nprintf '\\036"+marker+"%d\\037' \"$?\"\n"
 if _,err=io.WriteString(s.stdin,script);err!=nil{return nil,-1,fmt.Errorf("send shell command: %w",err)}
 return readShellReply(s.stdout,marker,maxShellOutputBytes)
}

// readShellReply preserves guest output bytes (including NUL) and consumes
// only the per-command protocol marker. Guest stderr is merged remotely with
// stdout for this stateful-shell API; it is not the raw-STDIO contract.
func readShellReply(r *bufio.Reader, marker string, max int)([]byte,int,error){
 var b bytes.Buffer
 pattern:=[]byte("\x1e"+marker)
 var pending []byte
 appendOutput:=func(v byte)error{
  if b.Len()>=max{return errors.New("shell output exceeds 4 MiB limit")}
  return b.WriteByte(v)
 }
 for {
  v,err:=r.ReadByte()
  if err!=nil{
   for _,x:=range pending{if appendErr:=appendOutput(x);appendErr!=nil{return b.Bytes(),-1,appendErr}}
   return b.Bytes(),-1,fmt.Errorf("remote shell closed before command completed: %w",err)
  }
  pending=append(pending,v)
  // Sliding prefix matching prevents consuming a subsequent marker when
  // guest output itself contains the ASCII record-separator byte.
  for len(pending)>0&&!bytes.HasPrefix(pattern,pending){
   if err:=appendOutput(pending[0]);err!=nil{return b.Bytes(),-1,err}
   pending=pending[1:]
  }
  if len(pending)!=len(pattern){continue}
  var digits []byte
  for {
   x,readErr:=r.ReadByte()
   if readErr!=nil{return b.Bytes(),-1,readErr}
   if x==0x1f{break}
   if x<'0'||x>'9'||len(digits)>=10{return b.Bytes(),-1,errors.New("invalid remote shell exit status")}
   digits=append(digits,x)
  }
  code,parseErr:=strconv.Atoi(string(digits))
  if parseErr!=nil{return b.Bytes(),-1,parseErr}
  return b.Bytes(),code,nil
 }
}

func openManagedShell(ctx context.Context,client *ssh.Client,id,vmID,workspace string)(*managedShell,error){
 if client==nil{return nil,errors.New("SSH connection is required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 process,err:=client.NewSession()
 if err!=nil{return nil,err}
 fail:=func(err error)(*managedShell,error){_ = process.Close();return nil,err}
 stdin,err:=process.StdinPipe()
 if err!=nil{return fail(err)}
 stdout,err:=process.StdoutPipe()
 if err!=nil{return fail(err)}
 // All guest stderr is merged into stdout by the remote shell.
 process.Stderr=io.Discard
 if err:=process.Start("sh -s 2>&1");err!=nil{return fail(err)}
 // A stalled initialization must not outlive caller cancellation.
 stopOnCancel:=context.AfterFunc(ctx,func(){_ = client.Close()})
 defer stopOnCancel()
 s:=&managedShell{
  id:id,vmID:vmID,client:client,process:process,
  stdin:stdin,stdout:bufio.NewReaderSize(stdout,64*1024),
  busy:make(chan struct{},1),
 }
 s.busy<-struct{}{}
 // Initialize the current directory once. Failure invalidates this shell.
 _,code,err:=shellExchange(ctx,s,"cd -- "+quoteGuest(workspace))
 if err!=nil||code!=0{
  s.close()
  return nil,fmt.Errorf("initialize guest shell directory: status %d: %w",code,errOrExit(err,code))
 }
 return s,nil
}

func errOrExit(err error,status int)error{
 if err!=nil{return err}
 return fmt.Errorf("remote shell exited with %d",status)
}

func validateShellCommand(command string)error{
 if strings.TrimSpace(command)=="" {return errors.New("shell command is required")}
 if strings.ContainsRune(command,0){return errors.New("shell command cannot contain NUL")}
 if len(command)>maxShellCommandBytes{return errors.New("shell command exceeds 64 KiB")}
 return nil
}
