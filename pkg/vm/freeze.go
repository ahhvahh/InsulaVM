package vm

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// ShutdownForFreeze never destroys a guest. Kept as the safe public API.
func ShutdownForFreeze(ctx context.Context,uri,uuid string,timeout time.Duration) error {
 _,err:=ShutdownForFreezeWithOptions(ctx,uri,uuid,timeout,false)
 return err
}

// ShutdownForFreezeWithOptions keeps the legacy graceful shutdown behavior.
// An inactive defined domain (shut off) is as safe for disk conversion as
// a transient domain absent from Libvirt.
func ShutdownForFreezeWithOptions(ctx context.Context,uri,uuid string,timeout time.Duration,force bool)(bool,error){
 return shutdownForFreezeWithOptions(ctx,uri,uuid,timeout,force,false,nil)
}

// ShutdownInstallerForFreeze first requests an in-guest poweroff through the
// QEMU Guest Agent. If unavailable, it falls back to the native ACPI request.
// Completion is ALWAYS proven by Libvirt state, never by command exit status.
// It never destroys the VM unless force=true was explicitly requested.
func ShutdownInstallerForFreeze(ctx context.Context,uri,uuid string,timeout time.Duration,force bool,report func(string))(bool,error){
 return shutdownForFreezeWithOptions(ctx,uri,uuid,timeout,force,true,report)
}

func freezeDiskSafeState(state string)bool{
 return state=="absent"||state=="shut off"
}

