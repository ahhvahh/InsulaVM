package insulavm

import (
 "bytes"
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

type deployFake struct {
 steps []string
 failAt string
}

func (m *deployFake) ImportToVM(_ context.Context,req VMImportRequest)(*ImportResult,error){
 step:="import:"+filepath.Base(req.SourcePath)
 m.steps=append(m.steps,step)
 if m.failAt==step{return nil,errors.New("upload failed")}
 return &ImportResult{Imported:true,DestinationPath:"/tmp/insulavm/workspace",Files:1},nil
}

func (m *deployFake) ExecVM(_ context.Context,req ExecRequest)(*ExecResult,error){
 step:="exec:"+req.Command
 m.steps=append(m.steps,step)
 if m.failAt==step{return nil,errors.New("remote command exited 7")}
 return &ExecResult{VMID:req.VMID,LogDirectory:"/data/logs/deploy"},nil
}

func deployTestFiles(t *testing.T)(string,string){
 t.Helper()
 dir:=t.TempDir()
 source:=filepath.Join(dir,"pacote.tar.gz")
 script:=filepath.Join(dir,"pos-carga.sh")
 if err:=os.WriteFile(source,[]byte("test"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(script,[]byte("#!/bin/bash\nset -euo pipefail\n"),0600);err!=nil{t.Fatal(err)}
 return source,script
}

func TestVMDeployStagesInOrderAndSuccess(t *testing.T){
 source,script:=deployTestFiles(t)
 mock:=&deployFake{}
 var log bytes.Buffer
 got,err:=deployToVM(context.Background(),mock,VMDeployRequest{
  VMID:"example",SourcePath:source,ScriptPath:script,Extract:true,Output:&log,
 })
 if err!=nil{t.Fatal(err)}
 if !got.SourceImported||!got.Extracted||!got.ScriptImported||!got.ScriptSucceeded{
  t.Fatalf("missing successful deployment stages: %+v",got)
 }
 want:=[]string{
  "import:pacote.tar.gz",
  "exec:tar -xzf 'pacote.tar.gz'",
  "import:pos-carga.sh",
  "exec:bash -e -- './pos-carga.sh' 'pacote.tar.gz'",
 }
 if strings.Join(mock.steps,"|")!=strings.Join(want,"|"){
  t.Fatalf("wrong sequence: %#v",mock.steps)
 }
 if got.ScriptLogDirectory!="/data/logs/deploy"||got.DestinationPath!="/tmp/insulavm/workspace"{t.Fatalf("unexpected output: %+v",got)}
 if !strings.Contains(log.String(),"Transferencia e script concluidos com sucesso"){t.Fatal("success line missing")}
}

func TestVMDeployFailureNeverReportsSuccess(t *testing.T){
 source,script:=deployTestFiles(t)
 stages:=[]struct{
  fail string
  expectedCount int
 }{
  {"import:pacote.tar.gz",1},
  {"exec:tar -xzf 'pacote.tar.gz'",2},
  {"import:pos-carga.sh",3},
  {"exec:bash -e -- './pos-carga.sh' 'pacote.tar.gz'",4},
 }
 for _,tc:=range stages{
  t.Run(tc.fail,func(t *testing.T){
   mock:=&deployFake{failAt:tc.fail}
   var log bytes.Buffer
   result,err:=deployToVM(context.Background(),mock,VMDeployRequest{
    VMID:"example",SourcePath:source,ScriptPath:script,Extract:true,Output:&log,
   })
   if err==nil{t.Fatalf("failure not propagated for %s",tc.fail)}
   if result==nil{t.Fatal("missing partial result")}
   if result.ScriptSucceeded{t.Fatal("reported completed script after failure")}
   if strings.Contains(log.String(),"Transferencia e script concluidos com sucesso"){t.Fatal("false success message")}
   if len(mock.steps)!=tc.expectedCount{t.Fatalf("unexpected subsequent commands: %#v",mock.steps)}
  })
 }
}

func TestVMDeployPreflightRejectsBadFilesBeforeUpload(t *testing.T){
 source,script:=deployTestFiles(t)
 testCases:=[]struct{name string;source string;script string;extract bool}{
  {"missing source",filepath.Join(t.TempDir(),"no.tar.gz"),script,false},
  {"missing script",source,filepath.Join(t.TempDir(),"no.sh"),false},
  {"wrong extension",script,source,true},
  {"directory source",filepath.Dir(source),script,false},
  {"same source and script",source,source,false},
 }
 for _,tc:=range testCases{
  t.Run(tc.name,func(t *testing.T){
   mock:=&deployFake{}
   _,err:=deployToVM(context.Background(),mock,VMDeployRequest{
    VMID:"example",SourcePath:tc.source,ScriptPath:tc.script,Extract:tc.extract,
   })
   if err==nil{t.Fatal("invalid request accepted")}
   if len(mock.steps)!=0{t.Fatalf("remote operation occurred during preflight: %#v",mock.steps)}
  })
 }
}

func TestVMDeployQuotesShellArguments(t *testing.T){
 dir:=t.TempDir()
 source:=filepath.Join(dir,"big' archive.tar.gz")
 script:=filepath.Join(dir,"install it.sh")
 if err:=os.WriteFile(source,[]byte("test"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(script,[]byte("echo ok"),0600);err!=nil{t.Fatal(err)}
 mock:=&deployFake{}
 _,err:=deployToVM(context.Background(),mock,VMDeployRequest{
  VMID:"example",SourcePath:source,ScriptPath:script,Extract:true,
 })
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(mock.steps[1],"big'\"'\"' archive.tar.gz"){t.Fatalf("archive not shell-quoted: %s",mock.steps[1])}
 if !strings.Contains(mock.steps[3],"'./install it.sh'"){t.Fatalf("script not shell-quoted: %s",mock.steps[3])}
}
