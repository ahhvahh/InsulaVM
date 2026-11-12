package vmstop

import (
    "context"
    "errors"
    "testing"
    "strings"
    "time"

    "github.com/ahhvahh/InsulaVM/pkg/registry"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

type stopPolicyMock struct {
    state string
    shutdownErr error
    shutdownCalled int
    stopped int
}
func(m *stopPolicyMock)GetVMStatus(_ context.Context,id string)(vm.LiveInfo,error){
    return vm.LiveInfo{Record:registry.Entry{ID:id,LibvirtUUID:id,Status:"running"},LibvirtState:m.state},nil
}
func(m *stopPolicyMock)GracefulShutdown(_ context.Context,_ string,_ time.Duration)error{
    m.shutdownCalled++
    return m.shutdownErr
}
func(m *stopPolicyMock)StopVM(_ context.Context,_ string)error{m.stopped++;return nil}

func TestStopWithPolicyGraceful(t *testing.T){
    backend:=&stopPolicyMock{state:"running"}
    got,err:=StopWithPolicy(context.Background(),backend,StopOptions{VMID:"id",ForceAfterTimeout:true})
    if err!=nil||!got.Graceful||got.Forced||backend.shutdownCalled!=1||backend.stopped!=1{
        t.Fatalf("unexpected graceful stop: result=%+v err=%v backend=%+v",got,err,backend)
    }
}
func TestStopWithPolicyForceAfterDeadline(t *testing.T){
    backend:=&stopPolicyMock{state:"running",shutdownErr:context.DeadlineExceeded}
    got,err:=StopWithPolicy(context.Background(),backend,StopOptions{VMID:"id",GracePeriod:30*time.Second,ForceAfterTimeout:true})
    if err!=nil||!got.Forced||!got.TimedOut||backend.stopped!=1{
        t.Fatalf("unexpected timeout escalation: result=%+v err=%v",got,err)
    }
}
func TestStopWithPolicyPreservesOnInfrastructureError(t *testing.T){
    backend:=&stopPolicyMock{state:"running",shutdownErr:errors.New("libvirt unavailable")}
    got,err:=StopWithPolicy(context.Background(),backend,StopOptions{VMID:"id",ForceAfterTimeout:true})
    if err==nil||got.Forced||backend.stopped!=0{
        t.Fatalf("must preserve VM when Libvirt fails: result=%+v err=%v",got,err)
    }
}
func TestStopWithPolicyNoForceByDefault(t *testing.T){
    backend:=&stopPolicyMock{state:"running",shutdownErr:context.DeadlineExceeded}
    _,err:=StopWithPolicy(context.Background(),backend,StopOptions{VMID:"id"})
    if !errors.Is(err,context.DeadlineExceeded)||backend.stopped!=0{
        t.Fatalf("unexpected force: %v %+v",err,backend)
    }
}
func TestStopWithPolicyAlreadyStopped(t *testing.T){
    backend:=&stopPolicyMock{state:"absent"}
    got,err:=StopWithPolicy(context.Background(),backend,StopOptions{VMID:"id"})
    if err!=nil||!got.Graceful||backend.shutdownCalled!=0||backend.stopped!=1{t.Fatalf("result=%+v err=%v",got,err)}
}

func TestStopPolicyRejectsUnsafeRequests(t *testing.T) {
 tests:=[]struct{name string;ctx context.Context;backend PolicyBackend;opts StopOptions;want string}{
  {"nil context",nil,&stopPolicyMock{state:"running"},StopOptions{VMID:"valid"},"context"},
  {"nil backend",context.Background(),nil,StopOptions{VMID:"valid"},"backend"},
  {"missing VM",context.Background(),&stopPolicyMock{state:"running"},StopOptions{},"VMID"},
  {"negative grace",context.Background(),&stopPolicyMock{state:"running"},StopOptions{VMID:"valid",GracePeriod:-time.Second},"GracePeriod"},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=StopWithPolicy(tc.ctx,tc.backend,tc.opts);err==nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("unsafe request accepted: %v",err)
   }
  })
 }
}
