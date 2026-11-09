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
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// LiveISOStartupOptions controls a Live ISO boot's readiness and diagnostics.
type LiveISOStartupOptions struct {
 ReadinessTimeout time.Duration
 PreserveOnFailure bool
}

// StartLiveISO keeps the original Go API. Existing integrations are unaffected.
func StartLiveISO(ctx context.Context,c *config.Config,profileName,isoPath string,memoryMiB,vcpus int,startupScript []byte)(*Instance,error){
 return StartLiveISOWithOptions(ctx,c,profileName,isoPath,memoryMiB,vcpus,startupScript,LiveISOStartupOptions{})
}

// StartLiveISOWithOptions boots a Debian Live ISO with a NoCloud seed and
// isolated scratch disk. When requested, a failed boot is retained so its
// serial console and Libvirt state can be diagnosed without losing evidence.
func StartLiveISOWithOptions(ctx context.Context,c *config.Config,profileName,isoPath string,memoryMiB,vcpus int,startupScript []byte,opts LiveISOStartupOptions)(instance *Instance,runErr error){
 if ctx==nil||c==nil{return nil,errors.New("context and configuration required")}
 if opts.ReadinessTimeout<0{return nil,errors.New("Live ISO readiness timeout must not be negative")}
 if opts.ReadinessTimeout==0{opts.ReadinessTimeout=5*time.Minute}
 if err:=ctx.Err();err!=nil{return nil,err}
 if memoryMiB<128||vcpus<1{return nil,errors.New("invalid memory or vcpus")}
 stat,err:=os.Stat(isoPath)
 if err!=nil{return nil,err}
 if !stat.Mode().IsRegular()||stat.Size()==0{return nil,errors.New("ISO must be a nonempty regular file")}
 absISO,err:=filepath.Abs(isoPath)
 if err!=nil{return nil,err}
 for _,tool:=range []string{"virsh","qemu-img"}{if _,err:=exec.LookPath(tool);err!=nil{return nil,err}}
 if err:=os.MkdirAll(c.Storage.Runs,0700);err!=nil{return nil,err}
 dir,err:=os.MkdirTemp(c.Storage.Runs,"insula-vm-")
 if err!=nil{return nil,err}
 instance=&Instance{Directory:dir,uri:c.Virtualization.LibvirtURI,registryPath:c.Storage.Database,Profile:profileName,SudoMode:c.SSH.SudoMode,SSHUser:c.SSH.User}
 created:=instance
 success,registered:=false,false
 defer func(){
  if success{return}
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
  defer cancel()
  preserved:=opts.PreserveOnFailure&&registered&&created.active
  if !preserved{
   if cleanErr:=created.Cleanup(cleanupCtx);cleanErr!=nil{
    runErr=errors.Join(runErr,fmt.Errorf("cleanup failed Live ISO VM: %w",cleanErr))
   }
  }
  if registered{
   if store,err:=registry.Open(created.registryPath);err==nil{
    _=store.SetFailed(cleanupCtx,created.ID)
    _=store.Close()
   }
  }
  if preserved{
   runErr=errors.Join(runErr,fmt.Errorf("diagnostic Live ISO VM preserved: id=%s directory=%s; inspect with 'insula vm status -id %s' and 'insula vm console -id %s'; remove with 'insula vm stop -id %s'",created.ID,created.Directory,created.ID,created.ID,created.ID))
  }
 }()
 id,err:=identity.UUIDv7()
 if err!=nil{return nil,err}
 instance.ID=id
 instance.LibvirtUUID=id
 instance.Name="insula-"+profileName+"-"+id[:8]
 instance.SSHPrivateKeyPath=filepath.Join(dir,"client_ed25519")
 instance.SSHKnownHostsPath=filepath.Join(dir,"known_hosts")
 mac:=make([]byte,3)
 if _,err:=rand.Read(mac);err!=nil{return nil,err}
 instance.MACAddress=fmt.Sprintf("52:54:00:%02x:%02x:%02x",mac[0],mac[1],mac[2])
 keys,err:=identity.GenerateKey()
 if err!=nil{return nil,err}
 if err:=identity.SavePrivateKey(instance.SSHPrivateKeyPath,keys.PrivatePEM);err!=nil{return nil,err}
 hostKeys,err:=identity.GenerateKey()
 if err!=nil{return nil,err}
 instance.SSHHostFingerprint=identity.Fingerprint(hostKeys.Public)
 seed,err:=cloudinit.PrepareWithStartupScript(ctx,dir,id,instance.SSHUser,instance.SudoMode,keys.PublicAuthorized,hostKeys,startupScript)
 if err!=nil{return nil,err}
 disk:=filepath.Join(dir,"scratch.qcow2")
 if out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2",disk,"1G").CombinedOutput();err!=nil{
  return nil,fmt.Errorf("create ISO scratch disk: %w: %s",err,strings.TrimSpace(string(out)))
 }
 xmlSpec:=liveISODomainXML(id,instance.Name,memoryMiB,vcpus,absISO,seed.ISO,disk,c.Virtualization.Network,instance.MACAddress)
 xmlFile:=filepath.Join(dir,"domain.xml")
 if err:=os.WriteFile(xmlFile,[]byte(xmlSpec),0600);err!=nil{return nil,err}
 if c.Virtualization.QEMUUID>0{
  if err:=grantQEMUStorageAccess(ctx,c.Virtualization.QEMUUID,absISO,disk,seed.ISO);err!=nil{return nil,err}
 }
 store,err:=registry.Open(c.Storage.Database)
 if err!=nil{return nil,err}
 insertErr:=store.Insert(ctx,registry.Entry{
  ID:id,LibvirtUUID:id,Name:instance.Name,Profile:profileName,MAC:instance.MACAddress,
  HostFingerprint:instance.SSHHostFingerprint,SudoMode:instance.SudoMode,
  SSHUser:instance.SSHUser,SSHPort:22,PrivateKeyPath:instance.SSHPrivateKeyPath,
  KnownHostsPath:instance.SSHKnownHostsPath,InstanceDir:dir,
 })
 _=store.Close()
 if insertErr!=nil{return nil,insertErr}
 registered=true
 if out,err:=virsh(ctx,instance.uri,"create",xmlFile);err!=nil{return nil,fmt.Errorf("boot Live ISO: %w: %s",err,strings.TrimSpace(string(out)))}
 instance.active=true
 bootCtx,cancel:=context.WithTimeout(ctx,opts.ReadinessTimeout)
 defer cancel()
 ticker:=time.NewTicker(2*time.Second)
 defer ticker.Stop()
 for {
  if bootCtx.Err()!=nil{return nil,liveISOReadinessError("DHCP",opts.ReadinessTimeout,bootCtx.Err())}
  if _,err:=ActiveDomainState(bootCtx,instance.uri,id);err!=nil{
   if bootCtx.Err()!=nil{return nil,liveISOReadinessError("DHCP",opts.ReadinessTimeout,bootCtx.Err())}
   return nil,fmt.Errorf("Live ISO state check before DHCP failed: %w",err)
  }
  out,err:=virsh(bootCtx,instance.uri,"domifaddr",instance.Name,"--source","lease")
  if err==nil{
   if ip:=parseIPv4(string(out));ip!=""{
    instance.IPAddress=ip
    instance.Address=net.JoinHostPort(ip,"22")
    if err:=identity.SaveKnownHost(instance.SSHKnownHostsPath,ip,hostKeys.Public);err!=nil{return nil,err}
    break
   }
  }
  select{
  case <-bootCtx.Done():return nil,liveISOReadinessError("DHCP",opts.ReadinessTimeout,bootCtx.Err())
  case <-ticker.C:
  }
 }
 var lastErr error
 for {
  if bootCtx.Err()!=nil{return nil,liveISOReadinessError("SSH",opts.ReadinessTimeout,bootCtx.Err())}
  if _,err:=ActiveDomainState(bootCtx,instance.uri,id);err!=nil{
   if bootCtx.Err()!=nil{return nil,liveISOReadinessError("SSH",opts.ReadinessTimeout,bootCtx.Err())}
   return nil,fmt.Errorf("Live ISO state check before SSH failed: %w",err)
  }
  client,err:=executor.Dial(executor.DialConfig{
   Address:instance.Address,User:instance.SSHUser,
   PrivateKeyPath:instance.SSHPrivateKeyPath,KnownHostsPath:instance.SSHKnownHostsPath,Timeout:8*time.Second,
  HostKeyAlgorithms:[]string{"ssh-ed25519"},
  })
  if err==nil{
   if instance.SudoMode=="passwordless"{
    session,e:=client.NewSession()
    if e==nil{e=session.Run("sudo -n true");_ = session.Close()}
    err=e
   }
   _=client.Close()
   if err==nil{break}
  }
  lastErr=err
  select{
  case <-bootCtx.Done():return nil,fmt.Errorf("%w; last SSH error: %v",liveISOReadinessError("SSH",opts.ReadinessTimeout,bootCtx.Err()),lastErr)
  case <-ticker.C:
  }
 }
 store,err=registry.Open(c.Storage.Database)
 if err!=nil{return nil,err}
 updateErr:=store.SetRunning(ctx,id,instance.IPAddress)
 _=store.Close()
 if updateErr!=nil{return nil,updateErr}
 if len(startupScript)>0{
  if err:=waitStartupScript(ctx,instance);err!=nil{return nil,err}
 }
 success=true
 return instance,nil
}

