package insulavm

import (
 "context"
 "bytes"
 "errors"
 "fmt"
 "strings"
 "time"
)

// VMExportISORequest freezes a running managed VM and exports its Debian Live
// ISO in one operation. The source VM is stopped and removed by FreezeVM.
type VMExportISORequest struct {
 VMID string
 Name string
 PrepareLiveBoot bool
 ForceShutdown bool
 SkipGuestCleanup bool
 AllowStopped bool
 ShutdownTimeout time.Duration
 CommandTimeout time.Duration
 OnProgress func(VMFreezeProgress)
}
type VMExportISOResult struct {
 Freeze *VMFreezeResult
 ISOPath string
}

// ExportVMToLiveISO is the Go-library equivalent of "insula vm export-iso".
// It checks/installs live-boot BEFORE freezing the VM. When ISO export fails,
// the named frozen profile survives, allowing an explicit retry via
// ExportProfileLiveISO without destroying another VM.
func(e *Engine)ExportVMToLiveISO(ctx context.Context,req VMExportISORequest)(*VMExportISOResult,error){
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return nil,err}
 if strings.TrimSpace(req.VMID)==""||strings.TrimSpace(req.Name)==""{
  return nil,errors.New("VMID and frozen profile name are required")
 }
 if req.CommandTimeout<0{return nil,errors.New("command timeout must not be negative")}
 if req.CommandTimeout==0{req.CommandTimeout=30*time.Minute}
 if req.PrepareLiveBoot{
  if req.SkipGuestCleanup||req.AllowStopped{
   return nil,errors.New("cannot prepare live-boot via SSH in recovery mode; export existing QCOW2 then prepare a new VM separately")
  }
  // Avoid apt-get update when the Live boot scripts are already installed.
  var output bytes.Buffer
  cmd:="if test -f /usr/share/initramfs-tools/scripts/live; then update-initramfs -u; else DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends live-boot live-boot-initramfs-tools && update-initramfs -u; fi; test -f /usr/share/initramfs-tools/scripts/live"
  run,err:=e.ExecVM(ctx,ExecRequest{
   VMID:req.VMID,Command:cmd,Sudo:true,CommandTimeout:req.CommandTimeout,Output:&output,
  })
  if err!=nil{
   logHint:=""
   if run!=nil{logHint=run.LogDirectory}
   return nil,fmt.Errorf("prepare guest live-boot failed before freeze (VM remains intact); logs=%s: %w: %s",logHint,err,output.String())
  }
 }
 freeze,err:=e.FreezeVM(ctx,VMFreezeRequest{
  VMID:req.VMID,Name:req.Name,ShutdownTimeout:req.ShutdownTimeout,
  ForceShutdown:req.ForceShutdown,SkipGuestCleanup:req.SkipGuestCleanup,
  AllowStopped:req.AllowStopped,ExportISO:true,OnProgress:req.OnProgress,
 })
 result:=&VMExportISOResult{Freeze:freeze}
 if freeze!=nil{result.ISOPath=freeze.ISOPath}
 return result,err
}
