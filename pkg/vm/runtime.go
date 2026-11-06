// Package vm starts disposable Libvirt/QEMU instances from an existing QCOW2
// profile. It does NOT install an operating system from an installer ISO.
package vm

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/xml"
 "errors"
 "fmt"
 "net"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
)

type Instance struct {
 Name string
 Address string
 Directory string
 uri string
 active bool
}

// Start creates a private QCOW2 overlay, starts a transient domain and obtains
// the DHCP lease address from Libvirt. On partial failure resources are cleaned.
func Start(ctx context.Context,c *config.Config,p *profile.Profile)(*Instance,error){
 image,err:=p.ImagePath()
 if err!=nil{return nil,fmt.Errorf("resolve profile image: %w",err)}
 if _,err:=exec.LookPath("qemu-img");err!=nil{return nil,err}
 if _,err:=exec.LookPath("virsh");err!=nil{return nil,err}
 dir,err:=os.MkdirTemp(c.Storage.Runs,"insula-vm-")
 if err!=nil{return nil,err}
 instance:=&Instance{Directory:dir,uri:c.Virtualization.LibvirtURI}
 success:=false
 defer func(){
  if !success{
   cleanupCtx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
   defer cancel()
   _=instance.Cleanup(cleanupCtx)
  }
 }()
 overlay:=filepath.Join(dir,"overlay.qcow2")
 if out,err:=exec.CommandContext(ctx,"qemu-img","create","-f","qcow2","-F","qcow2","-b",image,overlay).CombinedOutput();err!=nil{
  return nil,fmt.Errorf("create overlay: %w: %s",err,strings.TrimSpace(string(out)))
 }
 nonce:=make([]byte,8)
 if _,err:=rand.Read(nonce);err!=nil{return nil,err}
 prefix:=p.Name
 if len(prefix)>28{prefix=prefix[:28]}
 instance.Name="insula-"+prefix+"-"+hex.EncodeToString(nonce)
 // Allocate a MAC with QEMU's locally assigned prefix and random suffix.
 macBytes:=make([]byte,3)
 if _,err:=rand.Read(macBytes);err!=nil{return nil,err}
 mac:=fmt.Sprintf("52:54:00:%02x:%02x:%02x",macBytes[0],macBytes[1],macBytes[2])
 spec:=domainXML(instance.Name,p.MemoryMiB,p.VCPUs,overlay,c.Virtualization.Network,mac)
 xmlPath:=filepath.Join(dir,"domain.xml")
 if err:=os.WriteFile(xmlPath,[]byte(spec),0600);err!=nil{return nil,err}
 if out,err:=virsh(ctx,instance.uri,"create",xmlPath);err!=nil{
  return nil,fmt.Errorf("start disposable domain: %w: %s",err,strings.TrimSpace(string(out)))
 }
 instance.active=true
 pollCtx,cancel:=context.WithTimeout(ctx,2*time.Minute)
 defer cancel()
 ticker:=time.NewTicker(2*time.Second)
 defer ticker.Stop()
 for{
  out,err:=virsh(pollCtx,instance.uri,"domifaddr",instance.Name,"--source","lease")
  if err==nil{
   if ip:=parseIPv4(string(out));ip!=""{
    instance.Address=net.JoinHostPort(ip,"22")
    success=true
    return instance,nil
   }
  }
  select{
  case <-pollCtx.Done():
   return nil,fmt.Errorf("waiting for VM DHCP lease: %w (guest must use Libvirt DHCP)",pollCtx.Err())
  case <-ticker.C:
  }
 }
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
   if ip!=nil && ip.To4()!=nil && !ip.IsLoopback() {return ip.String()}
  }
 }
 return ""
}

// Cleanup destroys the transient domain, then removes its overlay only if the
// domain is gone. This avoids unlinking a disk from a still-active VM.
func (i *Instance) Cleanup(ctx context.Context)error{
 if i.active{
  if out,err:=virsh(ctx,i.uri,"destroy",i.Name);err!=nil{
   // If already shut down, Libvirt may report "domain not found".
   // Confirm that it no longer exists before deleting its files.
   if _,checkErr:=virsh(ctx,i.uri,"dominfo",i.Name);checkErr==nil{
    return fmt.Errorf("destroy domain: %w: %s",err,strings.TrimSpace(string(out)))
   }
  }
  i.active=false
 }
 if i.Directory==""{return errors.New("empty VM directory")}
 return os.RemoveAll(i.Directory)
}

func domainXML(name string,memoryMiB,vcpus int,disk,network,mac string)string{
 escape:=func(v string)string{
  var b strings.Builder
  _=xml.EscapeText(&b,[]byte(v))
  return b.String()
 }
 return fmt.Sprintf(
  "<domain type='kvm'><name>%s</name><memory unit='MiB'>%d</memory>"+
   "<vcpu>%d</vcpu><os><type arch='x86_64'>hvm</type></os>"+
   "<devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/>"+
   "<source file='%s'/><target dev='vda' bus='virtio'/></disk>"+
   "<interface type='network'><mac address='%s'/><source network='%s'/>"+
   "<model type='virtio'/></interface>"+
   "<serial type='pty'><target port='0'/></serial>"+
   "<console type='pty'><target type='serial' port='0'/></console>"+
   "</devices></domain>",
  escape(name),memoryMiB,vcpus,escape(disk),escape(mac),escape(network))
}
