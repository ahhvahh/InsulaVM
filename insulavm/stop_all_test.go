package insulavm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "reflect"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

type stopAllFake struct {
 live []VMLiveInfo
 inspectErr error
 stopErr map[string]error
 stopped []string
}

func(f *stopAllFake)ListVMStatus(_ context.Context)([]VMLiveInfo,error){
 return f.live,f.inspectErr
}
func(f *stopAllFake)StopVM(_ context.Context,id string)error{
 f.stopped=append(f.stopped,id)
 return f.stopErr[id]
}
func stopAllEntry(id,state string)VMLiveInfo{
 return VMLiveInfo{Record:registry.Entry{ID:id,LibvirtUUID:id,Status:"running"},LibvirtState:state}
}

func TestStopAllManagedDryRunNeverStops(t *testing.T){
 first:=stopAllEntry("01a11111-1111-7111-8111-111111111111","running")
 second:=stopAllEntry("01a22222-2222-7222-8222-222222222222","paused")
 absent:=stopAllEntry("01a33333-3333-7333-8333-333333333333","absent")
 fake:=&stopAllFake{live:[]VMLiveInfo{first,second,absent}}
 var events []VMStopAllProgress
 result,err:=stopAllManagedVMs(context.Background(),fake,VMStopAllOptions{
  DryRun:true,OnProgress:func(p VMStopAllProgress){events=append(events,p)},
 })
 if err!=nil{t.Fatal(err)}
 if result.Checked!=3||len(result.Targets)!=2||len(result.Skipped)!=1||
  len(result.Stopped)!=0||len(fake.stopped)!=0{
  t.Fatalf("dry-run must not modify VM state: %+v calls=%v",result,fake.stopped)
 }
 if len(events)!=3||events[0].Stage!="planned"||events[1].Stage!="planned"||events[2].Stage!="skipped"{
  t.Fatalf("unexpected callbacks: %+v",events)
 }
}

func TestStopAllManagedContinuesAfterFailure(t *testing.T){
 first:=stopAllEntry("01a11111-1111-7111-8111-111111111111","running")
 second:=stopAllEntry("01a22222-2222-7222-8222-222222222222","blocked")
 third:=stopAllEntry("01a33333-3333-7333-8333-333333333333","shut off")
 absent:=stopAllEntry("01a44444-4444-7444-8444-444444444444","absent")
 unknown:=stopAllEntry("01a55555-5555-7555-8555-555555555555","unknown")
 mismatched:=stopAllEntry("01a66666-6666-7666-8666-666666666666","running")
 mismatched.Record.LibvirtUUID="unmanaged-uuid"
 fake:=&stopAllFake{
  live:[]VMLiveInfo{first,second,third,absent,unknown,mismatched},
  stopErr:map[string]error{second.Record.ID:errors.New("libvirt destroy refused")},
 }
 result,err:=stopAllManagedVMs(context.Background(),fake,VMStopAllOptions{})
 if err==nil||!strings.Contains(err.Error(),"destroy refused"){t.Fatalf("missing error: %v",err)}
 if !reflect.DeepEqual(fake.stopped,[]string{first.Record.ID,second.Record.ID,third.Record.ID}){
  t.Fatalf("unexpected stop targets: %v",fake.stopped)
 }
 if len(result.Stopped)!=2||len(result.Failed)!=3||len(result.Skipped)!=1{
  t.Fatalf("unexpected partial report: %+v",result)
 }
}

func TestStopAllManagedStopsNothingWhenLibvirtInspectionFails(t *testing.T){
 fake:=&stopAllFake{inspectErr:errors.New("hypervisor unavailable")}
 result,err:=stopAllManagedVMs(context.Background(),fake,VMStopAllOptions{})
 if result!=nil||err==nil||len(fake.stopped)!=0{
  t.Fatalf("failure must prevent all stop attempts: result=%+v err=%v calls=%v",result,err,fake.stopped)
 }
}

func TestStopAllManagedCancellationDoesNotStopAnything(t *testing.T){
 fake:=&stopAllFake{live:[]VMLiveInfo{stopAllEntry("01a11111-1111-7111-8111-111111111111","running")}}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 result,err:=stopAllManagedVMs(ctx,fake,VMStopAllOptions{})
 if !errors.Is(err,context.Canceled)||result!=nil||len(fake.stopped)!=0{
  t.Fatalf("expected immediate cancellation: result=%+v err=%v calls=%v",result,err,fake.stopped)
 }
}


func TestStopAllVMsUsesRegistryNotForeignLibvirtDomains(t *testing.T){
 engine:=testEngine(t,"")
 ctx:=context.Background()
 const managedID="01a11111-1111-7111-8111-111111111111"
 const foreignID="01afffff-ffff-7fff-8fff-ffffffffffff"
 dir:=filepath.Join(engine.config.Storage.Runs,"insula-vm-managed")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"overlay.qcow2"),[]byte("overlay"),0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(engine.config.Storage.Database)
 if err!=nil{t.Fatal(err)}
 if err:=store.Insert(ctx,registry.Entry{
  ID:managedID,LibvirtUUID:managedID,Name:"insula-debian-"+managedID[:8],
  Profile:"debian",MAC:"52:54:00:aa:bb:cc",SSHUser:"insula",InstanceDir:dir,
 });err!=nil{t.Fatal(err)}
 _=store.Close()

 bin:=t.TempDir()
 logFile:=filepath.Join(bin,"virsh.log")
 virshScript:="#!/bin/sh\n"+
  "case \"$3\" in\n"+
  "  list) printf '%s\\n' \"$MOCK_MANAGED_ID\" \"$MOCK_FOREIGN_ID\" ;;\n"+
  "  domstate) echo running ;;\n"+
  "  destroy) printf '%s\\n' \"$4\" >> \"$MOCK_VIRSH_LOG\" ;;\n"+
  "  *) echo unexpected >&2; exit 91 ;;\n"+
  "esac\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(virshScript),0700);err!=nil{t.Fatal(err)}
 t.Setenv("MOCK_MANAGED_ID",managedID)
 t.Setenv("MOCK_FOREIGN_ID",foreignID)
 t.Setenv("MOCK_VIRSH_LOG",logFile)
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))

 dry,err:=engine.StopAllVMs(ctx,VMStopAllOptions{DryRun:true})
 if err!=nil||len(dry.Targets)!=1||len(dry.Stopped)!=0{t.Fatalf("dry run: %+v err=%v",dry,err)}
 if _,err:=os.Stat(dir);err!=nil{t.Fatalf("dry-run removed storage: %v",err)}
 if _,err:=os.Stat(logFile);!os.IsNotExist(err){t.Fatalf("dry-run issued destroy: %v",err)}

 done,err:=engine.StopAllVMs(ctx,VMStopAllOptions{})
 if err!=nil{t.Fatal(err)}
 if len(done.Stopped)!=1||done.Stopped[0]!=managedID{t.Fatalf("unexpected stopped VMs: %+v",done)}
 calls,err:=os.ReadFile(logFile)
 if err!=nil{t.Fatal(err)}
 if strings.TrimSpace(string(calls))!="insula-debian-"+managedID[:8]{
  t.Fatalf("attempted to destroy unrelated VM: %q",calls)
 }
 if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("VM overlay not removed: %v",err)}
 store,err=registry.Open(engine.config.Storage.Database)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 saved,err:=store.Get(ctx,managedID)
 if err!=nil||saved.Status!="destroyed"{t.Fatalf("VM not marked destroyed: %+v err=%v",saved,err)}
}
