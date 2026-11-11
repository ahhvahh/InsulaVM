package vm

import (
 "context"
 "encoding/xml"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/profile"
)

func TestInstallerISODomainXMLContract(t *testing.T){
 const uuid="019a5d11-1234-7123-8123-123456789abc"
 spec:=installerISODomainXML(uuid,"insula-alpine-virt",500,1,"/tmp/guest's & disk.qcow2","/tmp/alpine's & installer.iso","52:54:00:11:22:33",true,"default")
 for _,part:=range []string{
  "<domain type='kvm'>","<uuid>"+uuid+"</uuid>","<memory unit='MiB'>500</memory>",
  "<vcpu>1</vcpu>","<boot dev='cdrom'/>","<boot dev='hd'/>",
  "device='cdrom'","<readonly/>","type='qcow2'","bus='virtio'",
  "guest&apos;s &amp; disk.qcow2","alpine&apos;s &amp; installer.iso",
  "<serial type='pty'>","<console type='pty'>","<source network='default'/>",
 }{
  if !strings.Contains(spec,part){t.Errorf("missing %q in domain XML: %s",part,spec)}
 }
 var parsed struct{XMLName xml.Name;Name string `xml:"name"`;UUID string `xml:"uuid"`}
 if err:=xml.Unmarshal([]byte(spec),&parsed);err!=nil{t.Fatal(err)}
 if parsed.UUID!=uuid||parsed.Name!="insula-alpine-virt"{t.Fatalf("invalid domain identity: %+v",parsed)}
 withoutNetwork:=installerISODomainXML(uuid,"insula-test",500,1,"/tmp/disk.qcow2","/tmp/alpine.iso","",false,"")
 if strings.Contains(withoutNetwork,"<interface"){t.Fatalf("network disabled but XML has network interface: %s",withoutNetwork)}
}

func TestInstallerISORejectsInvalidInputs(t *testing.T){
 _,err:=StartInstallerISO(nil,nil,InstallerISOOptions{},nil)
 if err==nil{t.Fatal("nil context accepted")}
 p:=&profile.InstallerProfile{}
 _,err=StartInstallerISO(context.Background(),&config.Config{},InstallerISOOptions{Profile:p},nil)
 if err==nil||!strings.Contains(err.Error(),"installer name"){t.Fatalf("invalid profile accepted: %v",err)}
 p.Name="alpine-virt";p.Hardware.CPU.Quantity=1;p.Hardware.RAM.MiB=500
 p.Disk.StartISO="./iso/alpine.iso";p.Disk.SizeGB=8
 _,err=StartInstallerISO(context.Background(),&config.Config{},InstallerISOOptions{Profile:p,ISOPath:"./iso/alpine.iso"},nil)
 if err==nil||!strings.Contains(err.Error(),"absolute"){t.Fatalf("relative ISO accepted: %v",err)}
 _,err=StartInstallerISO(context.Background(),&config.Config{},InstallerISOOptions{Profile:p,ISOPath:"/does/not/exist/alpine.iso"},nil)
 if err==nil||!strings.Contains(err.Error(),"inspect installer ISO"){t.Fatalf("missing ISO accepted: %v",err)}
}
