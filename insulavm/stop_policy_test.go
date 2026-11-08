package insulavm

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/ahhvahh/InsulaVM/pkg/registry"
)

type stopPolicyMock struct {
    state string
    shutdownErr error
    shutdownCalled int
    stopped int
}
func(m *stopPolicyMock)GetVMStatus(_ context.Context,id string)(VMLiveInfo,error){
    return VMLiveInfo{Record:registry.Entry{ID:id,LibvirtUUID:id,Status:"running"},LibvirtState:m.state},nil
}
func(m *stopPolicyMock)gracefulShutdown(_ context.Context,_ string,_ time.Duration)error{
    m.shutdownCalled++
    return m.shutdownErr
}
func(m *stopPolicyMock)StopVM(_ context.Context,_ string)error{m.stopped++;return nil}

func TestStopWithPolicyGraceful(t *testing.T){
    backend:=&stopPolicyMock{state:"running"}
    got,err:=stopWithPolicy(context.Background(),backend,VMStopOptions{VMID:"id",ForceAfterTimeout:true})
    if err!=nil||!got.Graceful||got.Forced||backend.shutdownCalled!=1||backend.stopped!=1{
        t.Fatalf("unexpected graceful stop: result=%+v err=%v backend=%+v",got,err,backend)
    }
}
func TestStopWithPolicyForceAfterDeadline(t *testing.T){
    backend:=&stopPolicyMock{state:"running",shutdownErr:context.DeadlineExceeded}
    got,err:=stopWithPolicy(context.Background(),backend,VMStopOptions{VMID:"id",GracePeriod:30*time.Second,ForceAfterTimeout:true})
    if err!=nil||!got.Forced||!got.TimedOut||backend.stopped!=1{
        t.Fatalf("unexpected timeout escalation: result=%+v err=%v",got,err)
    }
}
func TestStopWithPolicyPreservesOnInfrastructureError(t *testing.T){
    backend:=&stopPolicyMock{state:"running",shutdownErr:errors.New("libvirt unavailable")}
    got,err:=stopWithPolicy(context.Background(),backend,VMStopOptions{VMID:"id",ForceAfterTimeout:true})
    if err==nil||got.Forced||backend.stopped!=0{
        t.Fatalf("must preserve VM when Libvirt fails: result=%+v err=%v",got,err)
    }
}
func TestStopWithPolicyNoForceByDefault(t *testing.T){
    backend:=&stopPolicyMock{state:"running",shutdownErr:context.DeadlineExceeded}
    _,err:=stopWithPolicy(context.Background(),backend,VMStopOptions{VMID:"id"})
    if !errors.Is(err,context.DeadlineExceeded)||backend.stopped!=0{
        t.Fatalf("unexpected force: %v %+v",err,backend)
    }
}
func TestStopWithPolicyAlreadyStopped(t *testing.T){
    backend:=&stopPolicyMock{state:"absent"}
    got,err:=stopWithPolicy(context.Background(),backend,VMStopOptions{VMID:"id"})
    if err!=nil||!got.Graceful||backend.shutdownCalled!=0||backend.stopped!=1{t.Fatalf("result=%+v err=%v",got,err)}
}
