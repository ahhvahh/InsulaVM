// Package vm creates disposable Libvirt instances from cloud-init-ready QCOW2
// images. The attached NoCloud seed ISO provisions SSH and sudo but does NOT
// install an operating system from a bare installer ISO.
package vm

import (
 "context"
 "crypto/rand"
 "encoding/xml"
 "errors"
 "fmt"
 "net"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/cloudinit"
 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/executor"
 "github.com/ahhvahh/InsulaVM/pkg/identity"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
 "golang.org/x/crypto/ssh"
)

// Instance represents an ephemeral VM that is recorded in the shared SQLite
// registry. Caller MUST call Cleanup after a manually started instance.
type Instance struct {
 ID string
 LibvirtUUID string
 Name string
 Profile string
 MACAddress string
 IPAddress string
 Address string
 Directory string
 SSHUser string
 SSHPrivateKeyPath string
 SSHKnownHostsPath string
 SSHHostFingerprint string
 SudoMode string
 uri string
 registryPath string
 active bool
}

// Progress is a stage update for both the CLI and Go library consumers.
type Progress struct {
 Stage string
 Message string
 ID string
 Name string
 At time.Time
}

// Start preserves the original public Go signature.
func Start(ctx context.Context,c *config.Config,p *profile.Profile)(*Instance,error){
 return StartWithProgress(ctx,c,p,nil)
}

// StartWithProgress creates the VM, reports stages, and checks actual Libvirt
// state during boot. Cloud-Init VMs return after SSH/sudo readiness;
// direct-boot VMs return after Libvirt reports running (not verified guest boot).
func StartWithProgress(ctx context.Context,c *config.Config,p *profile.Profile,onProgress func(Progress))(*Instance,error){
 return StartWithStartupScript(ctx,c,p,nil,onProgress)
}

// StartWithStartupScript configures the guest during its initial Cloud-Init
// provisioning. It returns a ready VM only after the startup script succeeds.
// StartupOptions controls readiness and inspection of failed starts.
type StartupOptions struct {
 SSHReadyTimeout time.Duration
 PreserveOnFailure bool
}

func StartWithStartupScript(ctx context.Context,c *config.Config,p *profile.Profile,
 startupScript []byte,onProgress func(Progress))(*Instance,error){
 return StartWithStartupOptions(ctx,c,p,startupScript,StartupOptions{},onProgress)
}

