package insulavm

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
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
 // StartupScriptPath is an optional absolute path to a trusted host script.
 // InsulaVM embeds it in the first-boot Cloud-Init seed as root. This is NOT
 // a path to a script supplied by an untrusted repository or workflow YAML.
 StartupScriptPath string
 // SSHReadyTimeout is independent of DHCP; zero defaults to five minutes.
 SSHReadyTimeout time.Duration
 // PreserveOnFailure keeps a failed VM, overlay and generated SSH keys for debugging.
 // The caller must explicitly stop it with StopVM after investigation.
 PreserveOnFailure bool
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
 if opts.SSHReadyTimeout<0||opts.SSHReadyTimeout>time.Hour{
  return nil,errors.New("SSHReadyTimeout must be 0..1h")
 }
 if opts.StopOnContextDone&&ctx.Done()==nil{
  return nil,errors.New("StopOnContextDone requires a cancelable context")
 }
 p,err:=e.resolveVMStartProfile(opts)
 if err!=nil{return nil,err}
 var startupScript []byte
 if opts.StartupScriptPath!=""{
  startupScript,err=readVMStartupScript(opts.StartupScriptPath)
  if err!=nil{return nil,err}
 }
 if err:=e.config.EnsureDirectories();err!=nil{return nil,err}
 instance,err:=vm.StartWithStartupOptions(ctx,&e.config,p,startupScript,vm.StartupOptions{SSHReadyTimeout:opts.SSHReadyTimeout,PreserveOnFailure:opts.PreserveOnFailure},onProgress)
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

// readVMStartupScript refuses symlinks, relative paths, binary payloads and
// scripts too large to fit safely in a NoCloud user-data seed.
func readVMStartupScript(p string)([]byte,error){
 if !filepath.IsAbs(p){return nil,errors.New("StartupScriptPath must be absolute")}
 info,err:=os.Lstat(p)
 if err!=nil{return nil,err}
 if !info.Mode().IsRegular(){return nil,errors.New("startup script must be a regular host file")}
 if info.Size()==0||info.Size()>256*1024{
  return nil,errors.New("startup script must be 1..262144 bytes")
 }
 content,err:=os.ReadFile(p)
 if err!=nil{return nil,err}
 if len(content)==0||len(content)>256*1024||strings.ContainsRune(string(content),0){
  return nil,errors.New("startup script must be text without NUL (maximum 256 KiB)")
 }
 return content,nil
}
