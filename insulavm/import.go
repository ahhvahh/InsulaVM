package insulavm

import (
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "path"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/executor"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/transfer"
 "golang.org/x/crypto/ssh"
)

// ImportRequest identifies a host file or directory and a running Linux VM.
// Destination overrides the legacy global default for unmanaged hosts.
// Managed VM imports resolve the directory from their VM profile.
type ImportRequest struct {
 SourcePath string
 Destination string
 Host string
 User string
 PrivateKey string
 KnownHosts string
 Output io.Writer
 OnProgress func(ImportProgress)
}

// ImportProgress is delivered synchronously to the consumer as each stage
// changes. A callback should return promptly and must not block the SSH stream.
// A stage can recur to report cumulative upload bytes.
type ImportProgress struct {
 Stage string
 Message string
 SourcePath string
 DestinationPath string
 BytesSent int64
 TotalBytes int64
 Files int
 At time.Time
}

// ImportResult is returned only after the guest confirms successful extraction.
type ImportResult struct {
 SourcePath string
 DestinationPath string
 Files int
 Directories int
 OriginalBytes int64
 CompressedBytes int64
 Imported bool
}

// ImportPath connects to an existing guest and synchronously imports a host
// path; the caller may cancel using ctx. When using Engine.StartVM, pass the
// Instance.Address from that VM as request.Host, then Cleanup the instance.
func(e *Engine)ImportPath(ctx context.Context,request ImportRequest)(result *ImportResult,runErr error){
 destination:=request.Destination
 if destination==""{destination=e.config.Guest.Destination}
 if err:=profile.ValidateGuestDestination(destination);err!=nil{return nil,fmt.Errorf("invalid import destination: %w",err)}
 destination=path.Clean(destination)
 reporter:=importReporter{out:request.Output,callback:request.OnProgress,source:request.SourcePath,destination:destination}
 defer func(){
  if runErr!=nil{reporter.send("failed","Falha na importacao: "+runErr.Error(),0,0,0)}
 }()
 if ctx==nil{return nil,errors.New("context is required")}
 if request.Host==""{return nil,errors.New("Host must identify an already running VM (host:port)")}
 user:=e.config.SSH.User
 key:=e.config.SSH.PrivateKey
 hosts:=e.config.SSH.KnownHosts
 if request.User!=""{user=request.User}
 if request.PrivateKey!=""{key=request.PrivateKey}
 if request.KnownHosts!=""{hosts=request.KnownHosts}
 if user==""||key==""||hosts==""{return nil,errors.New("SSH user, private key and known_hosts are required")}
 reporter.send("connecting","Conectando via SSH a VM "+request.Host,0,0,0)
 wait:=time.Duration(e.config.SSH.ConnectTimeoutSeconds)*time.Second
 client,err:=dialWithRetry(ctx,executor.DialConfig{
  Address:request.Host,User:user,PrivateKeyPath:key,
  KnownHostsPath:hosts,Timeout:wait,
 },wait)
 if err!=nil{return nil,err}
 defer client.Close()
 return importWithClient(ctx,client,request.SourcePath,destination,reporter)
}

// importWithClient reuses the authenticated SSH connection for standalone
// imports or pre-job imports inside Engine.Run.
func importWithClient(ctx context.Context,client *ssh.Client,source,destination string,reporter importReporter)(result *ImportResult,err error){
 reporter.send("validating","Verificando arquivo ou diretorio de origem",0,0,0)
 if source==""||destination==""{return nil,errors.New("source and destination paths are required")}
 reporter.send("compressing","Compactando conteudo em TAR.GZ no hospedeiro",0,0,0)
 archive,err:=transfer.Compress(ctx,source)
 if err!=nil{return nil,err}
 defer func(){_ = os.Remove(archive.Path)}()
 result=&ImportResult{
  SourcePath:archive.Source,DestinationPath:destination,
  Files:archive.Files,Directories:archive.Directories,
  OriginalBytes:archive.OriginalBytes,CompressedBytes:archive.CompressedBytes,
 }
 reporter.send("compressed",fmt.Sprintf("Compactacao concluida: %d arquivo(s), %d bytes compactados",archive.Files,archive.CompressedBytes),0,archive.CompressedBytes,archive.Files)
 reported:=int64(0)
 progress:=func(sent,total int64){
  if sent<=reported{return}
  reported=sent
  reporter.send("uploading",fmt.Sprintf("Enviando: %d/%d bytes",sent,total),sent,total,archive.Files)
 }
 stage:=func(name string){
  switch name{
  case "uploading":
   reporter.send("uploading",fmt.Sprintf("Enviando pacote de %d bytes para a VM",archive.CompressedBytes),0,archive.CompressedBytes,archive.Files)
  case "verifying":
   reporter.send("verifying","Verificando integridade do tamanho recebido na VM",archive.CompressedBytes,archive.CompressedBytes,archive.Files)
  case "extracting":
   reporter.send("extracting","Descompactando TAR.GZ dentro da VM em "+destination,archive.CompressedBytes,archive.CompressedBytes,archive.Files)
  }
 }
 if err:=transfer.Upload(ctx,client,archive,destination,progress,stage);err!=nil{return result,err}
 result.Imported=true
 reporter.send("completed",fmt.Sprintf("Arquivo importado com sucesso na VM: %s (%d arquivo(s))",destination,archive.Files),archive.CompressedBytes,archive.CompressedBytes,archive.Files)
 return result,nil
}

type importReporter struct{
 out io.Writer
 callback func(ImportProgress)
 source string
 destination string
}
func (r importReporter)send(stage,message string,sent,total int64,files int){
 p:=ImportProgress{
  Stage:stage,Message:message,SourcePath:r.source,DestinationPath:r.destination,
  BytesSent:sent,TotalBytes:total,Files:files,At:time.Now(),
 }
 if r.out!=nil{_,_=fmt.Fprintf(r.out,"[InsulaVM][import:%s] %s\n",stage,message)}
 if r.callback!=nil{r.callback(p)}
}
