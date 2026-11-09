package insulavm

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

type LiveISOStartOptions struct {
 Profile string
 ISOPath string
 MemoryMiB int
 VCPUs int
 StartupScriptPath string
 SSHReadyTimeout time.Duration
 PreserveOnFailure bool
}

// StartVMFromISO starts a Cloud-Init-enabled Debian Live ISO; it does not
// install an operating system or run an arbitrary netinst installer ISO.
func(e *Engine)StartVMFromISO(ctx context.Context,opts LiveISOStartOptions)(*vm.Instance,error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if opts.Profile==""{return nil,errors.New("profile name is required")}
 p,err:=e.GetProfile(opts.Profile)
 if err!=nil{return nil,err}
 if opts.ISOPath==""{
  opts.ISOPath=filepath.Join(p.Directory,"exports",p.Name+"-live.iso")
 }
 if _,err:=os.Stat(opts.ISOPath);err!=nil{return nil,fmt.Errorf("Live ISO unavailable: %w",err)}
 if opts.MemoryMiB==0{opts.MemoryMiB=p.MemoryMiB}
 if opts.VCPUs==0{opts.VCPUs=p.VCPUs}
 var script []byte
 if opts.StartupScriptPath!=""{
  script,err=readVMStartupScript(opts.StartupScriptPath)
  if err!=nil{return nil,err}
 }
 if err:=e.config.EnsureDirectories();err!=nil{return nil,err}
 return vm.StartLiveISOWithOptions(ctx,&e.config,opts.Profile,opts.ISOPath,opts.MemoryMiB,opts.VCPUs,script,vm.LiveISOStartupOptions{
  ReadinessTimeout:opts.SSHReadyTimeout,PreserveOnFailure:opts.PreserveOnFailure,
 })
}
