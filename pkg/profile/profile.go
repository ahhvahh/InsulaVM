// Package profile resolves a saved VM template (metadata + immutable QCOW2).
// Profile files live under <profiles_root>/<name>/profile.yaml.
package profile

import (
 "errors"
 "fmt"
 "io"
 "os"
 "path"
 "path/filepath"
 "sort"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/storage"
 "gopkg.in/yaml.v3"
)

type SSH struct {
 Address string `yaml:"address,omitempty"`
 User string `yaml:"user,omitempty"`
 PrivateKey string `yaml:"private_key,omitempty"`
 KnownHosts string `yaml:"known_hosts,omitempty"`
}

// Workspace stores the guest working directory for this profile.
// It does not need to exist before an SSH shell connection.
type Workspace struct {
 Destination string `yaml:"destination,omitempty"`
}

// Guest retains the per-profile guest destination used by existing profiles.
// workspace.destination takes precedence when both fields are present.
type Guest struct {
 Destination string `yaml:"destination,omitempty"`
}

const DefaultGuestDestination = "/tmp/insulavm/workspace"

// ValidateGuestDestination validates a guest-internal absolute path.
// An empty value denotes an unset field in legacy profile metadata.
func ValidateGuestDestination(dest string) error {
 if dest=="" {return nil}
 if !path.IsAbs(dest)||path.Clean(dest)=="/"||strings.ContainsAny(dest,"\x00\n\r") {
  return errors.New("guest.destination must be an absolute Linux directory other than /")
 }
 return nil
}

// GuestDestination resolves both accepted profile fields before the legacy default.
func (p *Profile) GuestDestination(legacy string) string {
 if p!=nil {
  if p.Workspace.Destination!="" {return path.Clean(p.Workspace.Destination)}
  if p.Guest.Destination!="" {return path.Clean(p.Guest.Destination)}
 }
 if legacy!="" {return legacy}
 return DefaultGuestDestination
}

type Profile struct {
 Name string `yaml:"name"`
 OS string `yaml:"os,omitempty"`
 MemoryMiB int `yaml:"memory_mib"`
 VCPUs int `yaml:"vcpus"`
 DiskGB int `yaml:"disk_gb"`
 Version string `yaml:"version"`
 Image string `yaml:"image"`
 ISO string `yaml:"iso,omitempty"`
 // BootMode is "direct" for installed disks without Cloud-Init/managed SSH.
 // Empty keeps the legacy Cloud-Init boot contract.
 BootMode string `yaml:"boot_mode,omitempty"`
 Workspace Workspace `yaml:"workspace,omitempty"`
 Guest Guest `yaml:"guest,omitempty"`
 SSH SSH `yaml:"ssh,omitempty"`
 Directory string `yaml:"-"`
}

func (p *Profile) Validate()error{
 if err:=storage.ValidateName(p.Name);err!=nil{return fmt.Errorf("profile.name: %w",err)}
 if p.BootMode!="" && p.BootMode!="cloud-init" && p.BootMode!="direct" {return fmt.Errorf("unsupported profile.boot_mode %q",p.BootMode)}
 if p.MemoryMiB<128 || p.MemoryMiB>1024*1024 {return errors.New("memory_mib must be between 128 and 1048576")}
 if p.VCPUs<1 || p.VCPUs>256 {return errors.New("vcpus must be between 1 and 256")}
 if p.DiskGB<1 {return errors.New("disk_gb must be positive")}
 if dest:=p.Workspace.Destination;dest!="" && (!path.IsAbs(dest)||path.Clean(dest)=="/"||strings.ContainsAny(dest,"\r\n\x00")){
  return errors.New("workspace.destination must be an absolute guest path other than /")
 }
 if err:=ValidateGuestDestination(p.Guest.Destination);err!=nil{return fmt.Errorf("profile.%w",err)}
 if err:=storage.ValidateName(p.Version);err!=nil{return fmt.Errorf("profile.version: %w",err)}
 if strings.TrimSpace(p.Image)==""||filepath.IsAbs(p.Image){return errors.New("profile.image must be a relative path inside the profile directory")}
 if strings.ContainsAny(p.Image,"\r\n\x00") {return errors.New("profile.image includes unsafe characters")}
 if p.Directory!=""{
  image:=filepath.Clean(filepath.Join(p.Directory,p.Image))
  rel,err:=filepath.Rel(p.Directory,image)
  if err!=nil||rel==".."||strings.HasPrefix(rel,".."+string(filepath.Separator)) {
   return errors.New("profile.image must stay inside the profile directory")
  }
 }
 return nil
}