// StartWithStartupOptions leaves a failed Libvirt domain and its overlay
// intact only when explicitly requested, for diagnostic purposes.
func StartWithStartupOptions(ctx context.Context,c *config.Config,p *profile.Profile,
 startupScript []byte,opts StartupOptions,onProgress func(Progress))(instance *Instance,runErr error){
 var progressID string
 emit:=func(stage,message string){
  if onProgress==nil{return}
  event:=Progress{Stage:stage,Message:message,ID:progressID,At:time.Now()}
  if p!=nil{event.Name=p.Name}
  if instance!=nil{event.ID=instance.ID}
  onProgress(event)
 }
 defer func(){
  if runErr!=nil{emit("failed","Falha na inicializacao: "+runErr.Error())}
 }()
 if ctx==nil||c==nil||p==nil{return nil,errors.New("context, config and profile are required")}
 directBoot:=p.BootMode=="direct"
 if directBoot&&len(startupScript)>0{return nil,errors.New("direct-boot profiles do not support Cloud-Init startup scripts")}
 emit("profile","Verificando imagem-base QCOW2 do perfil "+p.Name)
 image,err:=p.ImagePath()
 if err!=nil{
  if errors.Is(err,os.ErrPermission){
   return nil,fmt.Errorf("sem permissao para ler imagem-base do perfil %s: %w; verifique proprietario e ACL com stat/getfacl; pare VMs que utilizam a imagem antes de corrigir seu dono (chown)",p.Name,err)
  }
  return nil,fmt.Errorf("resolve profile image: %w",err)
 }
 emit("profile-ok","Imagem-base validada: "+image)
 for _,tool:=range []string{"qemu-img","virsh"}{
  if _,err:=exec.LookPath(tool);err!=nil{return nil,fmt.Errorf("missing VM host tool %s: %w",tool,err)}
 }
 if err:=os.MkdirAll(c.Storage.Runs,0700);err!=nil{return nil,err}
 dir,err:=os.MkdirTemp(c.Storage.Runs,"insula-vm-")
 if err!=nil{return nil,err}
 sudoMode:=c.SSH.SudoMode
 if directBoot{sudoMode="disabled"}
 instance=&Instance{Directory:dir,uri:c.Virtualization.LibvirtURI,registryPath:c.Storage.Database,Profile:p.Name,SudoMode:sudoMode}
 emit("preparing","Preparando armazenamento temporario isolado")
 // Keep a separate reference for cleanup. Start returns (nil, error) on
 // failure, which sets the named result "instance" to nil before deferred
 // functions execute. Using that named result would panic and hide the real
 // Libvirt/Cloud-Init error.
 created:=instance
 registered:=false
 success:=false
 defer func(){
  if success{return}
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
  defer cancel()
  preserved:=opts.PreserveOnFailure && created.active && registered
  if !preserved {
   emit("cleaning","Limpando recursos da inicializacao malsucedida")
   if cleanupErr:=created.Cleanup(cleanupCtx);cleanupErr!=nil{
    runErr=errors.Join(runErr,fmt.Errorf("failed to clean up failed VM: %w",cleanupErr))
   }
  }
  if registered{
   if store,err:=registry.Open(created.registryPath);err==nil{
    _=store.SetFailed(cleanupCtx,created.ID)
    _=store.Close()
   }
  }
  if preserved{
   emit("preserved",fmt.Sprintf("VM de diagnostico preservada: id=%s nome=%s pasta=%s; use 'insula test ssh -id %s' e 'insula vm stop -id %s' para liberar recursos",created.ID,created.Name,created.Directory,created.ID,created.ID))
  }
 }()
 // Check the persistent history before assigning a UUID, including IDs
 // from destroyed VMs. The subsequent SQLite INSERT is the atomic guard
 // against concurrent allocators using the same registry.
 id,err:=unusedVMID(ctx,c.Storage.Database,identity.UUIDv7)
 if err!=nil{return nil,fmt.Errorf("allocate unique VM ID: %w",err)}
 instance.ID=id
 progressID=id
 instance.LibvirtUUID=id
 if !directBoot {
  instance.SSHUser=c.SSH.User
  if p.SSH.User!=""{instance.SSHUser=p.SSH.User}
 }
 // Use the full UUIDv7: its first eight hex digits are timestamp-derived
 // and can be identical across many concurrent starts of the same profile.
 instance.Name=vmDomainName(p.Name,id)
 if !directBoot {
  instance.SSHPrivateKeyPath=filepath.Join(dir,"client_ed25519")
  instance.SSHKnownHostsPath=filepath.Join(dir,"known_hosts")
 }
 macBytes:=make([]byte,3)
 if _,err:=rand.Read(macBytes);err!=nil{return nil,err}
 instance.MACAddress=fmt.Sprintf("52:54:00:%02x:%02x:%02x",macBytes[0],macBytes[1],macBytes[2])

 var seedISO string
 var hostPublicKey ssh.PublicKey
 if !directBoot {
  clientKeys,err:=identity.GenerateKey()
  if err!=nil{return nil,err}
  if err:=identity.SavePrivateKey(instance.SSHPrivateKeyPath,clientKeys.PrivatePEM);err!=nil{return nil,err}
  hostKeys,err:=identity.GenerateKey()
  if err!=nil{return nil,err}
  hostPublicKey=hostKeys.Public
  instance.SSHHostFingerprint=identity.Fingerprint(hostKeys.Public)
  seed,err:=cloudinit.PrepareWithStartupScript(ctx,dir,id,instance.SSHUser,instance.SudoMode,clientKeys.PublicAuthorized,hostKeys,startupScript)
  if err!=nil{return nil,err}
  seedISO=seed.ISO
  emit("cloud-init","Chaves SSH e ISO NoCloud criadas; perfil de sudo: "+instance.SudoMode)
 }else{
  emit("boot-mode","Boot direto pelo QCOW2; Cloud-Init e gerenciamento SSH desativados")
 }

 overlay:=filepath.Join(dir,"overlay.qcow2")
 if out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2","-F","qcow2","-b",image,overlay).CombinedOutput();err!=nil{
  return nil,fmt.Errorf("create overlay: %w: %s",err,strings.TrimSpace(string(out)))
 }
 emit("disk","Overlay QCOW2 criado sem alterar o disco-base")
 xmlSpec:=domainXMLWithBacking(instance.LibvirtUUID,instance.Name,p.MemoryMiB,p.VCPUs,overlay,image,seedISO,c.Virtualization.Network,instance.MACAddress)
 xmlPath:=filepath.Join(dir,"domain.xml")
 if err:=os.WriteFile(xmlPath,[]byte(xmlSpec),0600);err!=nil{return nil,err}
 // With qemu:///system, Libvirt launches QEMU under a separate non-root
 // Linux account. Grant it access only to the backing disk, overlay and
 // cloud-init seed, without exposing SSH client keys or listing directories.
 if c.Virtualization.QEMUUID > 0 {
  if err:=grantQEMUStorageAccess(ctx,c.Virtualization.QEMUUID,image,overlay,seedISO);err!=nil{
   return nil,fmt.Errorf("prepare QEMU disk permissions: %w",err)
  }
 }
 emit("permissions","Permissoes do armazenamento preparadas para o QEMU")

 store,err:=registry.Open(instance.registryPath)
 if err!=nil{return nil,err}
 insertErr:=store.Insert(ctx,registry.Entry{
  ID:id,LibvirtUUID:id,Name:instance.Name,Profile:p.Name,MAC:instance.MACAddress,
  HostFingerprint:instance.SSHHostFingerprint,SudoMode:instance.SudoMode,
  SSHUser:instance.SSHUser,SSHPort:22,PrivateKeyPath:instance.SSHPrivateKeyPath,
  KnownHostsPath:instance.SSHKnownHostsPath,InstanceDir:dir,
 })
 _=store.Close()
 if insertErr!=nil{return nil,fmt.Errorf("register VM: %w",insertErr)}
 registered=true
 emit("registered","Cadastro SQLite criado: "+id)
 emit("booting","Criando dominio KVM/Libvirt: "+instance.Name)

 if out,err:=virsh(ctx,instance.uri,"create",xmlPath);err!=nil{
  return nil,fmt.Errorf("start disposable domain: %w: %s",err,strings.TrimSpace(string(out)))
 }
 instance.active=true
 if state,err:=ActiveDomainState(ctx,instance.uri,instance.LibvirtUUID);err!=nil{
  return nil,fmt.Errorf("VM created but did not enter running state: %w",err)
 }else{
  emit("running","Dominio ativo no Libvirt: "+state)
 }
 if directBoot {
  store,err=registry.Open(instance.registryPath)
  if err!=nil{return nil,err}
  updateErr:=store.SetRunning(ctx,id,"")
  _=store.Close()
  if updateErr!=nil{return nil,fmt.Errorf("persist direct-boot VM: %w",updateErr)}
  emit("ready","Dominio ativo no Libvirt; boot do sistema convidado e QGA ainda nao verificados")
  success=true
  return instance,nil
 }
 emit("dhcp","Aguardando endereco IP via DHCP da rede Libvirt")
 dhcpCtx,stopDHCP:=context.WithTimeout(ctx,3*time.Minute)
 ticker:=time.NewTicker(2*time.Second)
 defer ticker.Stop()
 for {
  if err:=dhcpCtx.Err();err!=nil{
   stopDHCP()
   return nil,fmt.Errorf("tempo limite de DHCP (3m): %w",err)
  }
  if _,err:=ActiveDomainState(dhcpCtx,instance.uri,instance.LibvirtUUID);err!=nil{
   if dhcpCtx.Err()!=nil{
    stopDHCP()
    return nil,fmt.Errorf("tempo limite de DHCP (3m): %w",dhcpCtx.Err())
   }
   stopDHCP()
   return nil,fmt.Errorf("verificar estado Libvirt durante DHCP: %w",err)
  }
  out,err:=virsh(dhcpCtx,instance.uri,"domifaddr",instance.Name,"--source","lease")
  if err==nil{
   if ip:=parseIPv4(string(out));ip!=""{
    instance.IPAddress=ip
    instance.Address=net.JoinHostPort(ip,"22")
    emit("ip","IP obtido: "+ip)
    if err:=identity.SaveKnownHost(instance.SSHKnownHostsPath,ip,hostPublicKey);err!=nil{
     stopDHCP()
     return nil,err
    }
    break
   }
  }
  select{
  case <-dhcpCtx.Done():
   stopDHCP()
   return nil,fmt.Errorf("tempo limite aguardando DHCP (3m): %w",dhcpCtx.Err())
  case <-ticker.C:
  }
 }
 stopDHCP()
 sshTimeout:=opts.SSHReadyTimeout
 if sshTimeout==0{sshTimeout=5*time.Minute}
 if sshTimeout<0 || sshTimeout>time.Hour{return nil,errors.New("SSHReadyTimeout must be 0..1h")}
 sshCtx,stopSSH:=context.WithTimeout(ctx,sshTimeout)
 defer stopSSH()
 emit("ssh","Aguardando SSH autenticado e validando chave do servidor")
 var lastErr error
 attempt:=0
 for {
  if err:=sshCtx.Err();err!=nil{
   return nil,fmt.Errorf("tempo limite aguardando SSH (%s): %w; ultima tentativa: %v",sshTimeout,err,lastErr)
  }
  if _,err:=ActiveDomainState(sshCtx,instance.uri,instance.LibvirtUUID);err!=nil{
   if sshCtx.Err()!=nil{
    return nil,fmt.Errorf("tempo limite aguardando SSH (%s): %w; ultima tentativa: %v",sshTimeout,sshCtx.Err(),lastErr)
   }
   return nil,fmt.Errorf("verificar estado Libvirt durante SSH: %w",err)
  }
  attempt++
  client,err:=executor.DialContext(sshCtx,executor.DialConfig{
   Address:instance.Address,User:instance.SSHUser,
   PrivateKeyPath:instance.SSHPrivateKeyPath,
   KnownHostsPath:instance.SSHKnownHostsPath,Timeout:10*time.Second,
   HostKeyAlgorithms:[]string{"ssh-ed25519"},
  })
  if err==nil{
   if instance.SudoMode=="passwordless"{
    emit("sudo","Conexao SSH aceita; verificando sudo -n sem senha")
    session,sessionErr:=client.NewSession()
    if sessionErr==nil{
     cancelSession:=context.AfterFunc(sshCtx,func(){_ = session.Close()})
     sessionErr=session.Run("sudo -n true")
     cancelSession()
     _=session.Close()
    }
    err=sessionErr
   }
   _=client.Close()
   if err==nil{break}
  }
  lastErr=err
  if attempt==1 || attempt%5==0{
   emit("ssh-retry",fmt.Sprintf("Tentativa SSH %d: %v",attempt,err))
  }
  select{
  case <-sshCtx.Done():
   return nil,fmt.Errorf("tempo limite aguardando SSH (%s): %w; ultima tentativa: %v",sshTimeout,sshCtx.Err(),lastErr)
  case <-ticker.C:
  }
 }
 store,err=registry.Open(instance.registryPath)
 if err!=nil{return nil,err}
 updateErr:=store.SetRunning(ctx,id,instance.IPAddress)
 _=store.Close()
 if updateErr!=nil{return nil,fmt.Errorf("persist started VM: %w",updateErr)}
 if len(startupScript)>0{
  emit("startup","Aguardando Cloud-Init concluir o script de instalacao da instancia")
  if err:=waitStartupScript(ctx,instance);err!=nil{return nil,fmt.Errorf("VM startup script failed: %w",err)}
  emit("startup-ok","Script de instalacao concluido; VM pronta para receber o pacote")
 }
 emit("ready","VM pronta: SSH validado; IP "+instance.IPAddress)
 success=true
 return instance,nil
}

