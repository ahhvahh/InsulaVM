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

// ResumeByID starts a stopped, transient InsulaVM domain from the preserved
// disk and XML, without allocating a new UUID, changing its QCOW2 or creating
// a new overlay. Installer guests try the installed hard disk before the ISO.
// A missing/unsafe disk or a still-existing domain is an error.
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
 if len(infos)!=1||infos[0].LibvirtState!="absent"{
  return registry.Entry{},fmt.Errorf("VM must be absent in Libvirt before resume (state=%s)",infos[0].LibvirtState)
 }
 dir,err:=managedInstanceDirectory(c,entry)
 if err!=nil{return registry.Entry{},err}
 path:=filepath.Join(dir,"domain.xml")
 info,err:=os.Lstat(path)
 if err!=nil{return registry.Entry{},fmt.Errorf("inspect preserved domain XML: %w",err)}
 if !info.Mode().IsRegular(){return registry.Entry{},errors.New("preserved domain XML is not a regular file")}
 content,err:=os.ReadFile(path)
 if err!=nil{return registry.Entry{},err}
 var domain struct{
  XMLName xml.Name
  Name string `xml:"name"`
  UUID string `xml:"uuid"`
  Devices struct{
   Disks []struct{
    Device string `xml:"device,attr"`
    Source struct{File string `xml:"file,attr"`} `xml:"source"`
    Target struct{Dev string `xml:"dev,attr"`} `xml:"target"`
   } `xml:"disk"`
  } `xml:"devices"`
 }
 if err:=xml.Unmarshal(content,&domain);err!=nil{return registry.Entry{},fmt.Errorf("decode preserved domain XML: %w",err)}
 if domain.XMLName.Local!="domain"||domain.UUID!=entry.LibvirtUUID||domain.Name!=entry.Name{
  return registry.Entry{},errors.New("preserved domain XML does not match registered VM identity")
 }
 var diskPath string
 for _,disk:=range domain.Devices.Disks{
  if disk.Device=="disk"&&disk.Target.Dev=="vda"{diskPath=disk.Source.File;break}
 }
 installer:=filepath.Join(dir,"installer.qcow2")
 overlay:=filepath.Join(dir,"overlay.qcow2")
 if diskPath!=installer&&diskPath!=overlay{
  return registry.Entry{},errors.New("preserved domain has no owned vda QCOW2")
 }
 diskInfo,err:=os.Lstat(diskPath)
 if err!=nil{return registry.Entry{},fmt.Errorf("inspect preserved VM disk: %w",err)}
 if !diskInfo.Mode().IsRegular(){return registry.Entry{},errors.New("preserved VM disk is not a regular file")}
 xmlFile:=path
 if diskPath==installer {
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
  return registry.Entry{},fmt.Errorf("restart preserved domain: %w: %s",err,strings.TrimSpace(string(out)))
 }
 if _,err:=ActiveDomainState(ctx,c.Virtualization.LibvirtURI,id);err!=nil{
  return registry.Entry{},fmt.Errorf("recreated domain, but running state is unconfirmed: %w",err)
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
