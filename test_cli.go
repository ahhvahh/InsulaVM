package main

import (
 "context"
 "errors"
 "flag"
 "fmt"
 "os"
 "os/exec"
 "os/signal"
 "path/filepath"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/insulavm"
)

func testCommand(args []string)error{
 usage:="usage: insula test doctor|download|install|boot|ssh|shell|iso|verify|cycle [-config config.yaml] [options]"
 if len(args)==0{return errors.New(usage)}
 action:=args[0]
 fs:=flag.NewFlagSet("test "+action,flag.ContinueOnError)
 cfgPath:=fs.String("config","config.yaml","InsulaVM config path")
 id:=fs.String("id","","managed VM UUID")
 profile:=fs.String("profile","","profile name")
 name:=fs.String("name","","name of frozen profile / ISO")
 iso:=fs.String("iso","","installer ISO path for install/cycle, or Live ISO path for verify")
 outputDir:=fs.String("output-dir","","mini.iso download/cache directory (defaults to data/isos)")
 memory:=fs.Int("memory-mib",2048,"RAM for installer or startup")
 vcpus:=fs.Int("vcpus",2,"CPU for installer or startup")
 disk:=fs.Int("disk-gb",12,"installer disk GiB")
 startup:=fs.String("startup-script","","absolute trusted script to run as root during first boot")
 sshTimeout:=fs.Duration("ssh-ready-timeout",5*time.Minute,"maximum SSH readiness time after DHCP")
 commandTimeout:=fs.Duration("command-timeout",30*time.Minute,"remote command timeout")
 installTimeout:=fs.Duration("install-timeout",90*time.Minute,"Debian installation timeout")
 confirm:=fs.Bool("confirm",false,"confirm destructive ISO export (stops/removes source VM)")
 verify:=fs.Bool("verify",true,"boot exported Live ISO, run check and stop verification VM")
 if err:=fs.Parse(args[1:]);err!=nil{return err}
 if fs.NArg()!=0{return errors.New("unexpected test arguments")}
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt)
 defer stop()
 engine,err:=insulavm.New(*cfgPath)
 if err!=nil{return err}
 config:=engine.Configuration()
 if *outputDir==""{*outputDir=filepath.Join(config.Storage.Root,"isos")}
 switch action {
 case "doctor":
  return testHostDoctor(ctx,engine)
 case "download":
  path,err:=insulavm.DownloadDebianMiniISO(ctx,*outputDir)
  if err!=nil{return err}
  fmt.Printf("TEST DOWNLOAD OK iso=%s\n",path)
  return nil
 case "install":
  if *profile==""{return errors.New("test install requires -profile")}
  path:=*iso
  if path==""{
   path,err=insulavm.DownloadDebianMiniISO(ctx,*outputDir)
   if err!=nil{return err}
  }
  return testInstall(ctx,engine,*profile,path,*memory,*vcpus,*disk,*installTimeout)
 case "boot":
  if *profile==""{return errors.New("test boot requires -profile")}
  _,err=testBoot(ctx,engine,*profile,*memory,*vcpus,*startup,*sshTimeout)
  return err
 case "ssh":
  if *id==""{return errors.New("test ssh requires -id")}
  result,err:=engine.DiagnoseVMSSH(ctx,*id,15*time.Second)
  if result!=nil{
   fmt.Printf("TEST SSH id=%s state=%s registry=%s ip=%s user=%s authenticated=%t sudo_ready=%t\n",
    result.VMID,result.LibvirtState,result.RegistryState,result.IP,result.SSHUser,result.Authenticated,result.SudoReady)
  }
  if err!=nil{return err}
  fmt.Println("TEST SSH OK")
  return nil
 case "shell":
  if *id==""{return errors.New("test shell requires -id")}
  return vmShell(ctx,engine,*id,"")
 case "iso":
  if *id==""||*name==""{return errors.New("test iso requires -id and -name")}
  if !*confirm{return errors.New("test iso stops/removes source VM; add -confirm explicitly")}
  return testExportISO(ctx,engine,*id,*name,*verify,*commandTimeout)
 case "verify":
  if *profile==""{return errors.New("test verify requires -profile")}
  return testVerifyLiveISO(ctx,engine,*profile,*iso)
 case "cycle":
  if *profile==""||*name==""{return errors.New("test cycle requires -profile and -name")}
  if *sshTimeout<0||*sshTimeout>time.Hour{return errors.New("invalid -ssh-ready-timeout")}
  if _,err=engine.GetProfile(*profile);err!=nil{
   path:=*iso
   if path==""{
    path,err=insulavm.DownloadDebianMiniISO(ctx,*outputDir)
    if err!=nil{return fmt.Errorf("download installer: %w",err)}
   }
   if err:=testInstall(ctx,engine,*profile,path,*memory,*vcpus,*disk,*installTimeout);err!=nil{return err}
  }
  vmID,err:=testBoot(ctx,engine,*profile,*memory,*vcpus,*startup,*sshTimeout)
  if err!=nil{return err}
  if !*confirm {
   fmt.Printf("VM pronta para configurar: ./insula test shell -id %s\n",vmID)
   fmt.Printf("Ao finalizar: ./insula test iso -id %s -name %s -confirm\n",vmID,*name)
   return nil
  }
  return testExportISO(ctx,engine,vmID,*name,*verify,*commandTimeout)
 default:
  return errors.New(usage)
 }
}

