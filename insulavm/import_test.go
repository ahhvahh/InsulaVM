package insulavm

import (
 "bytes"
 "context"
 "os"
 "net"
 "strconv"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestImportPathDirectoryProgressAndConfirmation(t *testing.T){
 host,key,hosts:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 source:=t.TempDir()
 if err:=os.Mkdir(filepath.Join(source,"nested"),0755);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(source,"nested","hello.txt"),[]byte("hello from host"),0600);err!=nil{t.Fatal(err)}
 dest:=filepath.Join(t.TempDir(),"guest output")
 engine.config.Guest.Destination=dest
 var out bytes.Buffer
 var events []ImportProgress
 result,err:=engine.ImportPath(context.Background(),ImportRequest{
  SourcePath:source,Host:host,User:"test",PrivateKey:key,
  KnownHosts:hosts,Output:&out,OnProgress:func(p ImportProgress){events=append(events,p)},
 })
 if err!=nil{t.Fatal(err)}
 if !result.Imported || result.Files!=1 || result.CompressedBytes<1{
  t.Fatalf("result: %+v",result)
 }
 data,err:=os.ReadFile(filepath.Join(dest,"nested","hello.txt"))
 if err!=nil{t.Fatalf("guest file not extracted: %v",err)}
 if string(data)!="hello from host"{t.Fatalf("guest file content: %q",data)}
 stages:=make([]string,0,len(events))
 for _,e:=range events{stages=append(stages,e.Stage)}
 for _,stage:=range []string{"connecting","validating","compressing","compressed","uploading","verifying","extracting","completed"}{
  found:=false
  for _,s:=range stages{if s==stage{found=true}}
  if !found{t.Fatalf("missing stage %q in %v",stage,stages)}
 }
 if events[len(events)-1].Stage!="completed"{t.Fatalf("final stage not completed: %v",stages)}
 if !strings.Contains(out.String(),"Arquivo importado com sucesso") ||
    !strings.Contains(out.String(),"[InsulaVM][import:extracting]"){
  t.Fatalf("console did not receive stage text: %s",out.String())
 }
}

func TestImportPathSingleFile(t *testing.T){
 host,key,hosts:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 source:=filepath.Join(t.TempDir(),"single.sh")
 if err:=os.WriteFile(source,[]byte("#!/bin/sh\necho okay\n"),0700);err!=nil{t.Fatal(err)}
 dest:=filepath.Join(t.TempDir(),"guest")
 engine.config.Guest.Destination=dest
 result,err:=engine.ImportPath(context.Background(),ImportRequest{
  SourcePath:source,Host:host,User:"test",PrivateKey:key,KnownHosts:hosts,
 })
 if err!=nil{t.Fatal(err)}
 if result.Files!=1||!result.Imported{t.Fatalf("result %+v",result)}
 data,err:=os.ReadFile(filepath.Join(dest,"single.sh"))
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(data),"echo okay"){t.Fatalf("invalid contents %s",data)}
}

func TestRunImportsBeforeJobs(t *testing.T){
 host,key,hosts:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 source:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(source,"marker.txt"),[]byte("import-before-job"),0600);err!=nil{t.Fatal(err)}
 dest:=filepath.Join(t.TempDir(),"guest")
 engine.config.Guest.Destination=dest
 var out bytes.Buffer
 var stages []string
 m:=&Manifest{
  Repository:Repository{Name:"integration"},
  Action:"import-check",
  Imports:[]ImportSpec{{Source:source}},
  Pipelines:[]Pipeline{{Name:"checks",Jobs:[]Job{{
   Name:"verify",Commands:[]string{"test -f '"+filepath.ToSlash(filepath.Join(dest,"marker.txt"))+"'","echo verified"},
  }}}},
 }
 result,err:=engine.Run(context.Background(),RunRequest{
  Manifest:m,Host:host,User:"test",PrivateKey:key,KnownHosts:hosts,
  Output:&out,CommandTimeout:5*time.Second,
  OnImportProgress:func(p ImportProgress){stages=append(stages,p.Stage)},
 })
 if err!=nil{t.Fatal(err)}
 if len(result.Imports)!=1||!result.Imports[0].Imported{
  t.Fatalf("missing successful import result: %+v",result)
 }
 if len(result.CompletedJobs)!=1||result.CompletedJobs[0]!="verify"{
  t.Fatalf("jobs were not executed: %+v",result.CompletedJobs)
 }
 if !strings.Contains(out.String(),"verified")||!strings.Contains(out.String(),"Arquivo importado com sucesso"){
  t.Fatalf("missing console progress or job output: %s",out.String())
 }
 if len(stages)==0||stages[len(stages)-1]!="completed"{t.Fatalf("structured progress: %v",stages)}
}

