// Command insula loads a global config, resolves a named VM profile and runs
// SSH jobs. Existing guests can be supplied via -host. Profile-based runs
// create disposable Libvirt/QEMU overlays; ISO installation is not included.
package main

import (
 "context"
 "errors"
 "flag"
 "fmt"
 "os"
 "os/signal"
 "path/filepath"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/executor"
 "github.com/ahhvahh/InsulaVM/pkg/manifest"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/storage"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
 "golang.org/x/crypto/ssh"
)

func main() {
 if err:=run(os.Args[1:]);err!=nil{
  fmt.Fprintln(os.Stderr,"InsulaVM:",err)
  os.Exit(1)
 }
}

func run(args []string)error{
 if len(args)>0{
  switch args[0]{
  case "config":return configCommand(args[1:])
  case "profile":return profileCommand(args[1:])
  case "run":return runAction(args[1:])
  case "help","-h","--help":
   fmt.Println("Usage: insula [run] -config config.yaml -manifest insula.yaml")
   fmt.Println("       insula config check -config config.yaml")
   fmt.Println("       insula profile list -config config.yaml")
   fmt.Println("       insula profile show -config config.yaml -name debian-go")
   fmt.Println("       insula profile register -config config.yaml -name debian-go -image installed.qcow2 -memory-mib 1024 -vcpus 1 -disk-gb 8")
   return nil
  }
 }
 return runAction(args)
}

func configCommand(args []string)error{
 if len(args)<1||args[0]!="check"{return errors.New("usage: insula config check [-config config.yaml]")}
 fs:=flag.NewFlagSet("config check",flag.ContinueOnError)
 filename:=fs.String("config","config.yaml","global config")
 if err:=fs.Parse(args[1:]);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected arguments")}
 c,err:=config.ReadFile(*filename)
 if err!=nil{return err}
 fmt.Printf("Valid: profiles=%s runs=%s logs=%s provider=%s\n",
  c.Storage.Profiles,c.Storage.Runs,c.Storage.Logs,c.Virtualization.Provider)
 return nil
}

func profileCommand(args []string)error{
 if len(args)==0{return errors.New("expected profile list, show or register")}
 cmd:=args[0]
 fs:=flag.NewFlagSet("profile "+cmd,flag.ContinueOnError)
 cfgPath:=fs.String("config","config.yaml","global config")
 name:=fs.String("name","","profile name")
 image:=fs.String("image","","fully installed QCOW2 source path")
 memory:=fs.Int("memory-mib",0,"guest RAM (MiB)")
 cpus:=fs.Int("vcpus",0,"guest vCPUs")
 disk:=fs.Int("disk-gb",0,"virtual disk capacity in GiB")
 distro:=fs.String("os","debian","OS label")
 iso:=fs.String("iso","","original ISO path (metadata only)")
 address:=fs.String("address","","existing guest SSH address (optional)")
 user:=fs.String("user","","guest SSH user (optional)")
 if err:=fs.Parse(args[1:]);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected profile arguments")}
 c,err:=config.ReadFile(*cfgPath)
 if err!=nil{return err}
 switch cmd{
 case "list":
  names,err:=profile.List(c.Storage.Profiles)
  if err!=nil{return err}
  if len(names)==0{fmt.Println("No registered profiles")}
  for _,n:=range names{fmt.Println(n)}
 case "show":
  p,err:=profile.Read(c.Storage.Profiles,*name)
  if err!=nil{return err}
  fmt.Printf("name=%s os=%s memory_mib=%d vcpus=%d disk_gb=%d version=%s image=%s\n",
   p.Name,p.OS,p.MemoryMiB,p.VCPUs,p.DiskGB,p.Version,p.Image)
 case "register":
  if *name==""||*image==""{return errors.New("profile register requires -name and -image")}
  if *disk==0{*disk=c.Virtualization.DefaultDiskGB}
  p:=profile.Profile{Name:*name,OS:*distro,MemoryMiB:*memory,VCPUs:*cpus,
   DiskGB:*disk,Version:"v001",Image:"versions/v001/base.qcow2",ISO:*iso,
   SSH:profile.SSH{Address:*address,User:*user}}
  if err:=profile.Register(c.Storage.Profiles,p,*image);err!=nil{return err}
  fmt.Printf("Profile registered: %s\n",filepath.Join(c.Storage.Profiles,p.Name,"profile.yaml"))
 default:return errors.New("expected profile list, show or register")
 }
 return nil
}

