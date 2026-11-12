// Package profile parses saved VM templates and installer profiles.
package profile

import (
 "errors"
 "fmt"
 "io"
 "os"
 "path"
 "path/filepath"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/storage"
 "gopkg.in/yaml.v3"
)

// InstallerProfile boots an unmodified installer ISO into an isolated QCOW2.
// It is intentionally separate from the established, already-installed
// Profile type: the two workflows have different readiness guarantees.
type InstallerProfile struct {
 Name string `yaml:"name"`
 OS string `yaml:"os,omitempty"`
 Hardware struct {
  CPU struct { Quantity int `yaml:"quantity"` } `yaml:"cpu"`
  RAM struct { MiB int `yaml:"mib"` } `yaml:"ram"`
 } `yaml:"hardware"`
 Disk struct {
  StartISO string `yaml:"start_iso"`
  SizeGB int `yaml:"size_gb"`
 } `yaml:"disk"`
 Workspace struct { Destination string `yaml:"destination,omitempty"` } `yaml:"workspace,omitempty"`
 Network struct {
  Enabled bool `yaml:"enabled"`
  Type string `yaml:"type,omitempty"`
  Name string `yaml:"name,omitempty"`
  IPv4 struct {
   Mode string `yaml:"mode,omitempty"`
   Address *string `yaml:"address,omitempty"`
   Gateway *string `yaml:"gateway,omitempty"`
  } `yaml:"ipv4,omitempty"`
 } `yaml:"network"`
}

// ValidateInstaller rejects unsupported modes rather than silently pretending
// to configure IP or network settings inside an unmodified live ISO.
func(p *InstallerProfile)ValidateInstaller()error{
 if p==nil{return errors.New("installer profile is required")}
 if err:=storage.ValidateName(p.Name);err!=nil{return fmt.Errorf("installer name: %w",err)}
 if p.Hardware.CPU.Quantity<1||p.Hardware.CPU.Quantity>256{return errors.New("hardware.cpu.quantity must be 1..256")}
 if p.Hardware.RAM.MiB<128||p.Hardware.RAM.MiB>1048576{return errors.New("hardware.ram.mib must be 128..1048576")}
 if p.Disk.SizeGB<1{return errors.New("disk.size_gb must be positive")}
 if strings.TrimSpace(p.Disk.StartISO)==""||strings.ContainsAny(p.Disk.StartISO,"\x00\r\n"){
  return errors.New("disk.start_iso must be a nonempty ISO path without NUL or newlines")
 }
 if dest:=p.Workspace.Destination;dest!=""&&(!path.IsAbs(dest)||path.Clean(dest)=="/"||strings.ContainsAny(dest,"\x00\r\n")){
  return errors.New("workspace.destination must be an absolute guest directory other than /")
 }
 if p.Network.Enabled{
  if p.Network.Type!=""&&p.Network.Type!="libvirt"{return errors.New("network.type must be libvirt")}
  if p.Network.Name==""{p.Network.Name="default"}
  if strings.ContainsAny(p.Network.Name," /\\\x00\r\n")||p.Network.Name==".."{
   return errors.New("network.name is invalid")
  }
  if p.Network.IPv4.Mode==""{p.Network.IPv4.Mode="dhcp"}
  if p.Network.IPv4.Mode!="dhcp"{
   return errors.New("network.ipv4.mode=static is not supported for unmodified installer ISOs; configure the guest manually")
  }
  if p.Network.IPv4.Address!=nil||p.Network.IPv4.Gateway!=nil{
   return errors.New("DHCP mode cannot specify static IPv4 address/gateway")
  }
 }
 return nil
}

// ReadInstaller reads a hardware/disk/network YAML profile. The installer
// ISO is resolved relative to the global config file, not this profile dir.
func ReadInstaller(root,name string)(*InstallerProfile,error){
 if err:=storage.ValidateName(name);err!=nil{return nil,err}
 filename:=filepath.Join(root,name,"profile.yaml")
 info,err:=os.Lstat(filename)
 if err!=nil{return nil,err}
 if !info.Mode().IsRegular(){return nil,errors.New("installer profile.yaml must be a regular file")}
 f,err:=os.Open(filename)
 if err!=nil{return nil,err}
 defer f.Close()
 dec:=yaml.NewDecoder(f)
 dec.KnownFields(true)
 var p InstallerProfile
 if err:=dec.Decode(&p);err!=nil{return nil,fmt.Errorf("decode installer profile: %w",err)}
 var extra interface{}
 if err:=dec.Decode(&extra);!errors.Is(err,io.EOF){
  if err==nil{return nil,errors.New("only one YAML document is allowed")}
  return nil,fmt.Errorf("decode trailing installer profile: %w",err)
 }
 if p.Name!=name{return nil,fmt.Errorf("installer profile name %q does not match directory %q",p.Name,name)}
 if err:=p.ValidateInstaller();err!=nil{return nil,err}
 return &p,nil
}
