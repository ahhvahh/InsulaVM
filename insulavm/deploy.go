package insulavm

import (
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "time"
)

// VMDeployRequest selects a running InsulaVM guest and two host files.
// The script is uploaded to the guest's configured destination and executed
// with the uploaded file's base name as its first argument.
type VMDeployRequest struct {
 VMID string
 SourcePath string
 ScriptPath string
 Extract bool
 Sudo bool
 Output io.Writer
 CommandTimeout time.Duration
}

// VMDeployResult reports which stages completed. A nil error is returned only
// when BOTH uploads and every requested guest command succeed.
type VMDeployResult struct {
 VMID string
 SourceName string
 ScriptName string
 DestinationPath string
 SourceImported bool
 Extracted bool
 ScriptImported bool
 ScriptSucceeded bool
 ScriptLogDirectory string
}

// deployOperations is kept private so tests can exercise error propagation
// without starting a hypervisor or changing the public Engine API.
type deployOperations interface {
 ImportToVM(context.Context, VMImportRequest)(*ImportResult,error)
 ExecVM(context.Context, ExecRequest)(*ExecResult,error)
}

// DeployToVM is the native single-call upload + post-load script operation.
func (e *Engine) DeployToVM(ctx context.Context, req VMDeployRequest)(*VMDeployResult,error){
 if e==nil{return nil,errors.New("engine is required")}
 return deployToVM(ctx,e,req)
}

func regularDeployFile(pathValue,label string)(string,error){
 if strings.TrimSpace(pathValue)==""{return "",fmt.Errorf("%s path is required",label)}
 abs,err:=filepath.Abs(pathValue)
 if err!=nil{return "",fmt.Errorf("resolve %s: %w",label,err)}
 info,err:=os.Lstat(abs)
 if err!=nil{return "",fmt.Errorf("inspect %s: %w",label,err)}
 if !info.Mode().IsRegular(){return "",fmt.Errorf("%s must be a regular file (no symlinks/directories)",label)}
 return abs,nil
}

func prepareVMDeploy(req VMDeployRequest)(VMDeployRequest,error){
 if strings.TrimSpace(req.VMID)==""{return req,errors.New("VMID is required")}
 if req.CommandTimeout<0{return req,errors.New("command timeout cannot be negative")}
 source,err:=regularDeployFile(req.SourcePath,"source")
 if err!=nil{return req,err}
 script,err:=regularDeployFile(req.ScriptPath,"script")
 if err!=nil{return req,err}
 if strings.EqualFold(filepath.Base(source),filepath.Base(script)){
  return req,errors.New("source and script must have different file names in guest.destination")
 }
 if req.Extract {
  lower:=strings.ToLower(source)
  if !strings.HasSuffix(lower,".tar.gz")&&!strings.HasSuffix(lower,".tgz"){
   return req,errors.New("extract requires a .tar.gz or .tgz source archive")
  }
 }
 req.SourcePath=source
 req.ScriptPath=script
 return req,nil
}

func deployToVM(ctx context.Context,ops deployOperations,req VMDeployRequest)(result *VMDeployResult,err error){
 if ctx==nil{return nil,errors.New("context is required")}
 if ops==nil{return nil,errors.New("deploy operations are required")}
 req,err=prepareVMDeploy(req)
 if err!=nil{return nil,err}
 result=&VMDeployResult{
  VMID:req.VMID,
  SourceName:filepath.Base(req.SourcePath),
  ScriptName:filepath.Base(req.ScriptPath),
 }
 report:=func(stage,msg string){
  if req.Output!=nil{_,_=fmt.Fprintf(req.Output,"[InsulaVM][deploy:%s] %s\n",stage,msg)}
 }
 upload:=func(stage,source string)(*ImportResult,error){
  report(stage,"Enviando "+filepath.Base(source)+" para VM "+req.VMID)
  imported,err:=ops.ImportToVM(ctx,VMImportRequest{
   VMID:req.VMID,SourcePath:source,Output:req.Output,
  })
  if err!=nil{return imported,fmt.Errorf("%s: %w",stage,err)}
  if imported==nil||!imported.Imported{
   return imported,fmt.Errorf("%s: VM nao confirmou o recebimento do arquivo",stage)
  }
  result.DestinationPath=imported.DestinationPath
  return imported,nil
 }
 execute:=func(stage,command string)(*ExecResult,error){
  report(stage,"Executando etapa na VM")
  done,err:=ops.ExecVM(ctx,ExecRequest{
   VMID:req.VMID,Command:command,Sudo:req.Sudo,Output:req.Output,CommandTimeout:req.CommandTimeout,
  })
  if done!=nil{result.ScriptLogDirectory=done.LogDirectory}
  if err!=nil{return done,fmt.Errorf("%s: %w",stage,err)}
  if done==nil{return done,fmt.Errorf("%s: resultado de execucao ausente",stage)}
  return done,nil
 }

 if _,err=upload("source",req.SourcePath);err!=nil{return result,err}
 result.SourceImported=true
 if req.Extract{
  // Explicit opt-in: only extract TAR.GZ archives supplied by a trusted user.
  if _,err=execute("extract","tar -xzf "+quoteGuest(result.SourceName));err!=nil{return result,err}
  result.Extracted=true
 }
 if _,err=upload("script",req.ScriptPath);err!=nil{return result,err}
 result.ScriptImported=true
 // bash -e treats a simple failing command as a failure; the script should
 // additionally set -euo pipefail to handle pipelines and unset variables.
 command:="bash -e -- "+quoteGuest("./"+result.ScriptName)+" "+quoteGuest(result.SourceName)
 if _,err=execute("script",command);err!=nil{return result,err}
 result.ScriptSucceeded=true
 report("success","Transferencia e script concluidos com sucesso")
 return result,nil
}
