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
// state during boot. A VM is returned only after SSH (and sudo, if enabled)
// is ready and the SQLite record was updated.
func StartWithProgress(ctx context.Context,c *config.Config,p *profile.Profile,onProgress func(Progress))(instance *Instance,runErr error){
 emit:=func(stage,message string){
  if onProgress==nil{return}
  event:=Progress{Stage:stage,Message:message,At:time.Now()}
  if p!=nil{event.Name=p.Name}
  if instance!=nil{event.ID=instance.ID}
  onProgress(event)
 }
 if ctx==nil||c==nil||p==nil{return nil,errors.New("context, config and profile are required")}
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
 instance=&Instance{Directory:dir,uri:c.Virtualization.LibvirtURI,registryPath:c.Storage.Database,Profile:p.Name,SudoMode:c.SSH.SudoMode}
 emit("preparing","Preparando armazenamento temporario isolado e credenciais SSH")
 // Keep a separate reference for cleanup. Start returns (nil, error) on
 // failure, which sets the named result "instance" to nil before deferred
 // functions execute. Using that named result would panic and hide the real
 // Libvirt/Cloud-Init error.
 created:=instance
 registered:=false
 success:=false
 defer func(){
  if success{return}
  if runErr!=nil{emit("failed","Falha: "+runErr.Error())}
  emit("cleaning","Limpando recursos da inicializacao malsucedida")
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
  defer cancel()
  if cleanupErr:=created.Cleanup(cleanupCtx);cleanupErr!=nil{
   runErr=errors.Join(runErr,fmt.Errorf("failed to clean up failed VM: %w",cleanupErr))
  }
  if registered{
   if store,err:=registry.Open(created.registryPath);err==nil{
    _=store.SetFailed(cleanupCtx,created.ID)
    _=store.Close()
   }
  }
 }()
 id,err:=identity.UUIDv7()
 if err!=nil{return nil,err}
 instance.ID=id
 instance.LibvirtUUID=id
 instance.SSHUser=c.SSH.User
 if p.SSH.User!=""{instance.SSHUser=p.SSH.User}
 instance.Name="insula-"+p.Name+"-"+id[:8]
 instance.SSHPrivateKeyPath=filepath.Join(dir,"client_ed25519")
 instance.SSHKnownHostsPath=filepath.Join(dir,"known_hosts")
 macBytes:=make([]byte,3)
 if _,err:=rand.Read(macBytes);err!=nil{return nil,err}
 instance.MACAddress=fmt.Sprintf("52:54:00:%02x:%02x:%02x",macBytes[0],macBytes[1],macBytes[2])

 clientKeys,err:=identity.GenerateKey()
 if err!=nil{return nil,err}
 if err:=identity.SavePrivateKey(instance.SSHPrivateKeyPath,clientKeys.PrivatePEM);err!=nil{return nil,err}
 hostKeys,err:=identity.GenerateKey()
 if err!=nil{return nil,err}
 instance.SSHHostFingerprint=identity.Fingerprint(hostKeys.Public)
 seed,err:=cloudinit.Prepare(ctx,dir,id,instance.SSHUser,instance.SudoMode,clientKeys.PublicAuthorized,hostKeys)
 if err!=nil{return nil,err}
 emit("cloud-init","Chaves SSH e ISO NoCloud criadas; perfil de sudo: "+instance.SudoMode)

 overlay:=filepath.Join(dir,"overlay.qcow2")
 if out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2","-F","qcow2","-b",image,overlay).CombinedOutput();err!=nil{
  return nil,fmt.Errorf("create overlay: %w: %s",err,strings.TrimSpace(string(out)))
 }
 emit("disk","Overlay QCOW2 criado sem alterar o disco-base")
 xmlSpec:=domainXMLWithBacking(instance.LibvirtUUID,instance.Name,p.MemoryMiB,p.VCPUs,overlay,image,seed.ISO,c.Virtualization.Network,instance.MACAddress)
 xmlPath:=filepath.Join(dir,"domain.xml")
 if err:=os.WriteFile(xmlPath,[]byte(xmlSpec),0600);err!=nil{return nil,err}
 // With qemu:///system, Libvirt launches QEMU under a separate non-root
 // Linux account. Grant it access only to the backing disk, overlay and
 // cloud-init seed, without exposing SSH client keys or listing directories.
 if c.Virtualization.QEMUUID > 0 {
  if err:=grantQEMUStorageAccess(ctx,c.Virtualization.QEMUUID,image,overlay,seed.ISO);err!=nil{
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
 emit("dhcp","Aguardando endereco IP via DHCP da rede Libvirt")
 bootCtx,cancel:=context.WithTimeout(ctx,3*time.Minute)
 defer cancel()
 ticker:=time.NewTicker(2*time.Second)
 defer ticker.Stop()
 for {
  if _,err:=ActiveDomainState(bootCtx,instance.uri,instance.LibvirtUUID);err!=nil{
   return nil,fmt.Errorf("VM parou enquanto aguardava DHCP: %w",err)
  }
  out,err:=virsh(bootCtx,instance.uri,"domifaddr",instance.Name,"--source","lease")
  if err==nil{
   if ip:=parseIPv4(string(out));ip!=""{
    instance.IPAddress=ip
    instance.Address=net.JoinHostPort(ip,"22")
    emit("ip","IP obtido: "+ip)
    if err:=identity.SaveKnownHost(instance.SSHKnownHostsPath,ip,hostKeys.Public);err!=nil{return nil,err}
    break
   }
  }
  select{
  case <-bootCtx.Done():return nil,fmt.Errorf("waiting for VM DHCP lease: %w",bootCtx.Err())
  case <-ticker.C:
  }
 }
 emit("ssh","Aguardando SSH autenticado e validando chave do servidor")
 // Verify the per-instance host key through known_hosts. The SSH login key
 // is generated for this VM, not shared with other instances.
 var lastErr error
 for {
  if _,err:=ActiveDomainState(bootCtx,instance.uri,instance.LibvirtUUID);err!=nil{
   return nil,fmt.Errorf("VM parou enquanto aguardava SSH: %w",err)
  }
  if err:=bootCtx.Err();err!=nil{
   return nil,fmt.Errorf("waiting for guest SSH/Cloud-Init: %w; last check: %v",err,lastErr)
  }
  client,err:=executor.Dial(executor.DialConfig{
   Address:instance.Address,User:instance.SSHUser,
   PrivateKeyPath:instance.SSHPrivateKeyPath,
   KnownHostsPath:instance.SSHKnownHostsPath,Timeout:8*time.Second,
  })
  if err==nil{
   if instance.SudoMode=="passwordless"{
    emit("sudo","Conexao SSH aceita; verificando sudo -n sem senha")
    session,sessionErr:=client.NewSession()
    if sessionErr==nil{
     sessionErr=session.Run("sudo -n true")
     _=session.Close()
    }
    err=sessionErr
   }
   _=client.Close()
   if err==nil{break}
  }
  lastErr=err
  select{
  case <-bootCtx.Done():return nil,fmt.Errorf("guest SSH or sudo readiness: %w; last check: %v",bootCtx.Err(),lastErr)
  case <-ticker.C:
  }
 }
 store,err=registry.Open(instance.registryPath)
 if err!=nil{return nil,err}
 updateErr:=store.SetRunning(ctx,id,instance.IPAddress)
 _=store.Close()
 if updateErr!=nil{return nil,fmt.Errorf("persist started VM: %w",updateErr)}
 emit("ready","VM pronta: SSH validado; IP "+instance.IPAddress)
 success=true
 return instance,nil
}

func virsh(ctx context.Context,uri string,args ...string)([]byte,error){
 cmd:=exec.CommandContext(ctx,"virsh",append([]string{"-c",uri},args...)...)
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
 root,err:=filepath.Abs(c.Storage.Runs)
 if err!=nil{return err}
 path,err:=filepath.Abs(entry.InstanceDir)
 if err!=nil{return err}
 relative,err:=filepath.Rel(root,path)
 if err!=nil||relative=="."||relative==".."||strings.HasPrefix(relative,".."+string(filepath.Separator))||!strings.HasPrefix(filepath.Base(path),"insula-vm-"){
  return errors.New("unsafe registered VM directory")
 }
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
   "<vcpu>%d</vcpu><os><type arch='x86_64'>hvm</type></os>"+
   "<devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/>"+
   "<source file='%s'/>%s<target dev='vda' bus='virtio'/></disk>%s"+
   "<interface type='network'><mac address='%s'/><source network='%s'/>"+
   "<model type='virtio'/></interface>"+
   "<serial type='pty'><target port='0'/></serial>"+
   "<console type='pty'><target type='serial' port='0'/></console>"+
   "</devices></domain>",
  escape(name),uuidElement,memoryMiB,vcpus,escape(disk),backingStore,seedDisk,escape(mac),escape(network))
}
