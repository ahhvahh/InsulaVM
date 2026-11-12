package insulavm

import (
 "context"
 "errors"
 "fmt"
 "path/filepath"

 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// StartVMFromInstallerISO starts the ISO declared by a saved installer
// profile. It returns a registered instance immediately after Libvirt enters
// running state: serial login, DHCP, SSH and OS installation are not assumed.
// Relative disk.start_iso paths are based on config.yaml's directory.
func(e *Engine)StartVMFromInstallerISO(ctx context.Context,name string,callback func(VMProgress))(*vm.Instance,error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if name==""{return nil,errors.New("installer profile name is required")}
 p,err:=profile.ReadInstaller(e.config.Storage.Profiles,name)
 if err!=nil{return nil,fmt.Errorf("load installer profile: %w",err)}
 iso:=p.Disk.StartISO
 if !filepath.IsAbs(iso){
  iso=filepath.Join(e.configDir,iso)
 }
 iso,err=filepath.Abs(iso)
 if err!=nil{return nil,fmt.Errorf("resolve ISO path: %w",err)}
 return vm.StartInstallerISO(ctx,&e.config,vm.InstallerISOOptions{
  ISOPath:iso,Profile:p,PreserveOnFailure:true,
 },callback)
}
