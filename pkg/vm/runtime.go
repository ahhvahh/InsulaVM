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

// Start generates a client and guest host key, writes a NoCloud seed ISO,
// starts a transient VM and waits for verified SSH readiness before returning.
// A separate per-instance QCOW2 overlay is always used.
func Start(ctx context.Context,c *config.Config,p *profile.Profile)(instance *Instance,runErr error){
 if ctx==nil||c==nil||p==nil{return nil,errors.New("context, config and profile are required")}
 image,err:=p.ImagePath()
 if err!=nil{return nil,fmt.Errorf("resolve profile image: %w",err)}
 for _,tool:=range []string{"qemu-img","virsh"}{
  if _,err:=exec.LookPath(tool);err!=nil{return nil,fmt.Errorf("missing VM host tool %s: %w",tool,err)}
 }
 if err:=os.MkdirAll(c.Storage.Runs,0700);err!=nil{return nil,err}
 dir,err:=os.MkdirTemp(c.Storage.Runs,"insula-vm-")
 if err!=nil{return nil,err}
 instance=&Instance{Directory:dir,uri:c.Virtualization.LibvirtURI,registryPath:c.Storage.Database,Profile:p.Name,SudoMode:c.SSH.SudoMode}
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

 overlay:=filepath.Join(dir,"overlay.qcow2")
 if out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2","-F","qcow2","-b",image,overlay).CombinedOutput();err!=nil{
  return nil,fmt.Errorf("create overlay: %w: %s",err,strings.TrimSpace(string(out)))
 }
 xmlSpec:=domainXMLWithSeed(instance.LibvirtUUID,instance.Name,p.MemoryMiB,p.VCPUs,overlay,seed.ISO,c.Virtualization.Network,instance.MACAddress)
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

 if out,err:=virsh(ctx,instance.uri,"create",xmlPath);err!=nil{
  return nil,fmt.Errorf("start disposable domain: %w: %s",err,strings.TrimSpace(string(out)))
 }
 instance.active=true
 bootCtx,cancel:=context.WithTimeout(ctx,3*time.Minute)
 defer cancel()
 ticker:=time.NewTicker(2*time.Second)
 defer ticker.Stop()
 for {
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
  case <-bootCtx.Done():return nil,fmt.Errorf("waiting for VM DHCP lease: %w",bootCtx.Err())
  case <-ticker.C:
  }
 }
 // Verify the per-instance host key through known_hosts. The SSH login key
 // is generated for this VM, not shared with other instances.
 var lastErr error
 for {
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
   output,checkErr:=virsh(ctx,i.uri,"dominfo",i.Name)
   if checkErr==nil||!strings.Contains(strings.ToLower(string(output)),"not found"){
    return fmt.Errorf("cannot confirm VM stopped; refusing to remove disk: %w: %s",err,strings.TrimSpace(string(out)))
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
 if entry.Status=="destroyed"{return nil}
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
  ID:id,Name:entry.Name,Directory:path,uri:c.Virtualization.LibvirtURI,
  registryPath:c.Storage.Database,active:true,
 }
 return instance.Cleanup(ctx)
}

// domainXML preserves the original helper for existing unit tests and
// existing Linux profiles; new VMs use domainXMLWithSeed.
func domainXML(name string,memoryMiB,vcpus int,disk,network,mac string)string{
 return domainXMLWithSeed("",name,memoryMiB,vcpus,disk,"",network,mac)
}

func domainXMLWithSeed(uuid,name string,memoryMiB,vcpus int,disk,seedISO,network,mac string)string{
 escape:=func(v string)string{
  var b strings.Builder
  _=xml.EscapeText(&b,[]byte(v))
  return strings.ReplaceAll(b.String(),"'","&apos;")
 }
 var uuidElement string
 if uuid!=""{uuidElement="<uuid>"+escape(uuid)+"</uuid>"}
 var seedDisk string
 if seedISO!=""{
  seedDisk="<disk type='file' device='cdrom'><driver name='qemu' type='raw'/>"+
   "<source file='"+escape(seedISO)+"'/><target dev='sda' bus='sata'/><readonly/></disk>"
 }
 return fmt.Sprintf(
  "<domain type='kvm'><name>%s</name>%s<memory unit='MiB'>%d</memory>"+
   "<vcpu>%d</vcpu><os><type arch='x86_64'>hvm</type></os>"+
   "<devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/>"+
   "<source file='%s'/><target dev='vda' bus='virtio'/></disk>%s"+
   "<interface type='network'><mac address='%s'/><source network='%s'/>"+
   "<model type='virtio'/></interface>"+
   "<serial type='pty'><target port='0'/></serial>"+
   "<console type='pty'><target type='serial' port='0'/></console>"+
   "</devices></domain>",
  escape(name),uuidElement,memoryMiB,vcpus,escape(disk),seedDisk,escape(mac),escape(network))
}
