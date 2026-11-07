package insulavm

import (
 "context"
 "errors"
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