// Cloud-Init runs the startup script as root in the first-boot final phase.
// Merely accepting SSH is not sufficient evidence that provisioning finished.
// A per-instance success sentinel is emitted ONLY after the script succeeds.
func waitStartupScript(ctx context.Context,instance *Instance)error{
 readyCtx,cancel:=context.WithTimeout(ctx,20*time.Minute)
 defer cancel()
 client,err:=executor.Dial(executor.DialConfig{
  Address:instance.Address,User:instance.SSHUser,
  PrivateKeyPath:instance.SSHPrivateKeyPath,
  KnownHostsPath:instance.SSHKnownHostsPath,Timeout:15*time.Second,
  HostKeyAlgorithms:[]string{"ssh-ed25519"},
 })
 if err!=nil{return err}
 defer client.Close()
 session,err:=client.NewSession()
 if err!=nil{return err}
 defer session.Close()
 done:=make(chan error,1)
 go func(){
  output,err:=session.CombinedOutput("cloud-init status --wait --long && test -f /var/lib/insula/startup-success")
  if err!=nil{done<-fmt.Errorf("cloud-init initialization did not confirm success: %w: %s",err,strings.TrimSpace(string(output)));return}
  done<-nil
 }()
 select{
 case err:=<-done:return err
 case <-readyCtx.Done():
  _=client.Close()
  <-done
  return fmt.Errorf("cloud-init startup deadline exceeded: %w",readyCtx.Err())
 }
}

