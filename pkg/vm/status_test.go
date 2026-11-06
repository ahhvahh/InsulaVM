package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

const runningID="019a5d11-1234-7123-8123-123456789abc"
const pausedID="019a5d11-1234-7123-8123-123456789abd"

func fakeVirshForStatus(t *testing.T,script string){
 t.Helper()
 bin:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte("#!/bin/sh\n"+script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestInspectRegisteredReconcilesRealStateWithoutChangingSQLite(t *testing.T){
 fakeVirshForStatus(t,"case \"$3\" in\n"+
 "list) printf '%s\\n' "+runningID+" "+pausedID+";;\n"+
 "domstate)\n case \"$4\" in\n"+
 " "+runningID+") printf 'running\\n';;\n"+
 " "+pausedID+") printf 'paused\\n';;\n"+
 " *) exit 9;;\n esac;;\n"+
 "*) exit 7;;\nesac\n")
 records:=[]registry.Entry{
  {ID:"r",LibvirtUUID:runningID,Status:"failed"},
  {ID:"p",LibvirtUUID:pausedID,Status:"running"},
  {ID:"missing",LibvirtUUID:"019a5d11-1234-7123-8123-123456789abe",Status:"creating"},
 }
 statuses,err:=InspectRegistered(context.Background(),"qemu:///system",records)
 if err!=nil{t.Fatal(err)}
 if len(statuses)!=3{t.Fatalf("unexpected statuses: %+v",statuses)}
 if !statuses[0].IsRunning()||statuses[0].LibvirtState!="running"{
  t.Fatalf("Libvirt active VM not identified: %+v",statuses[0])
 }
 if !strings.Contains(statuses[0].Warning,"saved SQLite"){t.Fatal("expected divergent SQLite warning")}
 if statuses[1].IsRunning()||statuses[1].LibvirtState!="paused"{
  t.Fatalf("paused VM incorrectly reported as running: %+v",statuses[1])
 }
 if statuses[2].LibvirtState!="absent"||!strings.Contains(statuses[2].Warning,"stale SQLite"){
  t.Fatalf("stale creation not identified: %+v",statuses[2])
 }
 if records[0].Status!="failed"||records[2].Status!="creating"{t.Fatal("read-only inspection modified SQLite state")}
}

func TestInspectRegisteredNeverClaimsOfflineWhenLibvirtUnavailable(t *testing.T){
 fakeVirshForStatus(t,"echo 'connection refused' >&2\nexit 12\n")
 _,err:=InspectRegistered(context.Background(),"qemu:///system",[]registry.Entry{{ID:"id"}})
 if err==nil||!strings.Contains(err.Error(),"connection refused"){t.Fatalf("expected Libvirt connection error: %v",err)}
}

func TestActiveDomainStateFailsFastForStoppedVM(t *testing.T){
 fakeVirshForStatus(t,"if [ \"$3\" = \"domstate\" ]; then printf 'shut off\\n'; exit 0; fi\nexit 9\n")
 state,err:=ActiveDomainState(context.Background(),"qemu:///system",runningID)
 if state!="shut off"||err==nil||!strings.Contains(err.Error(),"not running"){
  t.Fatalf("did not reject stopped domain: state=%q err=%v",state,err)
 }
}
