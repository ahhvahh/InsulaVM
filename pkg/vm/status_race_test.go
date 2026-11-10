package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// A transient domain vanishes between two virsh calls. Re-querying the
// authoritative UUID list is required before accepting graceful stop.
func TestInspectRegisteredTransientDomainDisappears(t *testing.T) {
 marker:=filepath.Join(t.TempDir(),"listed")
 t.Setenv("INSULA_RACE_MARKER",marker)
 t.Setenv("INSULA_RACE_UUID",runningID)
 fakeVirshForStatus(t,`case "$3" in
 list)
  if [ ! -e "$INSULA_RACE_MARKER" ]; then
   touch "$INSULA_RACE_MARKER"
   printf '%s\n' "$INSULA_RACE_UUID"
  fi ;;
 domstate) echo 'error: failed to get domain' >&2; exit 1 ;;
 *) exit 10 ;;
esac`)
 out,err:=InspectRegistered(context.Background(),"qemu:///system",[]registry.Entry{
  {ID:runningID,LibvirtUUID:runningID,Status:"running"},
 })
 if err!=nil||len(out)!=1||out[0].LibvirtState!="absent"{
  t.Fatalf("disappeared transient domain must be absent: %+v %v",out,err)
 }
}

func TestInspectRegisteredPreservesUnknownWhenDomainStillListed(t *testing.T){
 t.Setenv("INSULA_RACE_UUID",runningID)
 fakeVirshForStatus(t,`case "$3" in
 list) printf '%s\n' "$INSULA_RACE_UUID" ;;
 domstate) echo 'error: failed to get domain' >&2; exit 1 ;;
 *) exit 10 ;;
esac`)
 out,err:=InspectRegistered(context.Background(),"qemu:///system",[]registry.Entry{{LibvirtUUID:runningID,Status:"running"}})
 if err!=nil||len(out)!=1||out[0].LibvirtState!="unknown"||!strings.Contains(out[0].Warning,"domstate"){
  t.Fatalf("domain still listed cannot be considered stopped: %+v %v",out,err)
 }
}

func TestInspectRegisteredPreservesUnknownWhenRecheckFails(t *testing.T){
 marker:=filepath.Join(t.TempDir(),"listed")
 t.Setenv("INSULA_RACE_MARKER",marker)
 t.Setenv("INSULA_RACE_UUID",runningID)
 fakeVirshForStatus(t,`case "$3" in
 list)
  if [ -e "$INSULA_RACE_MARKER" ]; then
   echo 'connection lost' >&2; exit 12
  fi
  touch "$INSULA_RACE_MARKER"
  printf '%s\n' "$INSULA_RACE_UUID" ;;
 domstate) echo 'error: failed to get domain' >&2; exit 1 ;;
 *) exit 10 ;;
esac`)
 out,err:=InspectRegistered(context.Background(),"qemu:///system",[]registry.Entry{{LibvirtUUID:runningID}})
 if err!=nil||len(out)!=1||out[0].LibvirtState!="unknown"||!strings.Contains(out[0].Warning,"connection lost"){
  t.Fatalf("failed Libvirt recheck cannot prove absence: %+v %v",out,err)
 }
}

// Real policy adapter: ACPI shutdown returns before the transient domain
// is removed, and the next domstate races with domain removal.
func TestShutdownWithModeHandlesTransientDomainRemoval(t *testing.T){
 shutdown:=filepath.Join(t.TempDir(),"shutdown")
 raced:=filepath.Join(t.TempDir(),"raced")
 t.Setenv("INSULA_RACE_UUID",runningID)
 t.Setenv("INSULA_SHUTDOWN_MARKER",shutdown)
 t.Setenv("INSULA_RACE_MARKER",raced)
 fakeVirshForStatus(t,`case "$3" in
 list)
  if [ -e "$INSULA_SHUTDOWN_MARKER" ]; then
   if [ -e "$INSULA_RACE_MARKER" ]; then exit 0; fi
   touch "$INSULA_RACE_MARKER"
  fi
  printf '%s\n' "$INSULA_RACE_UUID" ;;
 domstate)
  if [ -e "$INSULA_SHUTDOWN_MARKER" ]; then
   echo 'transient domain no longer exists' >&2; exit 1
  fi
  echo running ;;
 shutdown) touch "$INSULA_SHUTDOWN_MARKER" ;;
 *) exit 10 ;;
esac`)
 if err:=ShutdownWithMode(context.Background(),"qemu:///system",runningID,time.Second,"acpi");err!=nil{
  t.Fatalf("graceful shutdown race rejected: %v",err)
 }
 if _,err:=os.Stat(shutdown);err!=nil{t.Fatalf("ACPI shutdown not requested: %v",err)}
}