func testHostDoctor(ctx context.Context,e *insulavm.Engine)error{
 required:=[]string{"virsh","qemu-img","virt-install","guestmount","guestunmount","openssl","ssh"}
 failed:=false
 for _,tool:=range required{
  if path,err:=exec.LookPath(tool);err!=nil{
   fmt.Printf("MISSING %s\n",tool);failed=true
  }else{fmt.Printf("OK %s=%s\n",tool,path)}
 }
 if stat,err:=os.Stat("/dev/kvm");err!=nil||stat.Mode()&os.ModeCharDevice==0{
  fmt.Println("MISSING /dev/kvm");failed=true
 }else{fmt.Println("OK /dev/kvm")}
 cfg:=e.Configuration()
 output,err:=exec.CommandContext(ctx,"virsh","-c",cfg.Virtualization.LibvirtURI,
  "net-info",cfg.Virtualization.Network).CombinedOutput()
 if err!=nil{
  fmt.Printf("MISSING libvirt_network=%s: %s\n",cfg.Virtualization.Network,strings.TrimSpace(string(output)))
  failed=true
 }else{fmt.Printf("OK libvirt_network=%s\n",cfg.Virtualization.Network)}
 if failed{return errors.New("TEST DOCTOR FAILED: correct host dependencies before installing")}
 fmt.Println("TEST DOCTOR OK")
 return nil
}

func testInstall(ctx context.Context,e *insulavm.Engine,profile,iso string,memory,vcpus,disk int,timeout time.Duration)error{
 if !filepath.IsAbs(iso){return errors.New("installer ISO must have an absolute path")}
 if _,err:=e.GetProfile(profile);err==nil{
  fmt.Printf("TEST INSTALL SKIPPED: existing profile=%s\n",profile)
  return nil
 }
 result,err:=e.CreateProfileFromISO(ctx,insulavm.CreateProfileFromISORequest{
  Name:profile,ISOPath:iso,MemoryMiB:memory,VCPUs:vcpus,DiskGB:disk,Timeout:timeout,
  OnProgress:func(p insulavm.ProfileISOProgress){
   fmt.Fprintf(os.Stderr,"[test:install:%s] %s\n",p.Stage,p.Message)
  },
 })
 if err!=nil{return err}
 fmt.Printf("TEST INSTALL OK profile=%s qcow2=%s\n",result.Profile.Name,result.ImagePath)
 return nil
}

func testBoot(ctx context.Context,e *insulavm.Engine,profile string,memory,vcpus int,script string,sshReadyTimeout time.Duration)(string,error){
 lastID:=""
 instance,err:=e.StartVMWithOptions(ctx,insulavm.VMStartOptions{
  Profile:profile,MemoryMiB:memory,VCPUs:vcpus,
  StartupScriptPath:script,SSHReadyTimeout:sshReadyTimeout,PreserveOnFailure:true,
 },func(p insulavm.VMProgress){
  if p.ID!=""{lastID=p.ID}
  fmt.Fprintf(os.Stderr,"[test:boot:%s] %s\n",p.Stage,p.Message)
 })
 if err!=nil{
  if lastID!=""{
   return "",fmt.Errorf("%w; VM identificada como %s; se preservada, diagnostique com './insula test ssh -id %s' ou './insula vm console -id %s' e encerre com './insula vm stop -id %s'",err,lastID,lastID,lastID,lastID)
  }
  return "",err
 }
 fmt.Printf("TEST BOOT OK id=%s ip=%s profile=%s\n",instance.ID,instance.IPAddress,profile)
 fmt.Printf("Acessar: ./insula test shell -id %s\n",instance.ID)
 fmt.Printf("Diagnosticar: ./insula test ssh -id %s\n",instance.ID)
 return instance.ID,nil
}

func testExportISO(ctx context.Context,e *insulavm.Engine,id,name string,verify bool,commandTimeout time.Duration)error{
 result,err:=e.ExportVMToLiveISO(ctx,insulavm.VMExportISORequest{
  VMID:id,Name:name,PrepareLiveBoot:true,CommandTimeout:commandTimeout,
  OnProgress:func(p insulavm.VMFreezeProgress){
   fmt.Fprintf(os.Stderr,"[test:iso:%s] %s\n",p.Stage,p.Message)
  },
 })
 if err!=nil{
  if result!=nil&&result.Freeze!=nil&&result.Freeze.ImagePath!=""{
   fmt.Printf("TEST ISO PROFILE SAVED name=%s qcow2=%s\n",name,result.Freeze.ImagePath)
   fmt.Printf("Para repetir exportacao: ./insula profile export-iso -name %s\n",name)
  }
  return err
 }
 fmt.Printf("TEST ISO CREATED profile=%s iso=%s\n",name,result.ISOPath)
 if !verify{return nil}
 return testVerifyLiveISO(ctx,e,name,result.ISOPath)
}

func testVerifyLiveISO(ctx context.Context,e *insulavm.Engine,profile,iso string)(runErr error){
 instance,err:=e.StartVMFromISO(ctx,insulavm.LiveISOStartOptions{Profile:profile,ISOPath:iso})
 if err!=nil{return fmt.Errorf("boot Live ISO: %w",err)}
 defer func(){
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
  defer cancel()
  if err:=e.StopVM(cleanupCtx,instance.ID);err!=nil{
   runErr=errors.Join(runErr,fmt.Errorf("stop verification VM %s: %w",instance.ID,err))
  }
 }()
 _,err=e.ExecVM(ctx,insulavm.ExecRequest{
  VMID:instance.ID,Command:"test -r /etc/debian_version && cat /etc/debian_version",
  Output:os.Stdout,
 })
 if err!=nil{return fmt.Errorf("Live ISO guest verification: %w",err)}
 fmt.Printf("TEST ISO BOOT OK profile=%s verification_vm=%s\n",profile,instance.ID)
 return nil
}
