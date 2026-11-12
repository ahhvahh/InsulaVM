package insulavm

import (
 "context"
 "errors"
 "time"

 "github.com/ahhvahh/InsulaVM/internal/vmstop"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// VMStopOptions describes graceful shutdown, with an optional forced fallback.
type VMStopOptions = vmstop.StopOptions
// VMStopResult describes the shutdown outcome.
type VMStopResult = vmstop.StopResult
// VMStopAllOptions configures stopping registered VMs, with dry-run and progress.
type VMStopAllOptions = vmstop.StopAllOptions
// VMStopAllProgress describes a managed shutdown progress event.
type VMStopAllProgress = vmstop.StopAllProgress
// VMStopAllResult contains successes, skipped VMs and failures.
type VMStopAllResult = vmstop.StopAllResult

// stopAdapter prevents exposing shutdown backend methods on the public Engine.
type stopAdapter struct { *Engine }

func (a stopAdapter) GracefulShutdown(ctx context.Context,id string,limit time.Duration)error {
 return vm.ShutdownWithMode(ctx,a.config.Virtualization.LibvirtURI,id,limit,"acpi")
}

// StopVMWithOptions stops a managed VM using graceful shutdown with an explicit force policy.
// The legacy StopVM remains immediate for compatibility.
func (e *Engine) StopVMWithOptions(ctx context.Context,opts VMStopOptions)(*VMStopResult,error) {
 if e==nil{return nil,errors.New("engine is required")}
 return vmstop.StopWithPolicy(ctx,stopAdapter{e},opts)
}

// StopAllVMs stops only VMs owned by this Engine registry, never arbitrary Libvirt domains.
func (e *Engine) StopAllVMs(ctx context.Context,opts VMStopAllOptions)(*VMStopAllResult,error) {
 if e==nil{return nil,errors.New("engine is required")}
 return vmstop.StopAllManagedVMs(ctx,e,opts)
}
