package vm

import (
 "context"
 "errors"
 "fmt"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// LiveInfo separates persisted metadata from current Libvirt state.
// Reading status never mutates the registry. A missing Libvirt domain is
// explicitly "absent", even if SQLite still says "creating" or "running".
type LiveInfo struct {
 Record registry.Entry
 LibvirtState string
 Warning string
}

func (v LiveInfo) IsRunning() bool {
 return v.LibvirtState == "running" || v.LibvirtState == "blocked"
}

// InspectRegistered queries the system Libvirt connection once to find
// registered domain UUIDs, then calls domstate only for domains that exist.
// An inaccessible Libvirt connection is an error, not a false "offline".
func InspectRegistered(ctx context.Context, uri string, records []registry.Entry) ([]LiveInfo,error) {
 if ctx == nil { return nil,errors.New("context is required") }
 if err := ctx.Err(); err != nil { return nil,err }
 if len(records)==0 {return []LiveInfo{},nil}
 output,err:=virsh(ctx,uri,"list","--all","--uuid")
 if err!=nil {return nil,fmt.Errorf("consult Libvirt domain list: %w: %s",err,strings.TrimSpace(string(output)))}
 existing:=make(map[string]bool)
 for _,line:=range strings.Split(string(output),"\n"){
  uuid:=strings.ToLower(strings.TrimSpace(line))
  if uuid!=""{existing[uuid]=true}
 }
 infos:=make([]LiveInfo,0,len(records))
 for _,record:=range records{
  info:=LiveInfo{Record:record,LibvirtState:"absent"}
  if existing[strings.ToLower(record.LibvirtUUID)]{
   state,err:=virsh(ctx,uri,"domstate",record.LibvirtUUID)
   if err!=nil{
    info.LibvirtState="unknown"
    info.Warning=fmt.Sprintf("Libvirt domstate: %v: %s",err,strings.TrimSpace(string(state)))
   }else{
    info.LibvirtState=strings.ToLower(strings.TrimSpace(string(state)))
    if info.LibvirtState==""{
     info.LibvirtState="unknown"
     info.Warning="Libvirt returned an empty state"
    }
   }
  }
  if info.Warning==""{
   switch{
   case info.IsRunning() && record.Status!="running":
    info.Warning="VM is active in Libvirt, but the saved SQLite status is "+record.Status
   case info.LibvirtState=="absent" && (record.Status=="running"||record.Status=="creating"):
    info.Warning="stale SQLite status: VM does not exist in Libvirt"
   case info.LibvirtState!="absent" && !info.IsRunning() && record.Status=="running":
    info.Warning="VM is not running in Libvirt despite SQLite status running"
   }
  }
  infos=append(infos,info)
 }
 return infos,nil
}

// ActiveDomainState verifies a freshly created domain while starting or
// waiting for DHCP/SSH. An unexpected stopped/paused/crashed state aborts
// promptly instead of hiding the failure behind a 3-minute timeout.
func ActiveDomainState(ctx context.Context,uri,uuid string) (string,error) {
 if ctx==nil{return "",errors.New("context is required")}
 out,err:=virsh(ctx,uri,"domstate",uuid)
 if err!=nil{return "",fmt.Errorf("query Libvirt state for VM %s: %w: %s",uuid,err,strings.TrimSpace(string(out)))}
 state:=strings.ToLower(strings.TrimSpace(string(out)))
 if state!="running"&&state!="blocked" {
  return state,fmt.Errorf("VM %s is not running in Libvirt (actual state: %s)",uuid,state)
 }
 return state,nil
}
