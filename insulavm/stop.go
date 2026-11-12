package insulavm

import (
 "context"
 "errors"
 "time"

 "github.com/ahhvahh/InsulaVM/internal/vmstop"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// VMStopOptions describes graceful shutdown, with an optional forced fallback.
// This remains a named insulavm type, retaining its Go reflection identity.
type VMStopOptions vmstop.StopOptions
// VMStopResult describes the shutdown outcome.
type VMStopResult vmstop.StopResult
// VMStopAllOptions configures stopping registered VMs, with dry-run and progress.
type VMStopAllOptions struct {
 DryRun bool
 OnProgress func(VMStopAllProgress)
}
// VMStopAllProgress describes a managed shutdown progress event.
type VMStopAllProgress vmstop.StopAllProgress
// VMStopAllResult contains successes, skipped VMs and failures.
type VMStopAllResult vmstop.StopAllResult

// stopAdapter prevents exposing shutdown backend methods on the public Engine.
type stopAdapter struct { *Engine }

func (a stopAdapter) GracefulShutdown(ctx context.Context,id string,limit time.Duration)error {
 return vm.ShutdownWithMode(ctx,a.config.Virtualization.LibvirtURI,id,limit,"acpi")
}

// StopVMWithOptions stops a managed VM using graceful shutdown with an explicit force policy.
// The legacy StopVM remains immediate for compatibility.
func (e *Engine) StopVMWithOptions(ctx context.Context,opts VMStopOptions)(*VMStopResult,error) {
 if e==nil{return nil,errors.New("engine is required")}
 result,err:=vmstop.StopWithPolicy(ctx,stopAdapter{e},vmstop.StopOptions(opts))
 if result==nil{return nil,err}
 public:=VMStopResult(*result)
 return &public,err
}

// StopAllVMs stops only VMs owned by this Engine registry, never arbitrary Libvirt domains.
func (e *Engine) StopAllVMs(ctx context.Context,opts VMStopAllOptions)(*VMStopAllResult,error) {
 if e==nil{return nil,errors.New("engine is required")}
 internal:=vmstop.StopAllOptions{DryRun:opts.DryRun}
 if opts.OnProgress!=nil{
  internal.OnProgress=func(p vmstop.StopAllProgress){opts.OnProgress(VMStopAllProgress(p))}
 }
 result,err:=vmstop.StopAllManagedVMs(ctx,e,internal)
 if result==nil{return nil,err}
 public:=VMStopAllResult(*result)
 return &public,err
}
