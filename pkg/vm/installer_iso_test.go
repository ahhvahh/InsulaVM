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
  "device='cdrom'","<readonly/>","<seclabel model='dac' relabel='no'/>","type='qcow2'","bus='virtio'",
  "guest&#39;s &amp; disk.qcow2","alpine&#39;s &amp; installer.iso",
  "<serial type='pty'>","<console type='pty'>","<source network='default'/>",
  "<channel type='unix'><target type='virtio' name='org.qemu.guest_agent.0'/></channel>",
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

func TestInstallerISOVNCAndSerialConsoles(t *testing.T) {
 for _,tc:=range []struct{
  name string
  networkEnabled bool
  wantInterfaces int
 }{
  {name:"with DHCP-capable NIC",networkEnabled:true,wantInterfaces:1},
  {name:"isolated installer",networkEnabled:false,wantInterfaces:0},
 }{
  t.Run(tc.name,func(t *testing.T){
   spec:=installerISODomainXML("019a5d11-1234-7123-8123-123456789abc",
    "insula-alpine-virt",500,1,"/tmp/disk.qcow2","/tmp/alpine.iso",
    "52:54:00:11:22:33",tc.networkEnabled,"default")
   var parsed struct{
    Devices struct{
     Graphics []struct{
      Type string `xml:"type,attr"`
      Port string `xml:"port,attr"`
      AutoPort string `xml:"autoport,attr"`
      Listen []struct{
       Type string `xml:"type,attr"`
       Address string `xml:"address,attr"`
      } `xml:"listen"`
     } `xml:"graphics"`
     Video []struct{
      Model struct{Type string `xml:"type,attr"`} `xml:"model"`
     } `xml:"video"`
     Serial []struct{} `xml:"serial"`
     Console []struct{} `xml:"console"`
     Interface []struct{} `xml:"interface"`
     Channels []struct {
      Type string `xml:"type,attr"`
      Target struct {Type string `xml:"type,attr"`; Name string `xml:"name,attr"`} `xml:"target"`
     } `xml:"channel"`
    } `xml:"devices"`
   }
   if err:=xml.Unmarshal([]byte(spec),&parsed);err!=nil{t.Fatalf("invalid domain XML: %v",err)}
   devices:=parsed.Devices
   if len(devices.Graphics)!=1{t.Fatalf("expected one VNC device, got %d",len(devices.Graphics))}
   graphics:=devices.Graphics[0]
   if graphics.Type!="vnc"||graphics.Port!="-1"||graphics.AutoPort!="yes" {
    t.Errorf("VNC must use automatic port allocation: %+v",graphics)
   }
   if len(graphics.Listen)!=1||graphics.Listen[0].Type!="address"||graphics.Listen[0].Address!="127.0.0.1" {
    t.Errorf("VNC must listen only on IPv4 loopback: %+v",graphics.Listen)
   }
   if len(devices.Video)!=1||devices.Video[0].Model.Type!="vga" {
    t.Errorf("installer needs one VGA device: %+v",devices.Video)
   }
   if len(devices.Serial)!=1||len(devices.Console)!=1{
    t.Errorf("serial console must remain available: serial=%d console=%d",len(devices.Serial),len(devices.Console))
   }
   if len(devices.Channels)!=1||devices.Channels[0].Type!="unix"||devices.Channels[0].Target.Type!="virtio"||devices.Channels[0].Target.Name!="org.qemu.guest_agent.0"{
    t.Errorf("installer must expose exactly one QGA channel: %+v",devices.Channels)
   }
   if got:=len(devices.Interface);got!=tc.wantInterfaces {
    t.Errorf("NIC count=%d, want=%d",got,tc.wantInterfaces)
   }
  })
 }
}
