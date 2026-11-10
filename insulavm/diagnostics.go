package insulavm

import (
 "context"
 "errors"
 "fmt"
 "net"
 "strconv"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/executor"
 "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// SSHDiagnostic is also available to Go callers: no shell commands are required.
// A partial result is returned on failure, showing the last completed step.
type SSHDiagnostic struct {
 VMID string
 VMName string
 LibvirtState string
 RegistryState string
 IP string
 SSHUser string
 Authenticated bool
 SudoReady bool
}

// DiagnoseVMSSH tests the SAME Go SSH client used by VM startup.
// It supports 'creating' and preserved 'failed' entries while Libvirt is running.
func (e *Engine) DiagnoseVMSSH(ctx context.Context,id string,timeout time.Duration)(*SSHDiagnostic,error) {
 if e==nil{return nil,errors.New("engine is required")}
 if ctx==nil{return nil,errors.New("context is required")}
 if id==""{return nil,errors.New("VM ID is required")}
 if timeout<0||timeout>time.Minute{return nil,errors.New("SSH test timeout must be 0..1m")}
 if timeout==0{timeout=15*time.Second}
 checked,err:=e.GetVMStatus(ctx,id)
 if err!=nil{return nil,err}
 record:=checked.Record
 result:=&SSHDiagnostic{VMID:id,VMName:record.Name,LibvirtState:checked.LibvirtState,
  RegistryState:record.Status,SSHUser:record.SSHUser,IP:record.IP}
 if !checked.IsRunning(){return result,fmt.Errorf("VM is not running in Libvirt: %s",checked.LibvirtState)}
 if result.IP==""{
  result.IP,err=vm.LeaseIPv4(ctx,e.config.Virtualization.LibvirtURI,record.Name)
  if err!=nil{return result,fmt.Errorf("find guest IP: %w",err)}
 }
 port:=record.SSHPort
 if port==0{port=22}
 probeCtx,cancel:=context.WithTimeout(ctx,timeout)
 defer cancel()
 client,err:=executor.DialContext(probeCtx,executor.DialConfig{
  Address:net.JoinHostPort(result.IP,strconv.Itoa(port)),User:record.SSHUser,
  PrivateKeyPath:record.PrivateKeyPath,KnownHostsPath:record.KnownHostsPath,Timeout:timeout,
  HostKeyAlgorithms:[]string{"ssh-ed25519"},
 })
 if err!=nil{return result,fmt.Errorf("SSH authentication failed: %w",err)}
 defer client.Close()
 result.Authenticated=true
 session,err:=client.NewSession()
 if err!=nil{return result,fmt.Errorf("create SSH test session: %w",err)}
 stop:=context.AfterFunc(probeCtx,func(){_ = session.Close()})
 output,err:=session.CombinedOutput("id -u")
 stop()
 _=session.Close()
 if err!=nil{return result,fmt.Errorf("SSH remote command failed: %w: %s",err,string(output))}
 if record.SudoMode=="passwordless" {
  session,err=client.NewSession()
  if err!=nil{return result,fmt.Errorf("create SSH sudo session: %w",err)}
  stop=context.AfterFunc(probeCtx,func(){_ = session.Close()})
  output,err=session.CombinedOutput("sudo -n true")
  stop()
  _=session.Close()
  if err!=nil{return result,fmt.Errorf("sudo -n true failed: %w: %s",err,string(output))}
  result.SudoReady=true
 }
 return result,nil
}
