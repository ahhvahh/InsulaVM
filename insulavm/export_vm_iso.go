package insulavm

import (
 "context"
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
 cmd:="test -d /usr/share/initramfs-tools/scripts/live"
 if req.PrepareLiveBoot{
  cmd="DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends live-boot && update-initramfs -u && test -d /usr/share/initramfs-tools/scripts/live"
 }
 _,err:=e.ExecVM(ctx,ExecRequest{
  VMID:req.VMID,Command:cmd,Sudo:req.PrepareLiveBoot,CommandTimeout:req.CommandTimeout,
 })
 if err!=nil{
  if !req.PrepareLiveBoot{return nil,fmt.Errorf("guest needs live-boot before ISO export; use PrepareLiveBoot or install it manually: %w",err)}
  return nil,fmt.Errorf("prepare guest for ISO export: %w",err)
 }
 freeze,err:=e.FreezeVM(ctx,VMFreezeRequest{
  VMID:req.VMID,Name:req.Name,ShutdownTimeout:req.ShutdownTimeout,OnProgress:req.OnProgress,
 })
 if err!=nil{return &VMExportISOResult{Freeze:freeze},err}
 iso,err:=e.ExportProfileLiveISO(ctx,req.Name)
 if err!=nil{return &VMExportISOResult{Freeze:freeze},fmt.Errorf("profile saved but Live ISO export failed; retry profile export-iso -name %s: %w",req.Name,err)}
 return &VMExportISOResult{Freeze:freeze,ISOPath:iso},nil
}