func Read(root,name string)(*Profile,error){
 if err:=storage.ValidateName(name);err!=nil{return nil,err}
 dir:=filepath.Join(root,name)
 filename:=filepath.Join(dir,"profile.yaml")
 if stat,err:=os.Lstat(filename);err!=nil{return nil,err}else if !stat.Mode().IsRegular(){
  return nil,errors.New("profile.yaml must be a regular file")
 }
 file,err:=os.Open(filename);if err!=nil{return nil,err}
 defer file.Close()
 decoder:=yaml.NewDecoder(file)
 decoder.KnownFields(true)
 var p Profile
 if err:=decoder.Decode(&p);err!=nil{return nil,fmt.Errorf("read profile %s: %w",name,err)}
 var extra interface{}
 if err:=decoder.Decode(&extra);!errors.Is(err,io.EOF){
  if err==nil{return nil,errors.New("only one YAML document is allowed")}
  return nil,err
 }
 p.Directory=dir
 if p.Name!=name{return nil,fmt.Errorf("profile name %q does not match directory %q",p.Name,name)}
 if err:=p.Validate();err!=nil{return nil,err}
 return &p,nil
}

// ImagePath checks the complete resolved path, not only the final file,
// preventing symlink traversal outside the configured profile directory.
func (p *Profile) ImagePath()(string,error){
 if err:=p.Validate();err!=nil{return "",err}
 if p.Directory==""{return "",errors.New("profile directory is unset")}
 base,err:=filepath.EvalSymlinks(p.Directory)
 if err!=nil{return "",err}
 path:=filepath.Join(p.Directory,p.Image)
 resolved,err:=filepath.EvalSymlinks(path)
 if err!=nil{return "",err}
 rel,err:=filepath.Rel(base,resolved)
 if err!=nil||rel==".."||strings.HasPrefix(rel,".."+string(filepath.Separator)){
  return "",errors.New("image symlink escapes the profile directory")
 }
 stat,err:=os.Stat(resolved)
 if err!=nil{return "",err}
 if !stat.Mode().IsRegular(){return "",errors.New("image must be a regular file")}
 f,err:=os.Open(resolved);if err!=nil{return "",err}
 defer f.Close()
 var magic [4]byte
 if _,err:=io.ReadFull(f,magic[:]);err!=nil{return "",err}
 if magic!=[4]byte{'Q','F','I',0xfb}{
  return "",errors.New("profile image must be a QCOW2 image")
 }
 return resolved,nil
}

func List(root string)([]string,error){
 entries,err:=os.ReadDir(root)
 if errors.Is(err,os.ErrNotExist){return []string{},nil}
 if err!=nil{return nil,err}
 names:=make([]string,0,len(entries))
 for _,entry:=range entries {
  if !entry.IsDir()||storage.ValidateName(entry.Name())!=nil{continue}
  p,err:=Read(root,entry.Name())
  if err==nil && p!=nil{names=append(names,entry.Name());continue}
  if installer,installErr:=ReadInstaller(root,entry.Name());installErr==nil&&installer!=nil{
   names=append(names,entry.Name())
  }
 }
 sort.Strings(names)
 return names,nil
}

// Register copies an existing, fully installed QCOW2 to a new managed
// version. It does not install an operating system from ISO.
func Register(root string,p Profile,source string)error{
 if err:=storage.ValidateName(p.Name);err!=nil{return err}
 if p.Version==""{p.Version="v001"}
 if p.Image==""{p.Image="versions/"+p.Version+"/base.qcow2"}
 if p.Guest.Destination=="" && p.Workspace.Destination=="" {p.Guest.Destination=p.GuestDestination("")}
 p.Directory=filepath.Join(root,p.Name)
 if err:=p.Validate();err!=nil{return err}
 sourceAbs,err:=filepath.Abs(source);if err!=nil{return err}
 f,err:=os.Open(sourceAbs);if err!=nil{return err}
 defer f.Close()
 var magic [4]byte
 if _,err:=io.ReadFull(f,magic[:]);err!=nil{return err}
 if magic!=[4]byte{'Q','F','I',0xfb}{return errors.New("source is not QCOW2")}
 if _,err:=f.Seek(0,io.SeekStart);err!=nil{return err}
 if err:=os.MkdirAll(root,0700);err!=nil{return err}
 dir:=p.Directory
 if err:=os.Mkdir(dir,0700);err!=nil{return fmt.Errorf("profile already exists or cannot be created: %w",err)}
 success:=false
 defer func(){if !success{_ = os.RemoveAll(dir)}}()
 image:=filepath.Join(dir,p.Image)
 if err:=os.MkdirAll(filepath.Dir(image),0700);err!=nil{return err}
 target,err:=os.OpenFile(image,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err!=nil{return err}
 _,copyErr:=io.Copy(target,f)
 closeErr:=target.Close()
 if err:=errors.Join(copyErr,closeErr);err!=nil{return err}
 encoded,err:=yaml.Marshal(&p);if err!=nil{return err}
 if err:=os.WriteFile(filepath.Join(dir,"profile.yaml"),encoded,0600);err!=nil{return err}
 success=true
 return nil
}
