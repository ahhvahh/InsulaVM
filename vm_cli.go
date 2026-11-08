package main

import (
 "context"
 "errors"
 "flag"
 "fmt"
 "os"
 "os/signal"
 "time"

 "github.com/ahhvahh/InsulaVM/insulavm"
)

func vmCommand(args []string)error{
 if len(args)==0{return errors.New("usage: insula vm start|start-iso|list|status|shell|console|stop|stop-all|freeze|exec|import|deploy")}
 command:=args[0]
 fs:=flag.NewFlagSet("vm "+command,flag.ContinueOnError)
 cfgPath:=fs.String("config","config.yaml","InsulaVM global settings")
 profileName:=fs.String("profile","","registered, reusable cloud-init-ready QCOW2 profile")
 isoPath:=fs.String("iso","","path to a prepared Debian Live ISO (not an installer)")
 startupScript:=fs.String("startup-script","","absolute path to trusted first-boot script (Live ISO only)")
 memoryMiB:=fs.Int("memory-mib",0,"override guest RAM in MiB for this VM only; 0 keeps profile default")
 vcpus:=fs.Int("vcpus",0,"override guest vCPUs for this VM only; 0 keeps profile default")
 graceTimeout:=fs.Duration("grace-timeout",30*time.Second,"VM graceful shutdown deadline before escalation")
 forceAfterTimeout:=fs.Bool("force-after-timeout",true,"use hard destroy after graceful shutdown deadline")
 dryRun:=fs.Bool("dry-run",false,"list managed VMs that would be stopped by stop-all without changing them")
 id:=fs.String("id","","VM UUIDv7")
 freezeName:=fs.String("name","","unique name for a frozen VM profile")
 shutdownTimeout:=fs.Duration("shutdown-timeout",3*time.Minute,"graceful guest shutdown deadline for freeze")
 commandText:=fs.String("command","","shell command inside the VM")
 workspace:=fs.String("workspace","","guest working directory; default: config.yaml guest.destination")
 directory:=fs.String("directory","","guest subdirectory for interactive vm shell (relative to guest.destination)")
 sudo:=fs.Bool("sudo",false,"execute as root via sudo -n (requires passwordless mode)")
 source:=fs.String("source","","host file/directory to import")
 script:=fs.String("script","","local Bash file to upload and execute after deploy")
 extract:=fs.Bool("extract",false,"extract trusted .tar.gz/.tgz source in guest.destination before executing script")
 cmdTimeout:=fs.Duration("command-timeout",30*time.Minute,"timeout for each remote deploy step")
 if err:=fs.Parse(args[1:]);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected vm arguments")}
 engine,err:=insulavm.New(*cfgPath)
 if err!=nil{return err}
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt)
 defer cancel()
 root:=engine.Configuration().Storage.Root
 switch command{
 case "start":
  if *profileName==""{return errors.New("vm start requires -profile")}
  current,err:=engine.ListVMStatus(ctx)
  if err!=nil{return fmt.Errorf("verificar instancias ja existentes no Libvirt: %w",err)}
  for _,entry:=range current{
   if entry.Record.Profile==*profileName&&entry.IsRunning(){
    fmt.Fprintf(os.Stderr,"[InsulaVM][aviso] VM do mesmo perfil ja esta ativa: %s (IP %s). 'vm start' cria uma NOVA instancia descartavel.\n",entry.Record.ID,entry.Record.IP)
   }
  }
  instance,err:=engine.StartVMWithOptions(ctx,insulavm.VMStartOptions{
   Profile:*profileName,MemoryMiB:*memoryMiB,VCPUs:*vcpus,
  },func(p insulavm.VMProgress){
   fmt.Fprintf(os.Stderr,"[InsulaVM][vm:%s] %s\n",p.Stage,localMessageForCLI(p.Message,root))
  })
  if err!=nil{return presentCLIError(err,root)}
  fmt.Printf("VM pronta: id=%s profile=%s ip=%s mac=%s user=%s sudo=%s\n",
   instance.ID,instance.Profile,instance.IPAddress,instance.MACAddress,instance.SSHUser,instance.SudoMode)
  fmt.Println("SSH:",sshCommandForCLI(instance.SSHPrivateKeyPath,instance.SSHKnownHostsPath,instance.SSHUser,instance.IPAddress))
  fmt.Println("Para encerrar: insula vm stop -id",instance.ID)
 case "start-iso":
  if *profileName==""{return errors.New("vm start-iso requires -profile")}
  instance,err:=engine.StartVMFromISO(ctx,insulavm.LiveISOStartOptions{Profile:*profileName,ISOPath:*isoPath,MemoryMiB:*memoryMiB,VCPUs:*vcpus,StartupScriptPath:*startupScript})
  if err!=nil{return presentCLIError(err,root)}
  fmt.Printf("VM LIVE pronta: id=%s profile=%s ip=%s user=%s\n",instance.ID,instance.Profile,instance.IPAddress,instance.SSHUser)
  fmt.Println("SSH:",sshCommandForCLI(instance.SSHPrivateKeyPath,instance.SSHKnownHostsPath,instance.SSHUser,instance.IPAddress))
 case "console":
  if *id==""{return errors.New("vm console requires -id")}
  return engine.OpenConsole(ctx,*id,os.Stdin,os.Stdout,os.Stderr)
 case "list":
  entries,err:=engine.ListVMStatus(ctx)
  if err!=nil{return err}
  if len(entries)==0{fmt.Println("Nenhuma VM cadastrada pelo InsulaVM");return nil}
  fmt.Printf("%-36s %-12s %-12s %-18s %-15s %s\n","UUID","SQLite","Libvirt","IP","Perfil","Nome")
  for _,entry:=range entries{
   e:=entry.Record
   fmt.Printf("%-36s %-12s %-12s %-18s %-15s %s\n",e.ID,e.Status,entry.LibvirtState,e.IP,e.Profile,e.Name)
   if entry.Warning!=""{fmt.Fprintln(os.Stderr,"  [aviso]",entry.Warning)}
  }
 case "status":
  if *id==""{return errors.New("vm status requires -id")}
  checked,err:=engine.GetVMStatus(ctx,*id)
  if err!=nil{return err}
  e:=checked.Record
  fmt.Printf("id=%s\nname=%s\nprofile=%s\nlibvirt_uuid=%s\nip=%s\nmac=%s\nuser=%s\nsudo_mode=%s\nssh_port=%d\nstatus_sqlite=%s\nstatus_libvirt=%s\ncreated_at=%s\n",
   e.ID,e.Name,e.Profile,e.LibvirtUUID,e.IP,e.MAC,e.SSHUser,e.SudoMode,e.SSHPort,e.Status,checked.LibvirtState,e.CreatedAt)
  if checked.Warning!=""{fmt.Fprintln(os.Stderr,"[aviso]",checked.Warning)}
  if checked.IsRunning(){
   fmt.Printf("ssh_key=%s\nknown_hosts=%s\n",localPathForCLI(e.PrivateKeyPath),localPathForCLI(e.KnownHostsPath))
   fmt.Println("SSH:",sshCommandForCLI(e.PrivateKeyPath,e.KnownHostsPath,e.SSHUser,e.IP))
  }
 case "shell":
  return vmShell(ctx,engine,*id,*directory)
 case "exec":
  if *id==""||*commandText==""{return errors.New("vm exec requires -id and -command")}
  result,err:=engine.ExecVM(ctx,insulavm.ExecRequest{
   VMID:*id,Command:*commandText,Workspace:*workspace,Sudo:*sudo,Output:os.Stdout,
  })
  if result!=nil{fmt.Println("Logs:",localPathForCLI(result.LogDirectory))}
  return presentCLIError(err,root)
 case "import":
  if *id==""||*source==""{return errors.New("vm import requires -id and -source; destination comes from config.yaml")}
  result,err:=engine.ImportToVM(ctx,insulavm.VMImportRequest{
   VMID:*id,SourcePath:*source,Output:os.Stdout,
  })
  if err!=nil{return err}
  fmt.Printf("Imported %d file(s) into VM %s at %s\n",result.Files,*id,result.DestinationPath)
  return nil
 case "deploy":
  if *id==""||*source==""||*script==""{
   return errors.New("vm deploy requires -id, -source and -script")
  }
  result,err:=engine.DeployToVM(ctx,insulavm.VMDeployRequest{
   VMID:*id,SourcePath:*source,ScriptPath:*script,Extract:*extract,Sudo:*sudo,
   Output:os.Stdout,CommandTimeout:*cmdTimeout,
  })
  if result!=nil && result.ScriptLogDirectory!=""{
   fmt.Println("Logs:",localPathForCLI(result.ScriptLogDirectory))
  }
  if err!=nil{return presentCLIError(err,root)}
  fmt.Printf("DEPLOY SUCCESS: vm=%s arquivo=%s script=%s status=success\n",result.VMID,result.SourceName,result.ScriptName)
  return nil
 case "freeze":
  if *id==""||*freezeName==""{return errors.New("vm freeze requires -id and -name")}
  result,err:=engine.FreezeVM(ctx,insulavm.VMFreezeRequest{
   VMID:*id,Name:*freezeName,ShutdownTimeout:*shutdownTimeout,
   OnProgress:func(p insulavm.VMFreezeProgress){
    fmt.Fprintf(os.Stderr,"[InsulaVM][freeze:%s] %s\n",p.Stage,localMessageForCLI(p.Message,root))
   },
  })
  if result!=nil&&result.ImagePath!=""{
   fmt.Printf("Perfil congelado: %s imagem=%s origem_encerrada=%t\n",result.ProfileName,localPathForCLI(result.ImagePath),result.SourceCleaned)
  }
  if err!=nil{return presentCLIError(err,root)}
  fmt.Printf("FREEZE SUCCESS: name=%s source=%s\n",result.ProfileName,result.VMID)
  return nil
 case "stop-all":
  if *id!=""{return errors.New("vm stop-all does not accept -id; use vm stop -id UUID instead")}
  result,err:=engine.StopAllVMs(ctx,insulavm.VMStopAllOptions{
   DryRun:*dryRun,
   OnProgress:func(p insulavm.VMStopAllProgress){
    fmt.Fprintf(os.Stderr,"[InsulaVM][stop-all:%s] %s %s\n",p.Stage,p.VMID,p.Message)
   },
  })
  if result!=nil{
   if *dryRun{
    fmt.Printf("STOP-ALL DRY-RUN: registradas=%d alvos=%d ignoradas=%d falhas=%d\n",result.Checked,len(result.Targets),len(result.Skipped),len(result.Failed))
   }else{
    fmt.Printf("STOP-ALL: registradas=%d encerradas=%d ignoradas=%d falhas=%d\n",result.Checked,len(result.Stopped),len(result.Skipped),len(result.Failed))
   }
  }
  return presentCLIError(err,root)
 case "stop":
  if *id==""{return errors.New("vm stop requires -id")}
  result,err:=engine.StopVMWithOptions(ctx,insulavm.VMStopOptions{VMID:*id,GracePeriod:*graceTimeout,ForceAfterTimeout:*forceAfterTimeout})
  if err!=nil{return err}
  fmt.Printf("VM encerrada: %s graceful=%t forced=%t timed_out=%t\n",*id,result.Graceful,result.Forced,result.TimedOut)
 default:return errors.New("usage: insula vm start|list|status|shell|stop|stop-all|freeze|exec|import|deploy")
 }
 return nil
}