func shutdownForFreezeWithOptions(ctx context.Context,uri,uuid string,timeout time.Duration,force,preferAgent bool,report func(string))(bool,error){
 if ctx==nil{return false,errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return false,err}
 if uuid==""{return false,errors.New("VM UUID is required")}
 if timeout<=0{return false,errors.New("shutdown timeout must be positive")}
 infos,err:=InspectRegistered(ctx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
 if err!=nil{return false,err}
 if len(infos)!=1{return false,errors.New("invalid Libvirt status result")}
 if !infos[0].IsRunning(){return false,fmt.Errorf("VM %s must be running (state: %s)",uuid,infos[0].LibvirtState)}

 gracefulCtx,cancel:=context.WithTimeout(ctx,timeout)
 defer cancel()
 notify:=func(message string){if report!=nil{report(message)}}
 var output []byte
 var shutdownErr error
 agentAccepted:=false
 if preferAgent{
  // Bound a missing/stuck agent so ACPI still has most of the deadline.
  agentCtx,agentCancel:=context.WithTimeout(gracefulCtx,8*time.Second)
  output,shutdownErr=virsh(agentCtx,uri,"shutdown",uuid,"--mode","agent")
  agentCancel()
  if shutdownErr==nil{
   agentAccepted=true
   notify("QEMU Guest Agent accepted poweroff request; waiting for guest to stop")
  }else{
   notify("QEMU Guest Agent unavailable ("+strings.TrimSpace(string(output))+"); trying ACPI shutdown")
  }
 }
 if !agentAccepted{
  args:=[]string{"shutdown",uuid}
  if preferAgent{args=append(args,"--mode","acpi")}
  output,shutdownErr=virsh(gracefulCtx,uri,args...)
 }
 var reason error
 if shutdownErr!=nil{
  // The guest may have powered off between state inspection and request.
  final,checkErr:=InspectRegistered(ctx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
  if checkErr==nil&&len(final)==1&&freezeDiskSafeState(final[0].LibvirtState){
   return false,nil
  }
  reason=fmt.Errorf("request graceful VM shutdown: %w: %s",shutdownErr,strings.TrimSpace(string(output)))
 }else{
  ticker:=time.NewTicker(time.Second)
  defer ticker.Stop()
  requestedAt:=time.Now()
  acpiFallbackSent:=!agentAccepted
  for reason==nil{
   // The agent can acknowledge an async shutdown that never completes.
   // Try ACPI as a second graceful mechanism before the overall deadline.
   if preferAgent && agentAccepted && !acpiFallbackSent && time.Since(requestedAt)>=15*time.Second {
    acpiFallbackSent=true
    acpiOutput,acpiErr:=virsh(gracefulCtx,uri,"shutdown",uuid,"--mode","acpi")
    if acpiErr!=nil{
     notify("ACPI fallback unavailable ("+strings.TrimSpace(string(acpiOutput))+"); waiting for guest agent shutdown")
    }else{
     notify("QEMU Guest Agent shutdown still pending; ACPI shutdown also requested")
    }
   }
   statusErr:=func()error{
    infos,err=InspectRegistered(gracefulCtx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
    if err!=nil{return fmt.Errorf("verify graceful shutdown: %w",err)}
    if len(infos)!=1{return errors.New("invalid Libvirt status during shutdown")}
    if freezeDiskSafeState(infos[0].LibvirtState){return nil}
    if infos[0].LibvirtState=="unknown"{return fmt.Errorf("Libvirt state unknown: %s",infos[0].Warning)}
    return errors.New("still running")
   }()
   if statusErr==nil{return false,nil}
   if statusErr.Error()!="still running"{reason=statusErr;break}
   select{
   case <-gracefulCtx.Done():
    reason=fmt.Errorf("VM %s did not shut down within %s: %w",uuid,timeout,gracefulCtx.Err())
   case <-ticker.C:
   }
  }
 }
 cancel()
 if !force{return false,fmt.Errorf("%w; source overlay preserved (use explicit force only after checking VM state)",reason)}
 if err:=ctx.Err();err!=nil{return false,fmt.Errorf("graceful shutdown failed: %w; force canceled: %w",reason,err)}
 hardCtx,hardCancel:=context.WithTimeout(ctx,30*time.Second)
 defer hardCancel()
 // Avoid forcing a guest that completed shutdown just after the timeout.
 infos,err=InspectRegistered(hardCtx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
 if err!=nil{return false,err}
 if len(infos)!=1||infos[0].LibvirtState=="unknown"{return false,errors.New("cannot verify Libvirt state before forced stop")}
 if freezeDiskSafeState(infos[0].LibvirtState){return false,nil}
 if _,err:=virsh(hardCtx,uri,"destroy",uuid);err!=nil{
  return false,fmt.Errorf("force stop VM %s after failed graceful shutdown (%v): %w",uuid,reason,err)
 }
 infos,err=InspectRegistered(hardCtx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
 if err!=nil{return true,fmt.Errorf("verify forced shutdown: %w",err)}
 if len(infos)!=1||!freezeDiskSafeState(infos[0].LibvirtState){
  return true,fmt.Errorf("forced VM shutdown not confirmed; overlay preserved (Libvirt=%v)",infos)
 }
 return true,nil
}

// ShutdownWithMode constrains shutdown to ACPI when requested; does not destroy.
func ShutdownWithMode(ctx context.Context,uri,uuid string,timeout time.Duration,mode string)error{
 if mode!=""&&mode!="acpi"{return errors.New("unsupported shutdown mode")}
 return shutdownWithMode(ctx,uri,uuid,timeout,mode)
}
func shutdownWithMode(ctx context.Context,uri,uuid string,timeout time.Duration,mode string)error{
 if mode==""{return ShutdownForFreeze(ctx,uri,uuid,timeout)}
 if ctx==nil{return errors.New("context is required")}
 if timeout<=0{return errors.New("shutdown timeout must be positive")}
 infos,err:=InspectRegistered(ctx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
 if err!=nil{return err}
 if len(infos)!=1||!infos[0].IsRunning(){return errors.New("VM must be running")}
 stopCtx,cancel:=context.WithTimeout(ctx,timeout)
 defer cancel()
 output,err:=virsh(stopCtx,uri,"shutdown",uuid,"--mode",mode)
 if err!=nil{return fmt.Errorf("request graceful VM shutdown: %w: %s",err,strings.TrimSpace(string(output)))}
 ticker:=time.NewTicker(time.Second)
 defer ticker.Stop()
 for {
  infos,err=InspectRegistered(stopCtx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
  if err!=nil{return err}
  if len(infos)!=1{return errors.New("invalid Libvirt status")}
  if infos[0].LibvirtState=="absent"{return nil}
  if infos[0].LibvirtState=="unknown"{return errors.New("unknown Libvirt state")}
  select{case <-stopCtx.Done():return fmt.Errorf("VM %s did not shut down within %s; overlay preserved: %w",uuid,timeout,stopCtx.Err());case <-ticker.C:}
 }
}
