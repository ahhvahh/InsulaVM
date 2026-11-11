package profile

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

const validInstallerYAML=`name: alpine-virt
os: alpine
hardware:
  cpu:
    quantity: 1
  ram:
    mib: 500
disk:
  start_iso: ./iso/alpine-virt-3.24.4-x86_64.iso
  size_gb: 8
workspace:
  destination: /tmp/workspace
network:
  enabled: true
  type: libvirt
  name: default
  ipv4:
    mode: dhcp
    address: null
    gateway: null
`

func TestReadInstallerProfile(t *testing.T){
 root:=t.TempDir()
 dir:=filepath.Join(root,"alpine-virt")
 if err:=os.Mkdir(dir,0700);err!=nil{t.Fatal(err)}
 file:=filepath.Join(dir,"profile.yaml")
 if err:=os.WriteFile(file,[]byte(validInstallerYAML),0600);err!=nil{t.Fatal(err)}
 p,err:=ReadInstaller(root,"alpine-virt")
 if err!=nil{t.Fatal(err)}
 if p.Name!="alpine-virt"||p.Hardware.CPU.Quantity!=1||p.Hardware.RAM.MiB!=500||p.Disk.SizeGB!=8||p.Network.Name!="default"||p.Workspace.Destination!="/tmp/workspace"{
  t.Fatalf("installer fields lost: %+v",p)
 }
}

func TestInstallerProfileValidation(t *testing.T){
 good:=func()*InstallerProfile{
  var p InstallerProfile
  p.Name="alpine-virt"
  p.Hardware.CPU.Quantity=1;p.Hardware.RAM.MiB=500
  p.Disk.StartISO="./iso/alpine.iso";p.Disk.SizeGB=8
  p.Workspace.Destination="/tmp/workspace"
  p.Network.Enabled=true;p.Network.Type="libvirt";p.Network.Name="default";p.Network.IPv4.Mode="dhcp"
  return &p
 }
 cases:=[]struct{name string;change func(*InstallerProfile);want string}{
  {"cpu",func(p *InstallerProfile){p.Hardware.CPU.Quantity=0},"hardware.cpu.quantity"},
  {"ram",func(p *InstallerProfile){p.Hardware.RAM.MiB=127},"hardware.ram.mib"},
  {"disk",func(p *InstallerProfile){p.Disk.SizeGB=0},"disk.size_gb"},
  {"iso missing",func(p *InstallerProfile){p.Disk.StartISO=""},"disk.start_iso"},
  {"iso newline",func(p *InstallerProfile){p.Disk.StartISO="/tmp/hi\nthere"},"disk.start_iso"},
  {"relative workspace",func(p *InstallerProfile){p.Workspace.Destination="workspace"},"workspace.destination"},
  {"bad network",func(p *InstallerProfile){p.Network.Type="bridge"},"network.type"},
  {"invalid network name",func(p *InstallerProfile){p.Network.Name="../host"},"network.name"},
  {"static not yet supported",func(p *InstallerProfile){p.Network.IPv4.Mode="static"},"static"},
  {"invalid DHCP address",func(p *InstallerProfile){v:="192.168.122.50";p.Network.IPv4.Address=&v},"DHCP"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   p:=good();tc.change(p)
   if err:=p.ValidateInstaller();err==nil||!strings.Contains(err.Error(),tc.want){t.Fatalf("expected %s, got %v",tc.want,err)}
  })
 }
 if err:=good().ValidateInstaller();err!=nil{t.Fatal(err)}
}

func TestReadInstallerRejectsUnknownFieldsAndUnsafeFiles(t *testing.T){
 root:=t.TempDir();dir:=filepath.Join(root,"alpine-virt")
 if err:=os.Mkdir(dir,0700);err!=nil{t.Fatal(err)}
 filename:=filepath.Join(dir,"profile.yaml")
 for _,tc:=range []struct{name,source string}{
  {"unknown",validInstallerYAML+"unexpected: true\n"},
  {"multiple",validInstallerYAML+"---\nname: extra\n"},
  {"malformed","name: [bad\n"},
  {"directory name mismatch",strings.Replace(validInstallerYAML,"name: alpine-virt","name: other",1)},
 }{
  t.Run(tc.name,func(t *testing.T){
   if err:=os.WriteFile(filename,[]byte(tc.source),0600);err!=nil{t.Fatal(err)}
   if _,err:=ReadInstaller(root,"alpine-virt");err==nil{t.Fatal("unsafe installer profile accepted")}
  })
 }
 if err:=os.Remove(filename);err!=nil{t.Fatal(err)}
 other:=filepath.Join(t.TempDir(),"external.yaml")
 if err:=os.WriteFile(other,[]byte(validInstallerYAML),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(other,filename);err!=nil{t.Fatal(err)}
 if _,err:=ReadInstaller(root,"alpine-virt");err==nil{t.Fatal("symlink installer profile accepted")}
}
