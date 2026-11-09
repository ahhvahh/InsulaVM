package insulavm

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// VMStopOptions requests an orderly guest shutdown before forced cleanup.
// GracePeriod defaults to 30 seconds; ForceAfterTimeout allows hard power-off
// only when the grace deadline expires (never on unrelated Libvirt errors).
type VMStopOptions struct {
    VMID string
    GracePeriod time.Duration
    ForceAfterTimeout bool
}

type VMStopResult struct {
    VMID string
    Graceful bool
    Forced bool
    TimedOut bool
}

type stopPolicyBackend interface {
    GetVMStatus(context.Context,string)(VMLiveInfo,error)
    gracefulShutdown(context.Context,string,time.Duration)error
    StopVM(context.Context,string)error
}

func(e *Engine)gracefulShutdown(ctx context.Context,id string,limit time.Duration)error {
    return vm.ShutdownWithMode(ctx,e.config.Virtualization.LibvirtURI,id,limit,"acpi")
}

// StopVMWithOptions performs graceful shutdown with an explicit escalation
// policy. Legacy StopVM remains immediate for API compatibility.
func(e *Engine)StopVMWithOptions(ctx context.Context,opts VMStopOptions)(*VMStopResult,error) {
    if e==nil{return nil,errors.New("engine is required")}
    return stopWithPolicy(ctx,e,opts)
}

func stopWithPolicy(ctx context.Context,backend stopPolicyBackend,opts VMStopOptions)(*VMStopResult,error) {
    if ctx==nil{return nil,errors.New("context is required")}
    if backend==nil{return nil,errors.New("backend is required")}
    if opts.VMID==""{return nil,errors.New("VMID is required")}
    if opts.GracePeriod<0{return nil,errors.New("GracePeriod must not be negative")}
    if opts.GracePeriod==0{opts.GracePeriod=30*time.Second}
    if err:=ctx.Err();err!=nil{return nil,err}
    result:=&VMStopResult{VMID:opts.VMID}
    entry,err:=backend.GetVMStatus(ctx,opts.VMID)
    if err!=nil{return result,err}
    if entry.Record.ID!=opts.VMID||entry.Record.LibvirtUUID!=opts.VMID{
        return result,errors.New("VM identity mismatch in registry/Libvirt")
    }
    if entry.LibvirtState=="unknown"||entry.LibvirtState==""{
        return result,errors.New("Libvirt VM state is unknown; refusing shutdown")
    }
    if entry.LibvirtState=="absent" {
        result.Graceful=true
        return result,backend.StopVM(ctx,opts.VMID)
    }
    // Only an active guest can receive ACPI shutdown. Other states must be
    // handled explicitly; never force a foreign or indeterminate domain.
    if !entry.IsRunning(){
        return result,fmt.Errorf("VM state %q cannot be shut down gracefully",entry.LibvirtState)
    }
    err=backend.gracefulShutdown(ctx,opts.VMID,opts.GracePeriod)
    if err==nil{
        result.Graceful=true
        return result,backend.StopVM(ctx,opts.VMID)
    }
    if ctx.Err()!=nil{return result,ctx.Err()}
    if !errors.Is(err,context.DeadlineExceeded) || !opts.ForceAfterTimeout{
        return result,fmt.Errorf("graceful shutdown failed; VM preserved: %w",err)
    }
    result.TimedOut=true
    // StopVM rechecks the instance ownership and actual Libvirt state,
    // then calls virsh destroy only for the registered domain.
    if cleanupErr:=backend.StopVM(ctx,opts.VMID);cleanupErr!=nil{
        return result,fmt.Errorf("force after grace deadline failed: %w",cleanupErr)
    }
    result.Forced=true
    return result,nil
}
