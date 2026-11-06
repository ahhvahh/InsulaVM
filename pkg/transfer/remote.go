package transfer

import (
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "strconv"
 "strings"
 "time"

 "golang.org/x/crypto/ssh"
)

const confirmation="INSULAVM_IMPORT_OK"

// Upload sends a locally compressed archive to the guest, verifies its byte
// length, extracts into an absolute target directory and requires an explicit
// confirmation after tar exits successfully. GNU tar is not required: POSIX
// sh, mktemp, cat, wc, mkdir, tar, and rm are used.
func Upload(ctx context.Context,client *ssh.Client,archive Archive,destination string,onSent func(sent,total int64),onStage func(stage string))(runErr error){
 if ctx==nil{return errors.New("context is required")}
 if client==nil{return errors.New("SSH client is required")}
 if !strings.HasPrefix(destination,"/") || destination=="/" ||
    strings.ContainsAny(destination,string([]byte{0,10,13})){
  return errors.New("destination must be an absolute guest directory other than /")
 }
 if archive.Path==""||archive.CompressedBytes<1{
  return errors.New("archive is not ready")
 }
 if err:=ctx.Err();err!=nil{return err}
 remoteDir,err:=runCommand(ctx,client,"mktemp -d /tmp/insulavm-import.XXXXXXXX")
 if err!=nil{return fmt.Errorf("create guest transfer directory: %w",err)}
 remoteDir=strings.TrimSpace(remoteDir)
 if !strings.HasPrefix(remoteDir,"/tmp/insulavm-import.") ||
    strings.ContainsAny(remoteDir,string([]byte{0,10,13,32,9})) ||
    strings.Contains(remoteDir[len("/tmp/insulavm-import."):],"/"){
  return fmt.Errorf("unrecognized guest staging directory %q",remoteDir)
 }
 defer func(){
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
  defer cancel()
  _,cleanupErr:=runCommand(cleanupCtx,client,"rm -rf -- "+shellQuote(remoteDir))
  if cleanupErr!=nil{runErr=errors.Join(runErr,fmt.Errorf("cleanup guest staging: %w",cleanupErr))}
 }()
 remoteArchive:=remoteDir+"/payload.tar.gz"
 if onStage!=nil{onStage("uploading")}
 if err:=uploadArchive(ctx,client,archive,remoteArchive,onSent);err!=nil{
  return fmt.Errorf("transfer compressed content: %w",err)
 }
 if onStage!=nil{onStage("verifying")}
 sizeText,err:=runCommand(ctx,client,"wc -c < "+shellQuote(remoteArchive))
 if err!=nil{return fmt.Errorf("verify guest archive length: %w",err)}
 size,err:=strconv.ParseInt(strings.TrimSpace(sizeText),10,64)
 if err!=nil{return fmt.Errorf("invalid guest archive size %q: %w",sizeText,err)}
 if size!=archive.CompressedBytes{
  return fmt.Errorf("guest archive size mismatch: got %d, expected %d",size,archive.CompressedBytes)
 }
 script:="mkdir -p -- "+shellQuote(destination)+" && tar -xzf "+shellQuote(remoteArchive)+" -C "+shellQuote(destination)+" && printf '"+confirmation+"\\n'"
 if onStage!=nil{onStage("extracting")}
 output,err:=runCommand(ctx,client,script)
 if err!=nil{return fmt.Errorf("unpack archive inside guest: %w",err)}
 // GNU tar may emit nonfatal timestamp warnings on stderr before the
 // confirmation line. Require a successful exit and the FINAL marker.
 if !strings.HasSuffix(strings.TrimSpace(output),confirmation){
  return fmt.Errorf("guest did not confirm import (response: %q)",output)
 }
 return nil
}

func uploadArchive(ctx context.Context,client *ssh.Client,archive Archive,remotePath string,onSent func(int64,int64))error{
 local,err:=os.Open(archive.Path)
 if err!=nil{return err}
 defer local.Close()
 session,err:=client.NewSession()
 if err!=nil{return err}
 defer session.Close()
 stop:=context.AfterFunc(ctx,func(){_ = session.Close()})
 defer stop()
 var sent int64
 var lastReported int64
 session.Stdin=&countingReader{source:local,read:func(n int){
  sent+=int64(n)
  if onSent!=nil && (sent-lastReported>=1024*1024 || sent>=archive.CompressedBytes){
   lastReported=sent
   onSent(sent,archive.CompressedBytes)
  }
 }}
 // Stderr is limited to 8 KiB to keep error messages bounded.
 errorOutput:=&limitedOutput{remaining:8192}
 session.Stderr=errorOutput
 err=session.Run("cat > "+shellQuote(remotePath))
 if ctx.Err()!=nil{return ctx.Err()}
 if err!=nil{return fmt.Errorf("SSH stream failed: %w: %s",err,errorOutput.String())}
 if sent!=archive.CompressedBytes{return fmt.Errorf("archive read size changed: %d vs %d",sent,archive.CompressedBytes)}
 if onSent!=nil && lastReported<sent{onSent(sent,archive.CompressedBytes)}
 return nil
}

func runCommand(ctx context.Context,client *ssh.Client,script string)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 session,err:=client.NewSession()
 if err!=nil{return "",err}
 defer session.Close()
 stop:=context.AfterFunc(ctx,func(){_ = session.Close()})
 defer stop()
 out,err:=session.CombinedOutput(script)
 if ctx.Err()!=nil{return "",ctx.Err()}
 if err!=nil{return "",fmt.Errorf("%w: %s",err,strings.TrimSpace(string(out)))}
 return string(out),nil
}

func shellQuote(v string)string{
 return "'"+strings.ReplaceAll(v,"'","'\"'\"'")+"'"
}

type countingReader struct{
 source io.Reader
 read func(int)
}
func (r *countingReader)Read(p []byte)(int,error){
 n,err:=r.source.Read(p)
 if n!=0{r.read(n)}
 return n,err
}

type limitedOutput struct{
 data strings.Builder
 remaining int
}
func (w *limitedOutput)Write(p []byte)(int,error){
 original:=len(p)
 n:=len(p)
 if n>w.remaining{n=w.remaining}
 if n>0{_,_ = w.data.Write(p[:n]);w.remaining-=n}
 return original,nil
}
func(w *limitedOutput)String()string{return w.data.String()}
