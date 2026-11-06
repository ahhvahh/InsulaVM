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
 user:=fs.String("user","","SSH account override")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected import arguments")}
 if *host==""||*source==""{
  return errors.New("import requires -host and -source; destination comes from config.yaml guest.destination")
 }
 engine,err:=insulavm.New(*configPath)
 if err!=nil{return err}
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt)
 defer cancel()
 result,err:=engine.ImportPath(ctx,insulavm.ImportRequest{
  Host:*host,SourcePath:*source,User:*user,
  Output:os.Stdout,
 })
 if err!=nil{return err}
 fmt.Printf("InsulaVM: imported %d file(s), archive %d bytes -> %s\n",
  result.Files,result.CompressedBytes,result.DestinationPath)
 return nil
}
