// Package cloudinit builds a NoCloud CIDATA seed ISO for already-installed
// Linux QCOW2 images prepared with cloud-init and OpenSSH. This is NOT an
// operating-system installer and does not work with a bare installer ISO.
package cloudinit

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
)

type Seed struct{
 ISO string
 UserData string
 MetaData string
}

// Prepare generates user-data and meta-data in the private per-instance
// directory, and creates an ISO9660 image labeled cidata. It embeds the
// guest host PRIVATE key to pin SSH host identity before first connection;
// protect seed.iso and delete it together with the VM on cleanup.
func Prepare(ctx context.Context,dir,id,user,sudoMode,clientPublic string,host identity.Keys)(Seed,error){
 var result Seed
 if ctx==nil{return result,errors.New("context is required")}
 if !identity.ValidUser(user){return result,fmt.Errorf("invalid non-root SSH user %q",user)}
 if sudoMode!="passwordless" && sudoMode!="disabled"{
  return result,fmt.Errorf("invalid sudo mode %q",sudoMode)
 }
 if !strings.HasPrefix(clientPublic,"ssh-ed25519 ") || strings.ContainsAny(clientPublic,"\r\n"){
  return result,errors.New("client SSH public key must be Ed25519")
 }
 if !strings.HasPrefix(host.PublicAuthorized,"ssh-ed25519 ") || len(host.PrivatePEM)==0{
  return result,errors.New("missing guest Ed25519 host identity")
 }
 if len(id)!=36||strings.ContainsAny(id,"\r\n/"){return result,errors.New("invalid instance UUID")}
 if err:=ctx.Err();err!=nil{return result,err}
 path:=filepath.Join(dir,"cloud-init")
 if err:=os.Mkdir(path,0700);err!=nil{return result,err}
 result.ISO=filepath.Join(dir,"seed.iso")
 result.UserData=filepath.Join(path,"user-data")
 result.MetaData=filepath.Join(path,"meta-data")
 sudo:="    sudo: false\n"
 if sudoMode=="passwordless"{sudo="    sudo: \"ALL=(ALL) NOPASSWD:ALL\"\n"}
 var keyBlock strings.Builder
 for _,line:=range strings.Split(strings.TrimSpace(string(host.PrivatePEM)),"\n"){
  keyBlock.WriteString("    "+line+"\n")
 }
 userData:="#cloud-config\n"+
  "ssh_pwauth: false\n"+
  "disable_root: true\n"+
  "ssh_deletekeys: true\n"+
  "users:\n"+
  "  - name: "+user+"\n"+
  "    shell: /bin/sh\n"+
  "    lock_passwd: true\n"+
  sudo+
  "    ssh_authorized_keys:\n"+
  "      - "+strconv.Quote(clientPublic)+"\n"+
  "ssh_keys:\n"+
  "  ed25519_private: |\n"+
  keyBlock.String()+
  "  ed25519_public: "+strconv.Quote(host.PublicAuthorized)+"\n"
 metaData:="instance-id: "+id+"\nlocal-hostname: insula-"+id[:8]+"\n"
 if err:=os.WriteFile(result.UserData,[]byte(userData),0600);err!=nil{return result,err}
 if err:=os.WriteFile(result.MetaData,[]byte(metaData),0600);err!=nil{return result,err}
 tool,args,err:=isoTool()
 if err!=nil{return result,err}
 args=append(args,"-quiet","-output",result.ISO,"-volid","cidata","-joliet","-rock","user-data","meta-data")
 command:=exec.CommandContext(ctx,tool,args...)
 command.Dir=path
 if out,err:=command.CombinedOutput();err!=nil{
  return result,fmt.Errorf("generate NoCloud CIDATA ISO: %w: %s",err,strings.TrimSpace(string(out)))
 }
 if err:=os.Chmod(result.ISO,0600);err!=nil{return result,err}
 return result,nil
}

func isoTool()(string,[]string,error){
 for _,name:=range []string{"genisoimage","mkisofs","xorriso"}{
  if path,err:=exec.LookPath(name);err==nil{
   if name=="xorriso"{return path,[]string{"-as","mkisofs"},nil}
   return path,nil,nil
  }
 }
 return "",nil,errors.New("NoCloud ISO generator not installed: install genisoimage or xorriso")
}