func liveISOReadinessError(stage string,timeout time.Duration,err error)error{
 if errors.Is(err,context.DeadlineExceeded){
  return fmt.Errorf("Live ISO did not reach %s readiness within %s (the VM may still be booting, without DHCP or SSH; use -preserve-on-failure to inspect its console): %w",stage,timeout,err)
 }
 return fmt.Errorf("Live ISO %s readiness interrupted: %w",stage,err)
}

func liveISODomainXML(uuid,name string,memory,vcpus int,iso,seed,scratch,network,mac string)string{
 esc:=func(value string)string{
  var b strings.Builder
  _=xml.EscapeText(&b,[]byte(value))
  return strings.ReplaceAll(b.String(),"'","&apos;")
 }
 cdrom:=func(source,target string)string{
  return "<disk type='file' device='cdrom'><driver name='qemu' type='raw'/><source file='"+esc(source)+"'/><target dev='"+target+"' bus='sata'/><readonly/></disk>"
 }
 return fmt.Sprintf("<domain type='kvm'><name>%s</name><uuid>%s</uuid><memory unit='MiB'>%d</memory><vcpu>%d</vcpu>"+
  "<os><type arch='x86_64'>hvm</type><boot dev='cdrom'/></os><features><acpi/></features><devices>"+
  "%s%s<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='%s'/><target dev='vda' bus='virtio'/></disk>"+
  "<interface type='network'><mac address='%s'/><source network='%s'/><model type='virtio'/></interface>"+
  "<channel type='unix'><source mode='bind'/><target type='virtio' name='org.qemu.guest_agent.0'/></channel>"+
  "<serial type='pty'><target port='0'/></serial><console type='pty'><target type='serial' port='0'/></console>"+
  "</devices></domain>",esc(name),esc(uuid),memory,vcpus,cdrom(iso,"sda"),cdrom(seed,"sdb"),esc(scratch),esc(mac),esc(network))
}
