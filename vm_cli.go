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

func vmCommand(args []string)error{
 if len(args)==0{return errors.New("usage: insula vm start|list|show|stop")}
 command:=args[0]
 fs:=flag.NewFlagSet("vm "+command,flag.ContinueOnError)
 cfgPath:=fs.String("config","config.yaml","InsulaVM global settings")
 profileName:=fs.String("profile","","installed cloud-init-ready QCOW2 profile")
 id:=fs.String("id","","VM UUIDv7")
 if err:=fs.Parse(args[1:]);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected vm arguments")}
 engine,err:=insulavm.New(*cfgPath)
 if err!=nil{return err}
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt)
 defer cancel()
 switch command{
 case "start":
  if *profileName==""{return errors.New("vm start requires -profile")}
  instance,err:=engine.StartVM(ctx,*profileName)
  if err!=nil{return err}
  fmt.Printf("VM pronta: id=%s profile=%s ip=%s mac=%s user=%s sudo=%s\n",
   instance.ID,instance.Profile,instance.IPAddress,instance.MACAddress,instance.SSHUser,instance.SudoMode)
  fmt.Printf("SSH: ssh -i %q -o UserKnownHostsFile=%q %s@%s\n",
   instance.SSHPrivateKeyPath,instance.SSHKnownHostsPath,instance.SSHUser,instance.IPAddress)
  fmt.Println("Para encerrar: insula vm stop -id",instance.ID)
 case "list":
  entries,err:=engine.ListVMs(ctx)
  if err!=nil{return err}
  for _,e:=range entries{
   fmt.Printf("%s %-12s %-18s %-20s %s\n",e.ID,e.Status,e.IP,e.Profile,e.Name)
  }
 case "show":
  if *id==""{return errors.New("vm show requires -id")}
  e,err:=engine.GetVM(ctx,*id)
  if err!=nil{return err}
  fmt.Printf("id=%s\nname=%s\nprofile=%s\nlibvirt_uuid=%s\nip=%s\nmac=%s\nuser=%s\nsudo_mode=%s\nhost_fingerprint=%s\nstatus=%s\ncreated_at=%s\n",
   e.ID,e.Name,e.Profile,e.LibvirtUUID,e.IP,e.MAC,e.SSHUser,e.SudoMode,e.HostFingerprint,e.Status,e.CreatedAt)
  if e.Status=="running"{
   fmt.Printf("ssh_key=%s\nknown_hosts=%s\n",e.PrivateKeyPath,e.KnownHostsPath)
  }
 case "stop":
  if *id==""{return errors.New("vm stop requires -id")}
  if err:=engine.StopVM(ctx,*id);err!=nil{return err}
  fmt.Println("VM encerrada:",*id)
 default:return errors.New("usage: insula vm start|list|show|stop")
 }
 return nil
}
