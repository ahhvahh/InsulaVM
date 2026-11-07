package insulavm

import (
 "context"
 "errors"
 "fmt"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// VMStartOptions creates an independent VM from a previously registered profile.
// Zero values for MemoryMiB and VCPUs keep the profile defaults.
// The on-disk profile and shared QCOW2 base image are never modified.
//
// StopOnContextDone is intended for long-running Go processes. When enabled,
// canceling ctx starts best-effort cleanup in a goroutine. Use StopVM explicitly
// for synchronous cleanup and to observe its error. The CLI always uses manual
// cleanup, so its process exiting never stops a freshly created VM.
type VMStartOptions struct {
 Profile string
 MemoryMiB int
 VCPUs int
 StopOnContextDone bool
 OnStopError func(error)
}

// resolveVMStartProfile copies the profile so overrides are per-instance only.
func(e *Engine)resolveVMStartProfile(opts VMStartOptions)(*Profile,error){
 if e==nil{return nil,errors.New("engine is required")}
 if opts.Profile==""{return nil,errors.New("VM start profile is required")}
 if opts.MemoryMiB<0||opts.VCPUs<0{
  return nil,errors.New("MemoryMiB and VCPUs must be >= 0 (zero uses profile default)")
 }
 source,err:=e.GetProfile(opts.Profile)
 if err!=nil{return nil,fmt.Errorf("load VM profile %s: %w",opts.Profile,err)}
 profileCopy:=*source
 if opts.MemoryMiB>0{profileCopy.MemoryMiB=opts.MemoryMiB}
 if opts.VCPUs>0{profileCopy.VCPUs=opts.VCPUs}
 if err:=profileCopy.Validate();err!=nil{
  return nil,fmt.Errorf("invalid VM instance options: %w",err)
 }
 return &profileCopy,nil
}

// StartVMWithOptions creates a VM from an existing profile, optionally
// overriding its RAM/vCPUs and arranging best-effort context-based cleanup.
// Use a cancelable context when StopOnContextDone is enabled.
func(e *Engine)StartVMWithOptions(ctx context.Context,opts VMStartOptions,onProgress func(VMProgress))(*vm.Instance,error){
 if ctx==nil{return nil,errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 if opts.StopOnContextDone&&ctx.Done()==nil{
  return nil,errors.New("StopOnContextDone requires a cancelable context")
 }
 p,err:=e.resolveVMStartProfile(opts)
 if err!=nil{return nil,err}
 if err:=e.config.EnsureDirectories();err!=nil{return nil,err}
 instance,err:=vm.StartWithProgress(ctx,&e.config,p,onProgress)
 if err!=nil{return nil,err}
 if opts.StopOnContextDone {
  // Do not use ctx after cancellation; cleanup needs its own timeout.
  go func(id string){
   <-ctx.Done()
   cleanupCtx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
   defer cancel()
   if err:=e.StopVM(cleanupCtx,id);err!=nil&&opts.OnStopError!=nil{
    opts.OnStopError(err)
   }
  }(instance.ID)
 }
 return instance,nil
}