func virsh(ctx context.Context,uri string,args ...string)([]byte,error){
 cmd:=exec.CommandContext(ctx,"virsh",append([]string{"-c",uri},args...)...)
 // Libvirt localizes domstate and error text. Stable machine-readable state
 // comparisons require an English locale even when the operator uses pt_BR.
 cmd.Env=append(os.Environ(),"LANG=C","LC_ALL=C")
 return cmd.CombinedOutput()
}

func parseIPv4(output string)string{
 for _,line:=range strings.Split(output,"\n"){
  for _,field:=range strings.Fields(line){
   ipText,_,ok:=strings.Cut(field,"/")
   if !ok{continue}
   ip:=net.ParseIP(ipText)
   if ip!=nil && ip.To4()!=nil && !ip.IsLoopback(){return ip.String()}
  }
 }
 return ""
}

// Cleanup destroys the Libvirt domain and deletes transient disk/seed/keys.
// History is retained in SQLite, but key contents are not.
func(i *Instance)Cleanup(ctx context.Context)error{
 if i==nil||i.Directory==""{return errors.New("missing instance or directory")}
 if ctx==nil{return errors.New("context is required")}
 if i.active{
  if out,err:=virsh(ctx,i.uri,"destroy",i.Name);err!=nil{
   // A transient VM may have already vanished before cleanup. Check its
   // Libvirt UUID rather than relying on localized error text.
   infos,checkErr:=InspectRegistered(ctx,i.uri,[]registry.Entry{{LibvirtUUID:i.LibvirtUUID,Status:"running"}})
   if checkErr!=nil{return fmt.Errorf("cannot verify domain stopped; refusing to remove disk: %w; destroy: %v: %s",checkErr,err,strings.TrimSpace(string(out)))}
   if len(infos)!=1||infos[0].LibvirtState!="absent"{
    return fmt.Errorf("cannot confirm domain stopped; refusing to remove disk: destroy: %w: %s; actual Libvirt state: %s",err,strings.TrimSpace(string(out)),infos[0].LibvirtState)
   }
  }
  i.active=false
 }
 if err:=os.RemoveAll(i.Directory);err!=nil{return err}
 if i.ID!=""&&i.registryPath!=""{
  store,err:=registry.Open(i.registryPath)
  if err!=nil{return err}
  defer store.Close()
  // Failed starts without an inserted row are allowed to clean temp files.
  if err:=store.SetDestroyed(ctx,i.ID);err!=nil&&!strings.Contains(err.Error(),"not found"){return err}
 }
 return nil
}

