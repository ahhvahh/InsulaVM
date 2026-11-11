package vm

import (
 "context"
 "crypto/rand"
 "encoding/xml"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/identity"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// InstallerISOOptions starts a guest from an unmodified installer/live ISO.
// Its VM becomes available at the Libvirt console; SSH and OS boot are NOT
// asserted merely because the hypervisor reports the domain as running.
type InstallerISOOptions struct {
 ISOPath string
 Profile *profile.InstallerProfile
 PreserveOnFailure bool
}

// StartInstallerISO provisions an independent writable QCOW2 and boots the
// ISO read-only. It registers a unique VM UUID in the shared SQLite database.
// The disk is disposable and StopByID removes it: install media guests should
// be exported before StopByID if their installed filesystem must be retained.
func StartInstallerISO(ctx context.Context,c *config.Config,opts InstallerISOOptions,
 onProgress func(Progress))(instance *Instance,runErr error){
 if ctx==nil||c==nil||opts.Profile==nil{return nil,errors.New("context, config and installer profile are required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 if err:=opts.Profile.ValidateInstaller();err!=nil{return nil,err}
 iso:=opts.ISOPath
 if !filepath.IsAbs(iso){return nil,errors.New("ISOPath must be an absolute host path")}
 info,err:=os.Lstat(iso)
 if err!=nil{return nil,fmt.Errorf("inspect installer ISO: %w",err)}
 if !info.Mode().IsRegular()||info.Size()==0{return nil,errors.New("installer ISO must be a nonempty regular file")}
 for _,tool:=range []string{"virsh","qemu-img"}{
  if _,err:=exec.LookPath(tool);err!=nil{return nil,fmt.Errorf("missing VM host tool %s: %w",tool,err)}
 }
 if _,err:=os.Stat("/dev/kvm");err!=nil{return nil,fmt.Errorf("KVM device unavailable: %w",err)}
 if err:=c.EnsureDirectories();err!=nil{return nil,err}
 dir,err:=os.MkdirTemp(c.Storage.Runs,"insula-vm-")
 if err!=nil{return nil,err}
 p:=opts.Profile
 instance=&Instance{Directory:dir,uri:c.Virtualization.LibvirtURI,registryPath:c.Storage.Database,Profile:p.Name,SudoMode:"disabled"}
 // Keep a stable pointer: named return values become nil on return nil,err.
 created:=instance
 emit:=func(stage,message string){
  if onProgress!=nil{onProgress(Progress{Stage:stage,Message:message,ID:created.ID,Name:created.Name,At:time.Now()})}
 }
 registered:=false
 success:=false
 defer func(){
  if success{return}
  emit("failed",fmt.Sprintf("ISO VM failed: %v",runErr))
  if opts.PreserveOnFailure&&registered&&created.active{
   emit("preserved","Domain/disk preserved for diagnostics; stop it explicitly by UUID")
   store,err:=registry.Open(c.Storage.Database)
   if err==nil{_ = store.SetFailed(context.Background(),created.ID);_ = store.Close()}
   return
  }
  cleanupCtx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
  defer cancel()
  if err:=created.Cleanup(cleanupCtx);err!=nil{
   runErr=errors.Join(runErr,fmt.Errorf("cleanup failed installer instance: %w",err))
  }
  if registered{
   store,err:=registry.Open(c.Storage.Database)
   if err==nil{_ = store.SetFailed(cleanupCtx,created.ID);_ = store.Close()}
  }
 }()
 id,err:=unusedVMID(ctx,c.Storage.Database,identity.UUIDv7)
 if err!=nil{return nil,fmt.Errorf("allocate installer VM ID: %w",err)}
 instance.ID=id
 instance.LibvirtUUID=id
 instance.Name=vmDomainName(p.Name,id)
 var mac [3]byte
 if _,err:=rand.Read(mac[:]);err!=nil{return nil,err}
 instance.MACAddress=fmt.Sprintf("52:54:00:%02x:%02x:%02x",mac[0],mac[1],mac[2])
 disk:=filepath.Join(dir,"installer.qcow2")
 emit("disk","Creating isolated QCOW2 disk for installer")
 out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2",disk,fmt.Sprintf("%dG",p.Disk.SizeGB)).CombinedOutput()
 if err!=nil{return nil,fmt.Errorf("create installer QCOW2: %w: %s",err,strings.TrimSpace(string(out)))}
 if c.Virtualization.QEMUUID>0{
  if err:=GrantQEMUInstallAccess(ctx,c.Virtualization.QEMUUID,iso,disk);err!=nil{return nil,err}
 }
 xmlSpec:=installerISODomainXML(id,instance.Name,p.Hardware.RAM.MiB,p.Hardware.CPU.Quantity,disk,iso,instance.MACAddress,p.Network.Enabled,p.Network.Name)
 xmlPath:=filepath.Join(dir,"domain.xml")
 if err:=os.WriteFile(xmlPath,[]byte(xmlSpec),0600);err!=nil{return nil,err}
 store,err:=registry.Open(c.Storage.Database)
 if err!=nil{return nil,err}
 insertErr:=store.Insert(ctx,registry.Entry{
  ID:id,LibvirtUUID:id,Name:instance.Name,Profile:p.Name,MAC:instance.MACAddress,
  SudoMode:"disabled",SSHPort:22,InstanceDir:dir,
 })
 _=store.Close()
 if insertErr!=nil{return nil,fmt.Errorf("register installer VM: %w",insertErr)}
 registered=true
 emit("booting","Creating Libvirt domain from the installer ISO")
 if out,err:=virsh(ctx,instance.uri,"create",xmlPath);err!=nil{
  return instance,fmt.Errorf("Libvirt ISO domain creation: %w: %s",err,strings.TrimSpace(string(out)))
 }
 instance.active=true
 state,err:=ActiveDomainState(ctx,instance.uri,instance.LibvirtUUID)
 if err!=nil{return instance,err}
 store,err=registry.Open(c.Storage.Database)
 if err!=nil{return instance,err}
 updateErr:=store.SetRunning(ctx,id,"")
 _=store.Close()
 if updateErr!=nil{return instance,fmt.Errorf("persist installer VM state: %w",updateErr)}
 emit("running","Domain in Libvirt state "+state+"; guest boot must be checked through vm console")
 success=true
 return instance,nil
}

// installerISODomainXML uses the same Libvirt/KVM architecture and virtio
// devices as regular InsulaVM guests, but boots the read-only CD-ROM first.
// An absent network interface is a supported, isolated installer mode.
func installerISODomainXML(id,name string,memoryMiB,vcpus int,disk,iso,mac string,networkEnabled bool,network string)string{
 escape:=func(s string)string{
  var b strings.Builder
  _=xml.EscapeText(&b,[]byte(s))
  return strings.ReplaceAll(b.String(),"'","&apos;")
 }
 nic:=""
 if networkEnabled{
  nic="<interface type='network'><mac address='"+escape(mac)+"'/><source network='"+escape(network)+"'/><model type='virtio'/></interface>"
 }
 return fmt.Sprintf("<domain type='kvm'><name>%s</name><uuid>%s</uuid>"+
  "<memory unit='MiB'>%d</memory><vcpu>%d</vcpu>"+
  "<os><type arch='x86_64'>hvm</type><boot dev='cdrom'/><boot dev='hd'/></os>"+
  "<features><acpi/></features><devices>"+
  "<disk type='file' device='disk'><driver name='qemu' type='qcow2'/>"+
  "<source file='%s'/><target dev='vda' bus='virtio'/></disk>"+
  "<disk type='file' device='cdrom'><driver name='qemu' type='raw'/>"+
  "<source file='%s'/><target dev='sda' bus='sata'/><readonly/></disk>"+
  "%s<serial type='pty'><target port='0'/></serial>"+
  "<console type='pty'><target type='serial' port='0'/></console>"+
  "</devices></domain>",
  escape(name),escape(id),memoryMiB,vcpus,escape(disk),escape(iso),nic)
}
