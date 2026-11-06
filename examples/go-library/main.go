// Example: orchestrate a disposable VM entirely through the InsulaVM Go module.
// Linux, SSH and Cloud-Init must already be installed in the selected QCOW2.
package main

import (
 "context"
 "fmt"
 "os"
 "time"

 "github.com/ahhvahh/InsulaVM/insulavm"
)

func main(){
 configPath:=os.Getenv("INSULAVM_CONFIG")
 if configPath==""{configPath="config.yaml"}
 engine,err:=insulavm.New(configPath)
 if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
 m:=&insulavm.Manifest{
  Profile:"debian-minimal",
  Repository:insulavm.Repository{Name:"example-repo"},
  Action:"checks",
  // Omit SSH to use global ssh.user (default: insula).
  // SSH: insulavm.SSHOptions{User:"buildagent"},
  // No destination: every transfer and job uses config.yaml guest.destination.
  Pipelines:[]insulavm.Pipeline{{
   Name:"validation",Jobs:[]insulavm.Job{{
    Name:"check-version",Commands:[]string{"uname -a","pwd"},
   }},
  }},
 }
 // SourcePath refers to an existing file/directory ON THE HOST.
 if source:=os.Getenv("INSULAVM_SOURCE");source!=""{
  m.Imports=[]insulavm.ImportSpec{{Source:source}}
 }
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Minute)
 defer cancel()
 result,err:=engine.Run(ctx,insulavm.RunRequest{
  Manifest:m,
  Output:os.Stdout,
  OnImportProgress:func(p insulavm.ImportProgress){
   fmt.Fprintf(os.Stderr,"Stage: %s; %d/%d bytes\n",p.Stage,p.BytesSent,p.TotalBytes)
  },
 })
 if result!=nil{
  fmt.Println("VM ID:",result.VMID,"SSH user:",result.SSHUser)
  fmt.Println("Logs:",result.LogDirectory)
 }
 if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}