func TestImportPathReportsFailure(t *testing.T){
 host,key,hosts:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 var events []ImportProgress
 _,err:=engine.ImportPath(context.Background(),ImportRequest{
  SourcePath:filepath.Join(t.TempDir(),"notfound"),
  Host:host,User:"test",PrivateKey:key,
  KnownHosts:hosts,OnProgress:func(p ImportProgress){events=append(events,p)},
 })
 if err==nil{t.Fatal("expected nonexistent source error")}
 if len(events)==0||events[len(events)-1].Stage!="failed"{
  t.Fatalf("expected failed callback, got: %v",events)
 }
}

func TestImportToVMByID(t *testing.T){
 host,key,hosts:=runTestSSHServer(t)
 engine:=testEngine(t,"")
 ctx:=context.Background()
 addr,portText,err:=net.SplitHostPort(host)
 if err!=nil{t.Fatal(err)}
 port,err:=strconv.Atoi(portText)
 if err!=nil{t.Fatal(err)}
 store,err:=registry.Open(engine.config.Storage.Database)
 if err!=nil{t.Fatal(err)}
 id:="019a5d11-1234-7123-8123-123456789abc"
 fakeRunningLibvirt(t,id)
 entry:=registry.Entry{
  ID:id,LibvirtUUID:id,Name:"insula-importer",Profile:"test",IP:addr,
  MAC:"52:54:00:ff:00:00",SSHPort:port,SSHUser:"test",
  PrivateKeyPath:key,KnownHostsPath:hosts,InstanceDir:t.TempDir(),
 }
 if err:=store.Insert(ctx,entry);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(ctx,id,addr);err!=nil{t.Fatal(err)}
 _=store.Close()
 source:=filepath.Join(t.TempDir(),"uploaded.txt")
 if err:=os.WriteFile(source,[]byte("transferred by VM ID"),0600);err!=nil{t.Fatal(err)}
 dest:=filepath.Join(t.TempDir(),"guest")
 engine.config.Guest.Destination=dest
 var stages []string
 result,err:=engine.ImportToVM(ctx,VMImportRequest{
  VMID:id,SourcePath:source,
  OnProgress:func(p ImportProgress){stages=append(stages,p.Stage)},
 })
 if err!=nil{t.Fatal(err)}
 if !result.Imported||len(stages)==0||stages[len(stages)-1]!="completed"{
  t.Fatalf("missing VM import completion: %+v %v",result,stages)
 }
 data,err:=os.ReadFile(filepath.Join(dest,"uploaded.txt"))
 if err!=nil||string(data)!="transferred by VM ID"{t.Fatalf("bad remote contents %q, %v",data,err)}
 // A second upload uses the exact same global destination without any
 // destination parameter; it must preserve already-imported files.
 next:=filepath.Join(t.TempDir(),"additional.txt")
 if err:=os.WriteFile(next,[]byte("additional"),0600);err!=nil{t.Fatal(err)}
 second,err:=engine.ImportToVM(ctx,VMImportRequest{VMID:id,SourcePath:next})
 if err!=nil{t.Fatal(err)}
 if second.DestinationPath!=dest{t.Fatalf("unexpected second import destination %q",second.DestinationPath)}
 for name,want:=range map[string]string{"uploaded.txt":"transferred by VM ID","additional.txt":"additional"}{
  content,err:=os.ReadFile(filepath.Join(dest,name))
  if err!=nil||string(content)!=want{t.Fatalf("file %s = %q, error %v",name,content,err)}
 }
}
