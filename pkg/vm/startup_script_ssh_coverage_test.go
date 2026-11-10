package vm

import (
 "bytes"
 "context"
 "encoding/binary"
 "errors"
 "net"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
 "golang.org/x/crypto/ssh"
 "golang.org/x/crypto/ssh/knownhosts"
)

// A real authenticated SSH loopback connection verifies Cloud-Init's final
// success sentinel without requiring a host Libvirt or a privileged TCP port.
func startupSSHGuest(t *testing.T,shouldSucceed bool)*Instance{
 t.Helper()
 host,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 client,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 signer,err:=ssh.ParsePrivateKey(host.PrivatePEM)
 if err!=nil{t.Fatal(err)}
 server:=&ssh.ServerConfig{PublicKeyCallback:func(_ ssh.ConnMetadata,p ssh.PublicKey)(*ssh.Permissions,error){
  if !bytes.Equal(p.Marshal(),client.Public.Marshal()){
   return nil,errors.New("guest rejected unknown login key")
  }
  return &ssh.Permissions{},nil
 }}
 server.AddHostKey(signer)
 listener,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ = listener.Close()})
 go func(){
  for {
   raw,err:=listener.Accept()
   if err!=nil{return}
   go func(raw net.Conn){
    conn,channels,requests,err:=ssh.NewServerConn(raw,server)
    if err!=nil{_ = raw.Close();return}
    defer conn.Close()
    go ssh.DiscardRequests(requests)
    for incoming:=range channels{
     if incoming.ChannelType()!="session"{
      _=incoming.Reject(ssh.UnknownChannelType,"session only")
      continue
     }
     ch,reqs,err:=incoming.Accept()
     if err!=nil{continue}
     go func(){
      defer ch.Close()
      for req:=range reqs{
       if req.Type!="exec"{_ = req.Reply(false,nil);continue}
       if len(req.Payload)<4{return}
       n:=int(binary.BigEndian.Uint32(req.Payload[:4]))
       if n<0||n>len(req.Payload)-4{return}
       command:=string(req.Payload[4:4+n])
       _=req.Reply(true,nil)
       code:=uint32(0)
       if !strings.Contains(command,"cloud-init status --wait --long")||
          !strings.Contains(command,"/var/lib/insula/startup-success"){
        code=97
       }else if !shouldSucceed{
        _,_ = ch.Stderr().Write([]byte("cloud-init startup sentinel was not created\n"))
        code=23
       }
       reply:=make([]byte,4)
       binary.BigEndian.PutUint32(reply,code)
       _,_ = ch.SendRequest("exit-status",false,reply)
       return
      }
     }()
    }
   }(raw)
  }
 }()
 root:=t.TempDir()
 key:=filepath.Join(root,"client")
 if err:=identity.SavePrivateKey(key,client.PrivatePEM);err!=nil{t.Fatal(err)}
 known:=filepath.Join(root,"known_hosts")
 address:=listener.Addr().String()
 if err:=os.WriteFile(known,[]byte(knownhosts.Line([]string{address},host.Public)+"\n"),0600);err!=nil{t.Fatal(err)}
 return &Instance{
  Address:address,SSHUser:"insula",
  SSHPrivateKeyPath:key,SSHKnownHostsPath:known,
 }
}

func TestWaitStartupScriptChecksCloudInitSuccessAfterSSH(t *testing.T){
 instance:=startupSSHGuest(t,true)
 if err:=waitStartupScript(context.Background(),instance);err!=nil{
  t.Fatalf("verified Cloud-Init sentinel failed: %v",err)
 }
}

func TestWaitStartupScriptReturnsExactGuestFailure(t *testing.T){
 instance:=startupSSHGuest(t,false)
 err:=waitStartupScript(context.Background(),instance)
 if err==nil||!strings.Contains(err.Error(),"cloud-init startup sentinel was not created"){
  t.Fatalf("guest provisioning failure was not propagated: %v",err)
 }
}

func TestWaitStartupScriptRejectsUnpinnedGuestHost(t *testing.T){
 instance:=startupSSHGuest(t,true)
 if err:=os.WriteFile(instance.SSHKnownHostsPath,[]byte(""),0600);err!=nil{t.Fatal(err)}
 err:=waitStartupScript(context.Background(),instance)
 if err==nil{t.Fatal("untrusted SSH server accepted without known_hosts")}
}
