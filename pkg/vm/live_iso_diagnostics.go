package vm

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/storage"
)

// LiveISOLog records host-side boot stages without logging user-data,
// private SSH keys, or Cloud-Init seed contents. Kept after VM cleanup.
type LiveISOLog struct {
 Dir string
 Path string
 file *os.File
}
func OpenLiveISOLog(root,uuid string)(*LiveISOLog,error){
 dir,err:=liveISOLogDirectory(root,uuid)
 if err!=nil{return nil,err}
 path:=filepath.Join(dir,"events.log")
 f,err:=os.OpenFile(path,os.O_CREATE|os.O_WRONLY|os.O_APPEND,0600)
 if err!=nil{return nil,fmt.Errorf("open Live ISO event log: %w",err)}
 return &LiveISOLog{Dir:dir,Path:path,file:f},nil
}
func(l *LiveISOLog) Event(stage,message string){
 if l==nil||l.file==nil{return}
 // Individual records contain no secrets; no raw Cloud-Init payload is logged.
 _,_=fmt.Fprintf(l.file,"%s [%s] %s\n",time.Now().UTC().Format(time.RFC3339Nano),stage,message)
 _=l.file.Sync()
}
func(l *LiveISOLog) Close()error{
 if l==nil||l.file==nil{return nil}
 return l.file.Close()
}
func liveISOLogDirectory(root,uuid string)(string,error){
 if err:=storage.ValidateName(uuid);err!=nil{return "",fmt.Errorf("invalid VM diagnostic ID: %w",err)}
 if root==""{return "",errors.New("VM log directory is required")}
 dir:=filepath.Join(root,"live-iso",uuid)
 if err:=os.MkdirAll(dir,0700);err!=nil{return "",fmt.Errorf("create private VM diagnostics: %w",err)}
 return dir,nil
}

// CaptureLiveISODiagnostics creates a bounded, private host-side evidence
// snapshot before the VM is optionally destroyed. Each Libvirt command uses
// its own timeout; failure of one probe never prevents other probes.
// No guest credentials or raw NoCloud seed are included.
func CaptureLiveISODiagnostics(ctx context.Context,logsRoot,uri,uuid,network,mac string)(string,error){
 if ctx==nil{return "",errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return "",err}
 dir,err:=liveISOLogDirectory(logsRoot,uuid)
 if err!=nil{return "",err}
 path:=filepath.Join(dir,"diagnostic.log")
 f,err:=os.OpenFile(path,os.O_CREATE|os.O_WRONLY|os.O_APPEND,0600)
 if err!=nil{return "",err}
 defer f.Close()
 _,_=fmt.Fprintf(f,"\n==== Live ISO snapshot %s uuid=%s ===\n",time.Now().UTC().Format(time.RFC3339),uuid)
 probes:=[]struct{
  name string
  args []string
 }{
  {"domstate",[]string{"domstate",uuid}},
  {"dominfo",[]string{"dominfo",uuid}},
  {"domiflist",[]string{"domiflist",uuid}},
  {"dhcp-lease",[]string{"domifaddr",uuid,"--source","lease"}},
  {"network-state",[]string{"net-info",network}},
  {"network-leases",[]string{"net-dhcp-leases",network}},
 }
 for _,p:=range probes{
  if err:=ctx.Err();err!=nil{return path,err}
  probeCtx,cancel:=context.WithTimeout(ctx,5*time.Second)
  args:=append([]string{"-c",uri},p.args...)
  out,runErr:=exec.CommandContext(probeCtx,"virsh",args...).CombinedOutput()
  cancel()
  // Other VM leases are not relevant; avoid recording unrelated MAC/IP data.
  if p.name=="network-leases"{
   lines:=strings.Split(string(out),"\n")
   var filtered []string
   for _,line:=range lines{
    if mac!=""&&strings.Contains(strings.ToLower(line),strings.ToLower(mac)){filtered=append(filtered,line)}
   }
   if len(filtered)==0{out=[]byte("No DHCP lease for this VM MAC: "+mac)}
   if len(filtered)>0{out=[]byte(strings.Join(filtered,"\n"))}
  }
  if len(out)>8192{out=out[:8192]}
  _,_=fmt.Fprintf(f,"[%s] command=virsh %s\n%s\n",p.name,strings.Join(p.args," "),strings.TrimSpace(string(out)))
  if runErr!=nil{_,_=fmt.Fprintf(f,"error=%v\n",runErr)}
 }
 _=f.Sync()
 return path,nil
}
