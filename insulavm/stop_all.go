package insulavm

import (
 "context"
 "errors"
 "fmt"
)

// VMStopAllOptions controls stopping instances managed by THIS InsulaVM
// registry. It never enumerates arbitrary Libvirt domains for destruction.
type VMStopAllOptions struct {
 DryRun bool
 OnProgress func(VMStopAllProgress)
}

type VMStopAllProgress struct {
 VMID string
 Stage string
 Message string
}

// VMStopAllResult provides a partial report even if one VM cannot be stopped.
// Targets includes domains found in Libvirt (including paused/shutoff).
type VMStopAllResult struct {
 Checked int
 Targets []string
 Stopped []string
 Skipped []string
 Failed []string
}

type vmStopAllBackend interface {
 ListVMStatus(context.Context)([]VMLiveInfo,error)
 StopVM(context.Context,string)error
}

// StopAllVMs safely stops all existing Libvirt domains registered in this
// engine's private SQLite database. Missing domains and foreign domains are
// never touched. Every target is checked again by StopVM before cleanup.
func(e *Engine)StopAllVMs(ctx context.Context,opts VMStopAllOptions)(*VMStopAllResult,error){
 if e==nil{return nil,errors.New("engine is required")}
 return stopAllManagedVMs(ctx,e,opts)
}

func stopAllManagedVMs(ctx context.Context,backend vmStopAllBackend,opts VMStopAllOptions)(*VMStopAllResult,error){
 if ctx==nil{return nil,errors.New("context is required")}
 if backend==nil{return nil,errors.New("stop backend is required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 live,err:=backend.ListVMStatus(ctx)
 if err!=nil{return nil,fmt.Errorf("cannot inspect managed VM status in Libvirt: %w",err)}
 result:=&VMStopAllResult{Checked:len(live)}
 report:=func(id,stage,msg string){
  if opts.OnProgress!=nil{opts.OnProgress(VMStopAllProgress{VMID:id,Stage:stage,Message:msg})}
 }
 var problems []error
 for _,entry:=range live{
  id:=entry.Record.ID
  if err:=ctx.Err();err!=nil{return result,errors.Join(append(problems,err)...)}
  // Current managed VM instances use the same UUIDv7 for registry and
  // Libvirt. Reject any inconsistent record instead of touching a foreign VM.
  if id==""||entry.Record.LibvirtUUID!=id{
   problem:=fmt.Errorf("VM %q has mismatched registry/Libvirt UUID: refusing to stop",id)
   problems=append(problems,problem)
   result.Failed=append(result.Failed,id)
   report(id,"failed",problem.Error())
   continue
  }
  switch entry.LibvirtState{
  case "absent":
   result.Skipped=append(result.Skipped,id)
   report(id,"skipped","VM not present in Libvirt")
   continue
  case "unknown","":
   problem:=fmt.Errorf("VM %s Libvirt state cannot be determined: %s",id,entry.Warning)
   problems=append(problems,problem)
   result.Failed=append(result.Failed,id)
   report(id,"failed",problem.Error())
   continue
  }
  result.Targets=append(result.Targets,id)
  if opts.DryRun{
   report(id,"planned","Registered VM exists in Libvirt: "+entry.LibvirtState)
   continue
  }
  report(id,"stopping","Stopping managed VM (Libvirt state: "+entry.LibvirtState+")")
  if err:=backend.StopVM(ctx,id);err!=nil{
   problem:=fmt.Errorf("stop managed VM %s: %w",id,err)
   problems=append(problems,problem)
   result.Failed=append(result.Failed,id)
   report(id,"failed",problem.Error())
   continue
  }
  result.Stopped=append(result.Stopped,id)
  report(id,"stopped","VM stopped and temporary storage cleaned")
 }
 return result,errors.Join(problems...)
}