// StopByID stops a VM started by a prior CLI/library invocation. Its UUID and
// directory must match the registry and storage root before removal.
func StopByID(ctx context.Context,c *config.Config,id string)error{
 if ctx==nil||c==nil{return errors.New("context and config are required")}
 store,err:=registry.Open(c.Storage.Database)
 if err!=nil{return err}
 entry,err:=store.Get(ctx,id)
 _=store.Close()
 if err!=nil{return err}
 // Query the actual hypervisor state before deleting anything. A stale
 // SQLite record is NOT evidence of a running VM; a Libvirt failure is NOT
 // evidence of an absent VM.
 live,err:=InspectRegistered(ctx,c.Virtualization.LibvirtURI,[]registry.Entry{entry})
 if err!=nil{return err}
 if live[0].LibvirtState=="unknown"{
  return fmt.Errorf("cannot safely stop VM %s: Libvirt state unknown: %s",id,live[0].Warning)
 }
 if entry.Status=="destroyed"{
  if live[0].LibvirtState=="absent"{return nil}
  return fmt.Errorf("VM %s is marked destroyed in SQLite but still exists in Libvirt (%s); refusing to silently ignore it",id,live[0].LibvirtState)
 }
 if entry.Status!="running"&&entry.Status!="creating"&&entry.Status!="failed"{
  return fmt.Errorf("unsupported VM status %q",entry.Status)
 }
 path,err:=managedInstanceDirectory(c,entry)
 if err!=nil{return err}
 instance:=&Instance{
  ID:id,LibvirtUUID:entry.LibvirtUUID,Name:entry.Name,Directory:path,uri:c.Virtualization.LibvirtURI,
  registryPath:c.Storage.Database,active:live[0].LibvirtState!="absent",
 }
 return instance.Cleanup(ctx)
}

