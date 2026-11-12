package insulavm

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"

 "github.com/ahhvahh/InsulaVM/pkg/profile"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
 "github.com/ahhvahh/InsulaVM/pkg/storage"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// ResumeVM restarts the same transient VM and preserves its UUID and writable
// QCOW2. It requires the original domain.xml and disk to still be present.
func(e *Engine)ResumeVM(ctx context.Context,id string)(*VMInfo,error){
 if e==nil{return nil,errors.New("engine is required")}
 entry,err:=vm.ResumeByID(ctx,&e.config,id)
 if err!=nil{return nil,err}
 return &entry,nil
}

// DeleteVM permanently removes a managed VM and its SQLite/shell history.
// Unlike StopVM, the VM no longer appears in vm list. Log files are kept.
func(e *Engine)DeleteVM(ctx context.Context,id string)error{
 if e==nil{return errors.New("engine is required")}
 if ctx==nil{return errors.New("context is required")}
 if id==""{return errors.New("VM ID is required")}
 if err:=e.StopVM(ctx,id);err!=nil{return err}
 live,err:=e.GetVMStatus(ctx,id)
 if err!=nil{return err}
 if live.LibvirtState!="absent"{
  return fmt.Errorf("refusing registry purge: Libvirt still reports %s",live.LibvirtState)
 }
 if _,err:=os.Lstat(live.Record.InstanceDir);err==nil{
  return errors.New("refusing registry purge: VM temporary directory still exists")
 }else if !errors.Is(err,os.ErrNotExist){return err}
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return err}
 defer store.Close()
 return store.Delete(ctx,id)
}

// DeleteProfile removes one named image profile only when no registered VM
// refers to it (including stopped VMs whose overlay must be recoverable).
// Users must delete VM records explicitly before deleting their shared base.
func(e *Engine)DeleteProfile(ctx context.Context,name string)error{
 if e==nil{return errors.New("engine is required")}
 if ctx==nil{return errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return err}
 if err:=storage.ValidateName(name);err!=nil{return err}
 entries,err:=e.ListVMStatus(ctx)
 if err!=nil{return err}
 for _,entry:=range entries {
  if entry.Record.Profile==name{
   return fmt.Errorf("profile %q is referenced by VM %s; delete the VM first",name,entry.Record.ID)
  }
 }
 path:=filepath.Join(e.config.Storage.Profiles,name)
 info,err:=os.Lstat(path)
 if err!=nil{return err}
 if !info.IsDir(){return errors.New("profile directory must be a real directory")}
 // Only remove profiles recognized by the managed catalog.
 if _,err:=e.GetProfile(name);err!=nil{
  // Installer profiles deliberately use a different metadata format.
  if _,installErr:=profile.ReadInstaller(e.config.Storage.Profiles,name);installErr!=nil{
   return fmt.Errorf("refusing to remove unrecognized profile %q: %w",name,err)
  }
 }
 return os.RemoveAll(path)
}