func runAction(args []string)(result error){
 fs:=flag.NewFlagSet("run",flag.ContinueOnError)
 cfgPath:=fs.String("config","config.yaml","global config path")
 manifestPath:=fs.String("manifest","insula.yaml","job manifest")
 logsOverride:=fs.String("logs-root","","override global log directory")
 hostOverride:=fs.String("host","","connect to an existing guest; do not start a VM")
 userOverride:=fs.String("user","","override SSH user")
 keyOverride:=fs.String("key","","override SSH private key")
 hostsOverride:=fs.String("known-hosts","","override SSH known_hosts")
 timeout:=fs.Duration("command-timeout",30*time.Minute,"per-command timeout")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected run arguments")}
 if *timeout<=0{return errors.New("command-timeout must be positive")}
 c,err:=config.ReadFile(*cfgPath)
 if err!=nil{return err}
 m,err:=manifest.ReadFile(*manifestPath)
 if err!=nil{return err}
 if err:=c.EnsureDirectories();err!=nil{return err}

 var p *profile.Profile
 if m.Profile!=""{
  p,err=profile.Read(c.Storage.Profiles,m.Profile)
  if err!=nil{return fmt.Errorf("read selected profile: %w",err)}
 }
 if p==nil&&*hostOverride==""{
  return errors.New("select a profile in insula.yaml or supply -host for an existing VM")
 }
 sshUser,sshKey,knownHosts:=c.SSH.User,c.SSH.PrivateKey,c.SSH.KnownHosts
 host:=*hostOverride
 if p!=nil{
  if host==""{host=p.SSH.Address}
  if p.SSH.User!=""{sshUser=p.SSH.User}
  if p.SSH.PrivateKey!=""{sshKey=p.SSH.PrivateKey}
  if p.SSH.KnownHosts!=""{knownHosts=p.SSH.KnownHosts}
 }
 if *userOverride!=""{sshUser=*userOverride}
 if *keyOverride!=""{sshKey=*keyOverride}
 if *hostsOverride!=""{knownHosts=*hostsOverride}
 if sshUser==""||sshKey==""||knownHosts==""{
  return errors.New("SSH user, private key and known_hosts are required in global/profile config or CLI flags")
 }

 logDir:=c.Storage.Logs
 if *logsOverride!=""{logDir=*logsOverride}
 run,err:=storage.NewRun(logDir,m.Repository.Name,m.GroupName(),time.Now())
 if err!=nil{return err}
 fmt.Println("Logs:",run.Directory)
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt)
 defer cancel()

 if host==""{
  instance,err:=vm.Start(ctx,c,p)
  if err!=nil{return err}
  host=instance.Address
  fmt.Printf("Started disposable VM %s on %s\n",instance.Name,host)
  defer func(){
   closeCtx,closeCancel:=context.WithTimeout(context.Background(),30*time.Second)
   defer closeCancel()
   result=errors.Join(result,instance.Cleanup(closeCtx))
  }()
 }

 client,err:=dialWithRetry(ctx,executor.DialConfig{
  Address:host,User:sshUser,PrivateKeyPath:sshKey,KnownHostsPath:knownHosts,
  Timeout:time.Duration(c.SSH.ConnectTimeoutSeconds)*time.Second,
 },time.Duration(c.SSH.ConnectTimeoutSeconds)*time.Second)
 if err!=nil{return err}
 defer client.Close()

 runner:=&executor.Executor{Client:client,Workspace:m.Workspace,
  Console:os.Stdout,CommandTimeout:*timeout}
 job:=func(name string,commands []string,allowFailure bool)error{
  log,err:=run.OpenJob(name)
  if err!=nil{return err}
  return errors.Join(runner.Run(ctx,name,commands,allowFailure,log),log.Close())
 }
 if len(m.Setup)>0{
  if err:=job("setup",m.Setup,false);err!=nil{return err}
 }
 for _,pipeline:=range m.Pipelines{
  for _,j:=range pipeline.Jobs{
   if err:=job(j.Name,j.Commands,j.AllowFailure);err!=nil{
    return fmt.Errorf("pipeline %s: %w",pipeline.Name,err)
   }
  }
 }
 fmt.Println("InsulaVM: all jobs completed")
 return nil
}

// Keep waiting if the IP is assigned before the SSH daemon is ready.
// Host-key mismatches cannot be bypassed and ultimately cause failure.
func dialWithRetry(ctx context.Context,c executor.DialConfig,wait time.Duration)(*ssh.Client,error){
 timed,cancel:=context.WithTimeout(ctx,wait)
 defer cancel()
 var last error
 for{
  client,err:=executor.Dial(c)
  if err==nil{return client,nil}
  last=err
  select{
  case <-timed.Done():return nil,fmt.Errorf("SSH guest unavailable: %w; last attempt: %v",timed.Err(),last)
  case <-time.After(2*time.Second):
  }
 }
}
