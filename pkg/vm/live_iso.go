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
 // CaptureSerial records guest ttyS0 output in a private file via Libvirt.
 // Disabled by default because host permissions/virtlogd vary.
 CaptureSerial bool
 // OnProgress receives host-side diagnostic stages without sensitive seed data.
 OnProgress func(stage,message string)
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
  // Preserve forensic evidence in storage.logs, regardless of cleanup policy.
  // Use a fresh bounded context because the DHCP/SSH context may have expired.
  if created.ID!=""{
   forensicCtx,done:=context.WithTimeout(context.Background(),35*time.Second)
   evidence,forensicErr:=CaptureLiveISODiagnostics(forensicCtx,c.Storage.Logs,c.Virtualization.LibvirtURI,created.ID,c.Virtualization.Network,created.MACAddress)
   done()
   if forensicErr!=nil{runErr=errors.Join(runErr,fmt.Errorf("capture VM diagnosis: %w",forensicErr))}
   if evidence!=""{runErr=errors.Join(runErr,fmt.Errorf("VM diagnostic snapshot: %s",evidence))}
  }
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
   runErr=errors.Join(runErr,fmt.Errorf("diagnostic Live ISO VM preserved: id=%s; run 'insula vm diagnose -id %s' or 'insula vm console -id %s'",created.ID,created.ID,created.ID))
  }
  // The events file survives even if the disposable VM has been removed.
  if created.ID!=""&&runErr!=nil{runErr=errors.Join(runErr,fmt.Errorf("host events: %s",filepath.Join(c.Storage.Logs,"live-iso",created.ID,"events.log")))}
 }()
 id,err:=identity.UUIDv7()
 if err!=nil{return nil,err}
 instance.ID=id
 instance.LibvirtUUID=id
 instance.Name="insula-"+profileName+"-"+id[:8]
 log,logErr:=OpenLiveISOLog(c.Storage.Logs,id)
 if logErr!=nil{return nil,logErr}
 defer log.Close()
 emit:=func(stage,message string){
  log.Event(stage,message)
  if opts.OnProgress!=nil{opts.OnProgress(stage,message)}
 }
 emit("preparing",fmt.Sprintf("uuid=%s profile=%s iso=%s memory_mib=%d vcpus=%d network=%s diagnostics=%s",id,profileName,absISO,memoryMiB,vcpus,c.Virtualization.Network,log.Dir))
 // Report errors before closing the event file; the outer cleanup defer
 // will then capture Libvirt evidence without erasing this error history.
 defer func(){
  if runErr!=nil{emit("failed",runErr.Error())}
 }()
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
 seed,err:=cloudinit.PrepareWithStartupScriptAndNetwork(ctx,dir,id,instance.SSHUser,instance.SudoMode,keys.PublicAuthorized,hostKeys,startupScript,instance.MACAddress)
 if err!=nil{return nil,err}
 emit("seed","NoCloud seed generated with explicit MAC-matched DHCPv4")
 disk:=filepath.Join(dir,"scratch.qcow2")
 if out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2",disk,"1G").CombinedOutput();err!=nil{
  return nil,fmt.Errorf("create ISO scratch disk: %w: %s",err,strings.TrimSpace(string(out)))
 }
 serialLog:=""
 if opts.CaptureSerial{
  serialLog=filepath.Join(log.Dir,"serial.log")
  f,createErr:=os.OpenFile(serialLog,os.O_CREATE|os.O_WRONLY|os.O_TRUNC,0600)
  if createErr!=nil{return nil,fmt.Errorf("prepare VM serial capture: %w",createErr)}
  _=f.Close()
  emit("serial","Serial console output configured: "+serialLog)
 }
 xmlSpec:=liveISODomainXMLWithLog(id,instance.Name,memoryMiB,vcpus,absISO,seed.ISO,disk,c.Virtualization.Network,instance.MACAddress,serialLog)
 xmlFile:=filepath.Join(dir,"domain.xml")
 if err:=os.WriteFile(xmlFile,[]byte(xmlSpec),0600);err!=nil{return nil,err}
 emit("domain","Libvirt XML created; serial console ttyS0 configured")
 if c.Virtualization.QEMUUID>0{
  if err:=grantQEMUStorageAccess(ctx,c.Virtualization.QEMUUID,absISO,disk,seed.ISO);err!=nil{return nil,err}
  if serialLog!=""{
   // Ensure only the configured QEMU uid can write serial output.
   if err:=grantQEMUStorageAccess(ctx,c.Virtualization.QEMUUID,serialLog,serialLog,seed.ISO);err!=nil{return nil,err}
  }
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
 emit("boot","Creating transient Libvirt domain from Live ISO")
 if out,err:=virsh(ctx,instance.uri,"create",xmlFile);err!=nil{return nil,fmt.Errorf("boot Live ISO: %w: %s",err,strings.TrimSpace(string(out)))}
 instance.active=true
 emit("boot","Libvirt accepted VM; waiting for DHCP lease and SSH; no guest boot has been verified")
 bootCtx,cancel:=context.WithTimeout(ctx,opts.ReadinessTimeout)
 defer cancel()
 ticker:=time.NewTicker(2*time.Second)
 defer ticker.Stop()
 lastDHCPReport:=time.Time{}
 for {
  if bootCtx.Err()!=nil{return nil,liveISOReadinessError("DHCP",opts.ReadinessTimeout,bootCtx.Err())}
  if _,err:=ActiveDomainState(bootCtx,instance.uri,id);err!=nil{
   if bootCtx.Err()!=nil{return nil,liveISOReadinessError("DHCP",opts.ReadinessTimeout,bootCtx.Err())}
   return nil,fmt.Errorf("Live ISO state check before DHCP failed: %w",err)
  }
  out,err:=virsh(bootCtx,instance.uri,"domifaddr",instance.Name,"--source","lease")
  if time.Since(lastDHCPReport)>30*time.Second{
   emit("dhcp-wait",fmt.Sprintf("No IPv4 lease yet; VM still present in Libvirt (lease probe error: %v)",err))
   lastDHCPReport=time.Now()
  }
  if err==nil{
   if ip:=parseIPv4(string(out));ip!=""{
    emit("dhcp","Lease detected for managed guest: "+ip)
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
 emit("ssh","DHCP ready; checking SSH and configured sudo mode")
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
 emit("ready",fmt.Sprintf("Live ISO guest reachable: uuid=%s ip=%s user=%s",id,instance.IPAddress,instance.SSHUser))
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
 return liveISODomainXMLWithLog(uuid,name,memory,vcpus,iso,seed,scratch,network,mac,"")
}

func liveISODomainXMLWithLog(uuid,name string,memory,vcpus int,iso,seed,scratch,network,mac,serialLog string)string{
 esc:=func(value string)string{
  var b strings.Builder
  _=xml.EscapeText(&b,[]byte(value))
  return strings.ReplaceAll(b.String(),"'","&apos;")
 }
 cdrom:=func(source,target string)string{
  return "<disk type='file' device='cdrom'><driver name='qemu' type='raw'/><source file='"+esc(source)+"'/><target dev='"+target+"' bus='sata'/><readonly/></disk>"
 }
 serialXML:="<serial type='pty'><target port='0'/></serial>"
 if serialLog!=""{
  serialXML="<serial type='pty'><target port='0'/><log file='"+esc(serialLog)+"' append='on'/></serial>"
 }
 return fmt.Sprintf("<domain type='kvm'><name>%s</name><uuid>%s</uuid><memory unit='MiB'>%d</memory><vcpu>%d</vcpu>"+
  "<os><type arch='x86_64'>hvm</type><boot dev='cdrom'/></os><features><acpi/></features><devices>"+
  "%s%s<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='%s'/><target dev='vda' bus='virtio'/></disk>"+
  "<interface type='network'><mac address='%s'/><source network='%s'/><model type='virtio'/></interface>"+
  "<channel type='unix'><source mode='bind'/><target type='virtio' name='org.qemu.guest_agent.0'/></channel>"+
  "%s<console type='pty'><target type='serial' port='0'/></console>"+
  "</devices></domain>",esc(name),esc(uuid),memory,vcpus,cdrom(iso,"sda"),cdrom(seed,"sdb"),esc(scratch),esc(mac),esc(network),serialXML)
}
