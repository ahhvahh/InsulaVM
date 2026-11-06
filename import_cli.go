package main

import (
 "context"
 "errors"
 "flag"
 "fmt"
 "os"
 "os/signal"

 "github.com/ahhvahh/InsulaVM/insulavm"
)

// importCommand imports one host file or directory into an already running VM.
func importCommand(args []string)error{
 fs:=flag.NewFlagSet("import",flag.ContinueOnError)
 configPath:=fs.String("config","config.yaml","global InsulaVM config")
 host:=fs.String("host","","already running guest SSH address (host:port)")
 source:=fs.String("source","","host file or directory to archive and transfer")
 destination:=fs.String("dest","","absolute directory inside guest to receive contents")
 user:=fs.String("user","","SSH account override")
 key:=fs.String("key","","SSH private key path override")
 knownHosts:=fs.String("known-hosts","","SSH known_hosts override")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected import arguments")}
 if *host==""||*source==""||*destination==""{
  return errors.New("import requires -host, -source and -dest")
 }
 engine,err:=insulavm.New(*configPath)
 if err!=nil{return err}
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt)
 defer cancel()
 result,err:=engine.ImportPath(ctx,insulavm.ImportRequest{
  Host:*host,SourcePath:*source,DestinationPath:*destination,
  User:*user,PrivateKey:*key,KnownHosts:*knownHosts,
  Output:os.Stdout,
 })
 if err!=nil{return err}
 fmt.Printf("InsulaVM: imported %d file(s), archive %d bytes -> %s\n",
  result.Files,result.CompressedBytes,result.DestinationPath)
 return nil
}
