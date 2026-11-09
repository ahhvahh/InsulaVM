package vm

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// ShutdownForFreeze requests an orderly guest shutdown and waits for its
// transient Libvirt domain to disappear. The overlay is NOT removed.
// Never convert an overlay while Libvirt still reports its domain present.
func ShutdownForFreeze(ctx context.Context, uri, uuid string, timeout time.Duration) error {
 return shutdownWithMode(ctx,uri,uuid,timeout,"")
}

// ShutdownWithMode constrains the shutdown method. The stop-with-force
// policy uses ACPI explicitly to avoid switching to QEMU guest-agent shutdown
// when testing a guest that must ignore ACPI power events.
func ShutdownWithMode(ctx context.Context,uri,uuid string,timeout time.Duration,mode string) error {
 if mode!="" && mode!="acpi"{return errors.New("unsupported shutdown mode")}
 return shutdownWithMode(ctx,uri,uuid,timeout,mode)
}

func shutdownWithMode(ctx context.Context, uri, uuid string, timeout time.Duration,mode string) error {
 if ctx == nil { return errors.New("context is required") }
 if uuid == "" { return errors.New("VM UUID is required") }
 if timeout <= 0 { return errors.New("shutdown timeout must be positive") }
 infos, err := InspectRegistered(ctx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
 if err != nil { return err }
 if len(infos)!=1 { return errors.New("Libvirt returned an invalid VM status result") }
 if !infos[0].IsRunning() {
  return fmt.Errorf("VM %s must be running for a safe freeze (state: %s)",uuid,infos[0].LibvirtState)
 }
 stopCtx,cancel:=context.WithTimeout(ctx,timeout)
 defer cancel()
 shutdownArgs:=[]string{"shutdown",uuid}
 if mode!=""{shutdownArgs=append(shutdownArgs,"--mode",mode)}
 output,err := virsh(stopCtx,uri,shutdownArgs...)
 if err!=nil {
  return fmt.Errorf("request graceful VM shutdown: %w: %s",err,strings.TrimSpace(string(output)))
 }
 ticker:=time.NewTicker(time.Second)
 defer ticker.Stop()
 for {
  infos,err=InspectRegistered(stopCtx,uri,[]registry.Entry{{LibvirtUUID:uuid}})
  if err!=nil { return fmt.Errorf("verify VM shutdown: %w",err) }
  if len(infos)!=1 { return errors.New("missing Libvirt status during shutdown") }
  if infos[0].LibvirtState=="absent" { return nil }
  if infos[0].LibvirtState=="unknown" {
   return fmt.Errorf("VM %s state unknown during shutdown: %s",uuid,infos[0].Warning)
  }
  select {
  case <-stopCtx.Done():
   return fmt.Errorf("VM %s did not shut down within %s; source overlay preserved: %w",uuid,timeout,stopCtx.Err())
  case <-ticker.C:
  }
 }
}
