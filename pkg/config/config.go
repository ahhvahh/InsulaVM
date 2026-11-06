// Package config loads the host-wide InsulaVM configuration.
// It never creates persistent files merely by reading configuration.
package config

import (
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "path"
 "strings"

 "gopkg.in/yaml.v3"
)

type Storage struct {
 Root string `yaml:"root"`
 Profiles string `yaml:"profiles"`
 Runs string `yaml:"runs"`
 Logs string `yaml:"logs"`
 Database string `yaml:"database"`
}

type Virtualization struct {
 Provider string `yaml:"provider"`
 LibvirtURI string `yaml:"libvirt_uri"`
 Network string `yaml:"network"`
 DefaultDiskGB int `yaml:"default_disk_gb"`
 // QEMUUID is the non-root host uid used by qemu:///system for guest disks.
 // Zero disables ACL provisioning; never use the root UID.
 QEMUUID int `yaml:"qemu_uid"`
}

type SSH struct {
 User string `yaml:"user"`
 PrivateKey string `yaml:"private_key"`
 KnownHosts string `yaml:"known_hosts"`
 ConnectTimeoutSeconds int `yaml:"connect_timeout_seconds"`
 SudoMode string `yaml:"sudo_mode"`
}

// Guest configures the shared target directory INSIDE every guest VM.
// The same destination is used for imports and the default job workspace.
type Guest struct {
 Destination string `yaml:"destination"`
}

type Config struct {
 Guest Guest `yaml:"guest"`
 Storage Storage `yaml:"storage"`
 Virtualization Virtualization `yaml:"virtualization"`
 SSH SSH `yaml:"ssh"`
}

func ReadFile(filename string) (*Config,error) {
 absolute,err:=filepath.Abs(filename)
 if err!=nil{return nil,err}
 file,err:=os.Open(absolute)
 if err!=nil{return nil,fmt.Errorf("open global config: %w",err)}
 defer file.Close()
 var c Config
 decoder:=yaml.NewDecoder(file)
 decoder.KnownFields(true)
 if err:=decoder.Decode(&c);err!=nil{return nil,fmt.Errorf("decode global config: %w",err)}
 var extra interface{}
 if err:=decoder.Decode(&extra);!errors.Is(err,io.EOF) {
  if err==nil{return nil,errors.New("only one YAML document is allowed")}
  return nil,fmt.Errorf("decode trailing config: %w",err)
 }
 if err:=c.normalize(filepath.Dir(absolute));err!=nil{return nil,err}
 return &c,nil
}

func (c *Config) normalize(configDir string) error {
 if strings.TrimSpace(c.Storage.Root)=="" {return errors.New("storage.root is required")}
 c.Storage.Root=absolutePath(configDir,c.Storage.Root)
 if c.Storage.Root==string(filepath.Separator) {return errors.New("storage.root cannot be filesystem root")}
 c.Storage.Profiles=childOrAbsolute(c.Storage.Root,c.Storage.Profiles,"profiles")
 c.Storage.Runs=childOrAbsolute(c.Storage.Root,c.Storage.Runs,"runs")
 c.Storage.Logs=childOrAbsolute(c.Storage.Root,c.Storage.Logs,"logs")
 c.Storage.Database=childOrAbsolute(c.Storage.Root,c.Storage.Database,"insula.db")
 if c.Storage.Profiles==c.Storage.Runs || c.Storage.Profiles==c.Storage.Logs || c.Storage.Runs==c.Storage.Logs {
  return errors.New("profiles, runs and logs directories must be distinct")
 }
 for _,p:=range []string{c.Storage.Profiles,c.Storage.Runs,c.Storage.Logs}{
  if p==string(filepath.Separator){return errors.New("storage directory cannot be filesystem root")}
 }
 if c.Guest.Destination==""{c.Guest.Destination="/tmp/insulavm/workspace"}
 if !path.IsAbs(c.Guest.Destination)||path.Clean(c.Guest.Destination)=="/"||
   strings.ContainsAny(c.Guest.Destination,string([]byte{0,10,13})){
  return errors.New("guest.destination must be an absolute Linux directory other than /")
 }
 c.Guest.Destination=path.Clean(c.Guest.Destination)
 if c.Virtualization.Provider==""{c.Virtualization.Provider="kvm"}
 if c.Virtualization.Provider!="kvm"{return fmt.Errorf("unsupported virtualization.provider %q",c.Virtualization.Provider)}
 if c.Virtualization.LibvirtURI==""{c.Virtualization.LibvirtURI="qemu:///system"}
 if c.Virtualization.Network==""{c.Virtualization.Network="default"}
 if strings.ContainsAny(c.Virtualization.Network," /\\\n\r\x00") || c.Virtualization.Network==".." {
  return errors.New("invalid virtualization.network")
 }
 if c.Virtualization.DefaultDiskGB==0{c.Virtualization.DefaultDiskGB=8}
 if c.Virtualization.DefaultDiskGB<1 {return errors.New("virtualization.default_disk_gb must be positive")}
 if c.Virtualization.QEMUUID<0 || c.Virtualization.QEMUUID>2147483647 {return errors.New("virtualization.qemu_uid must be 0 (disabled) or a positive Linux uid")}
 if c.Virtualization.QEMUUID==0 { /* unset: storage ACL provisioning remains disabled */ }
 if c.SSH.ConnectTimeoutSeconds==0{c.SSH.ConnectTimeoutSeconds=30}
 if c.SSH.User==""{c.SSH.User="insula"}
 if c.SSH.SudoMode==""{c.SSH.SudoMode="disabled"}
 if c.SSH.SudoMode!="passwordless"&&c.SSH.SudoMode!="disabled"{return fmt.Errorf("unsupported ssh.sudo_mode %q",c.SSH.SudoMode)}
 if c.SSH.ConnectTimeoutSeconds<1 || c.SSH.ConnectTimeoutSeconds>3600{
  return errors.New("ssh.connect_timeout_seconds must be 1..3600")
 }
 // Relative SSH paths are anchored to the configuration file directory.
 if c.SSH.PrivateKey!=""{c.SSH.PrivateKey=absolutePath(configDir,c.SSH.PrivateKey)}
 if c.SSH.KnownHosts!=""{c.SSH.KnownHosts=absolutePath(configDir,c.SSH.KnownHosts)}
 return nil
}

func absolutePath(base,p string)string{
 if filepath.IsAbs(p){return filepath.Clean(p)}
 return filepath.Clean(filepath.Join(base,p))
}
func childOrAbsolute(root,value,defaultName string)string{
 if strings.TrimSpace(value)==""{value=defaultName}
 return absolutePath(root,value)
}

// EnsureDirectories creates only the configured application locations, with
// private permissions. Each execution creates its own subdirectory separately.
func (c *Config) EnsureDirectories()error{
 for _,p:=range []string{c.Storage.Root,c.Storage.Profiles,c.Storage.Runs,c.Storage.Logs}{
  if err:=os.MkdirAll(p,0700);err!=nil{return fmt.Errorf("create %s: %w",p,err)}
 }
 return nil
}