// domainXML preserves the original helper for existing unit tests and
// existing Linux profiles; new VMs use domainXMLWithSeed.
func domainXML(name string,memoryMiB,vcpus int,disk,network,mac string)string{
 return domainXMLWithSeed("",name,memoryMiB,vcpus,disk,"",network,mac)
}

func domainXMLWithSeed(uuid,name string,memoryMiB,vcpus int,disk,seedISO,network,mac string)string{
 return domainXMLWithBacking(uuid,name,memoryMiB,vcpus,disk,"",seedISO,network,mac)
}

// domainXMLWithBacking declares the immutable base as a read-only backing
// image with DAC relabeling disabled. Libvirt otherwise may chown the shared
// base image to the QEMU UID and not restore it when the VM stops. The host
// has already granted the QEMU UID read-only permission via a scoped ACL.
func domainXMLWithBacking(uuid,name string,memoryMiB,vcpus int,disk,baseImage,seedISO,network,mac string)string{
 escape:=func(v string)string{
  var b strings.Builder
  _=xml.EscapeText(&b,[]byte(v))
  return strings.ReplaceAll(b.String(),"'","&apos;")
 }
 var uuidElement string
 if uuid!=""{uuidElement="<uuid>"+escape(uuid)+"</uuid>"}
 var backingStore string
 if baseImage!=""{
  backingStore="<backingStore type='file'><format type='qcow2'/>"+
   "<source file='"+escape(baseImage)+"'>"+
   "<seclabel model='dac' relabel='no'/></source><backingStore/></backingStore>"
 }
 var seedDisk string
 if seedISO!=""{
  seedDisk="<disk type='file' device='cdrom'><driver name='qemu' type='raw'/>"+
   "<source file='"+escape(seedISO)+"'/><target dev='sda' bus='sata'/><readonly/></disk>"
 }
 return fmt.Sprintf(
  "<domain type='kvm'><name>%s</name>%s<memory unit='MiB'>%d</memory>"+
   "<vcpu>%d</vcpu><os><type arch='x86_64'>hvm</type></os><features><acpi/></features>"+
   "<devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/>"+
   "<source file='%s'/>%s<target dev='vda' bus='virtio'/></disk>%s"+
   "<interface type='network'><mac address='%s'/><source network='%s'/>"+
   "<model type='virtio'/></interface>"+
   "<channel type='unix'><source mode='bind'/><target type='virtio' name='org.qemu.guest_agent.0'/></channel>"+
   "<serial type='pty'><target port='0'/></serial>"+
   "<console type='pty'><target type='serial' port='0'/></console>"+
   "</devices></domain>",
  escape(name),uuidElement,memoryMiB,vcpus,escape(disk),backingStore,seedDisk,escape(mac),escape(network))
}
