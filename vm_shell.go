package main

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path"
 "strconv"
 "strings"

 "github.com/ahhvahh/InsulaVM/insulavm"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// vmShellArgs opens an interactive SSH terminal with strict host key checking.
// -directory is a subdirectory of guest.destination, never a host path.
func vmShellArgs(record registry.Entry, workspace,directory string)([]string,error){
 if record.IP==""||record.SSHUser==""||record.SSHPort<1||record.SSHPort>65535 {
  return nil,errors.New("VM sem IP, usuario ou porta SSH validos")
 }
 if record.PrivateKeyPath==""||record.KnownHostsPath==""{
  return nil,errors.New("VM sem caminhos de chave SSH e known_hosts")
 }
 if !path.IsAbs(workspace)||path.Clean(workspace)=="/"{
  return nil,errors.New("guest.destination precisa ser um diretorio absoluto")
 }
 cleaned:=path.Clean(directory)
 if path.IsAbs(directory)||cleaned==".."||strings.HasPrefix(cleaned,"../"){
  return nil,errors.New("-directory deve ser relativo a guest.destination, sem escapar com ..")
 }
 guestDir:=path.Join(workspace,cleaned)
 remote:="cd -- "+shellQuote(guestDir)+" && exec \"${SHELL:-/bin/sh}\" -l"
 return []string{
  "-tt",
  "-i",record.PrivateKeyPath,
  "-o","UserKnownHostsFile="+record.KnownHostsPath,
  "-o","StrictHostKeyChecking=yes",
  "-o","IdentitiesOnly=yes",
  "-p",strconv.Itoa(record.SSHPort),
  record.SSHUser+"@"+record.IP,
  remote,
 },nil
}

func vmShell(ctx context.Context,engine *insulavm.Engine,vmID,directory string)error{
 if vmID==""{return errors.New("vm shell requires -id")}
 checked,err:=engine.GetVMStatus(ctx,vmID)
 if err!=nil{return fmt.Errorf("consultar VM: %w",err)}
 if !checked.IsRunning()||checked.Record.Status!="running"{
  return fmt.Errorf("VM nao esta pronta para acesso (SQLite: %s; Libvirt: %s)",checked.Record.Status,checked.LibvirtState)
 }
 record:=checked.Record
 for _,file:=range []string{record.PrivateKeyPath,record.KnownHostsPath}{
  if _,err:=os.Stat(file);err!=nil{return fmt.Errorf("arquivo SSH indisponivel (%s): %w",localPathForCLI(file),err)}
 }
 args,err:=vmShellArgs(record,engine.Configuration().Guest.Destination,directory)
 if err!=nil{return err}
 fmt.Fprintf(os.Stderr,"[InsulaVM][vm:shell] Abrindo terminal SSH: %s@%s\n",record.SSHUser,record.IP)
 cmd:=exec.CommandContext(ctx,"ssh",args...)
 cmd.Stdin=os.Stdin
 cmd.Stdout=os.Stdout
 cmd.Stderr=os.Stderr
 return cmd.Run()
}
