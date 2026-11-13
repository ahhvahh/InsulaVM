package vm

import (
 "context"
 "encoding/xml"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// managedInstanceDirectory confirms that a VM's writable storage belongs to
// the InsulaVM runs directory. Never resolve a registered VM by its display name.
func managedInstanceDirectory(c *config.Config,entry registry.Entry)(string,error){
 if c==nil{return "",errors.New("config is required")}
 root,err:=filepath.Abs(c.Storage.Runs);if err!=nil{return "",err}
 dir,err:=filepath.Abs(entry.InstanceDir);if err!=nil{return "",err}
 rel,err:=filepath.Rel(root,dir)
 if err!=nil||rel=="."||rel==".."||strings.HasPrefix(rel,".."+string(os.PathSeparator))||
  !strings.HasPrefix(filepath.Base(dir),"insula-vm-"){
  return "",errors.New("unsafe registered VM directory")
 }
 info,err:=os.Lstat(dir)
 if err!=nil{return "",fmt.Errorf("inspect registered VM directory: %w",err)}
 if !info.IsDir(){return "",errors.New("registered VM directory is not a regular directory")}
 return dir,nil
}

// resumeDomain selects only the stable identity and writable disk fields
// needed to verify the preserved XML and any existing Libvirt definition.
type resumeDomain struct {
 XMLName xml.Name
 Name string `xml:"name"`
 UUID string `xml:"uuid"`
 Devices struct {
  Disks []struct {
   Device string `xml:"device,attr"`
   Source struct { File string `xml:"file,attr"` } `xml:"source"`
   Target struct { Dev string `xml:"dev,attr"` } `xml:"target"`
  } `xml:"disk"`
 } `xml:"devices"`
}

func(d resumeDomain)writableDisk()string{
 for _,disk:=range d.Devices.Disks{
  if disk.Device=="disk"&&disk.Target.Dev=="vda"{return disk.Source.File}
 }
 return ""
}

// ResumeByID safely starts an existing shut-off domain, resumes a paused
// domain, or recreates an absent transient domain from its preserved XML.
// Already-running domains are returned without a second start. UUID and disk
// are preserved, and the existing Libvirt definition must match the registry.
// Installer guests recreated from XML boot the installed disk first.
func ResumeByID(ctx context.Context,c *config.Config,id string)(registry.Entry,error){
 if ctx==nil||c==nil{return registry.Entry{},errors.New("context and config are required")}
 if err:=ctx.Err();err!=nil{return registry.Entry{},err}
 if id==""{return registry.Entry{},errors.New("VM ID is required")}
 store,err:=registry.Open(c.Storage.Database)
 if err!=nil{return registry.Entry{},err}
 entry,err:=store.Get(ctx,id)
 _=store.Close()
 if err!=nil{return registry.Entry{},err}
 if entry.ID!=id||entry.LibvirtUUID!=id{return registry.Entry{},errors.New("VM registry/Libvirt UUID mismatch")}
 if entry.Status=="destroyed"{return registry.Entry{},errors.New("destroyed VM cannot be resumed: its temporary disk was removed")}
 if entry.Status!="running"&&entry.Status!="failed"{
  return registry.Entry{},fmt.Errorf("VM status %q cannot be resumed",entry.Status)
 }
 infos,err:=InspectRegistered(ctx,c.Virtualization.LibvirtURI,[]registry.Entry{entry})
 if err!=nil{return registry.Entry{},err}
 if len(infos)!=1{return registry.Entry{},errors.New("unexpected Libvirt status count")}
 state:=infos[0].LibvirtState
 switch state{
 case "absent","shut off","paused","running","blocked":
  // All resumable states must pass the same ownership and disk checks.
 default:
  return registry.Entry{},fmt.Errorf("VM cannot be resumed from Libvirt state %q",state)
 }
 dir,err:=managedInstanceDirectory(c,entry)
 if err!=nil{return registry.Entry{},err}
 path:=filepath.Join(dir,"domain.xml")
 info,err:=os.Lstat(path)
 if err!=nil{return registry.Entry{},fmt.Errorf("inspect preserved domain XML: %w",err)}
 if !info.Mode().IsRegular(){return registry.Entry{},errors.New("preserved domain XML is not a regular file")}
 content,err:=os.ReadFile(path)
 if err!=nil{return registry.Entry{},err}
 var domain resumeDomain
 if err:=xml.Unmarshal(content,&domain);err!=nil{return registry.Entry{},fmt.Errorf("decode preserved domain XML: %w",err)}
 if domain.XMLName.Local!="domain"||domain.UUID!=entry.LibvirtUUID||domain.Name!=entry.Name{
  return registry.Entry{},errors.New("preserved domain XML does not match registered VM identity")
 }
 diskPath:=domain.writableDisk()
 installer:=filepath.Join(dir,"installer.qcow2")
 overlay:=filepath.Join(dir,"overlay.qcow2")
 if diskPath!=installer&&diskPath!=overlay{
  return registry.Entry{},errors.New("preserved domain has no owned vda QCOW2")
 }
 diskInfo,err:=os.Lstat(diskPath)
 if err!=nil{return registry.Entry{},fmt.Errorf("inspect preserved VM disk: %w",err)}
 if !diskInfo.Mode().IsRegular(){return registry.Entry{},errors.New("preserved VM disk is not a regular file")}
 switch state{
 case "absent":
  xmlFile:=path
  if diskPath==installer{
   // An installed OS should boot from vda, not re-enter the ISO installer.
   before:="<boot dev='cdrom'/><boot dev='hd'/>"
   if !strings.Contains(string(content),before){
    return registry.Entry{},errors.New("installer domain XML lacks the expected boot order")
   }
   content=[]byte(strings.Replace(string(content),before,"<boot dev='hd'/><boot dev='cdrom'/>",1))
   temporary,err:=os.CreateTemp(dir,"resume-*.xml")
   if err!=nil{return registry.Entry{},err}
   xmlFile=temporary.Name()
   defer os.Remove(xmlFile)
   if _,err:=temporary.Write(content);err!=nil{_ = temporary.Close();return registry.Entry{},err}
   if err:=temporary.Close();err!=nil{return registry.Entry{},err}
  }
  if out,err:=virsh(ctx,c.Virtualization.LibvirtURI,"create",xmlFile);err!=nil{
   return registry.Entry{},fmt.Errorf("recreate preserved domain: %w: %s",err,strings.TrimSpace(string(out)))
  }
 default:
  // A known UUID is not sufficient proof of ownership: verify the real
  // Libvirt definition before controlling an already-defined domain.
  output,err:=virsh(ctx,c.Virtualization.LibvirtURI,"dumpxml",id)
  if err!=nil{
   return registry.Entry{},fmt.Errorf("inspect defined Libvirt domain: %w: %s",err,strings.TrimSpace(string(output)))
  }
  var existing resumeDomain
  if err:=xml.Unmarshal(output,&existing);err!=nil{
   return registry.Entry{},fmt.Errorf("decode defined Libvirt domain XML: %w",err)
  }
  if existing.XMLName.Local!="domain"||existing.Name!=entry.Name||
   existing.UUID!=id||existing.writableDisk()!=diskPath{
   return registry.Entry{},errors.New("defined Libvirt domain does not match registered VM identity and disk")
  }
  action:=""
  switch state{
  case "shut off":action="start"
  case "paused":action="resume"
  }
  if action!=""{
   if out,err:=virsh(ctx,c.Virtualization.LibvirtURI,action,id);err!=nil{
    return registry.Entry{},fmt.Errorf("%s defined Libvirt domain: %w: %s",action,err,strings.TrimSpace(string(out)))
   }
  }
 }
 if _,err:=ActiveDomainState(ctx,c.Virtualization.LibvirtURI,id);err!=nil{
  return registry.Entry{},fmt.Errorf("VM is not running after resume (previous state=%s): %w",state,err)
 }
 // An already-running VM must not lose its known IP or be restarted.
 if (state=="running"||state=="blocked")&&entry.Status=="running"{
  return entry,nil
 }
 store,err=registry.Open(c.Storage.Database)
 if err!=nil{return registry.Entry{},fmt.Errorf("domain started but registry unavailable: %w",err)}
 defer store.Close()
 if err:=store.SetRestarted(ctx,id);err!=nil{
  return registry.Entry{},fmt.Errorf("domain started but registry update failed: %w",err)
 }
 entry.IP=""
 entry.Status="running"
 return entry,nil
}
