package executor

import (
 "bytes"
 "context"
 "crypto/ecdsa"
 "crypto/elliptic"
 "crypto/rand"
 "fmt"
 "net"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
 "golang.org/x/crypto/ssh"
 "golang.org/x/crypto/ssh/knownhosts"
)

func TestDialContextCancelsHangingSSHHandshake(t *testing.T){
 root:=t.TempDir()
 keys,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 key:=filepath.Join(root,"key")
 if err:=identity.SavePrivateKey(key,keys.PrivatePEM);err!=nil{t.Fatal(err)}
 known:=filepath.Join(root,"known_hosts")
 if err:=os.WriteFile(known,nil,0600);err!=nil{t.Fatal(err)}
 listener,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 defer listener.Close()
 done:=make(chan struct{})
 go func(){
  conn,err:=listener.Accept()
  if err==nil{
   defer conn.Close()
   <-done
  }
 }()
 start:=time.Now()
 ctx,cancel:=context.WithTimeout(context.Background(),150*time.Millisecond)
 defer cancel()
 _,err=DialContext(ctx,DialConfig{Address:listener.Addr().String(),
  User:"insula",PrivateKeyPath:key,KnownHostsPath:known,Timeout:time.Second})
 close(done)
 if err==nil{t.Fatal("expected handshake cancellation")}
 if elapsed:=time.Since(start);elapsed>2*time.Second{t.Fatalf("handshake blocked for %v: %v",elapsed,err)}
 if !strings.Contains(err.Error(),"timed out") && !strings.Contains(err.Error(),"deadline"){
  t.Fatalf("missing meaningful timeout: %v",err)
 }
}

func TestDialContextRejectsNilContext(t *testing.T){
 if _,err:=DialContext(nil,DialConfig{});err==nil{t.Fatal("nil context accepted")}
}

func TestDialContextAuthenticatesWithPinnedHostKey(t *testing.T) {
 host,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 clientKeys,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 hostSigner,err:=ssh.ParsePrivateKey(host.PrivatePEM)
 if err!=nil{t.Fatal(err)}
 serverConfig:=&ssh.ServerConfig{PublicKeyCallback:func(_ ssh.ConnMetadata,key ssh.PublicKey)(*ssh.Permissions,error){
  if bytes.Equal(key.Marshal(),clientKeys.Public.Marshal()){return &ssh.Permissions{},nil}
  return nil,fmt.Errorf("unrecognized key")
 }}
 // Simulates Debian sshd offering multiple host key algorithms.
 // Default Go preference chooses ECDSA, whereas managed VM pins ED25519.
 ecdsaKey,err:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader)
 if err!=nil{t.Fatal(err)}
 ecdsaSigner,err:=ssh.NewSignerFromKey(ecdsaKey)
 if err!=nil{t.Fatal(err)}
 serverConfig.AddHostKey(ecdsaSigner)
 serverConfig.AddHostKey(hostSigner)
 listener,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 defer listener.Close()
 ready:=make(chan struct{})
 go func(){
  raw,e:=listener.Accept()
  if e!=nil{close(ready);return}
  defer raw.Close()
  session,chans,reqs,e:=ssh.NewServerConn(raw,serverConfig)
  close(ready)
  if e!=nil{return}
  defer session.Close()
  go ssh.DiscardRequests(reqs)
  for ch:=range chans{_ = ch.Reject(ssh.UnknownChannelType,"not supported")}
 }()
 root:=t.TempDir()
 key:=filepath.Join(root,"client_ed25519")
 if err:=identity.SavePrivateKey(key,clientKeys.PrivatePEM);err!=nil{t.Fatal(err)}
 known:=filepath.Join(root,"known_hosts")
 address:=listener.Addr().String()
 line:=knownhosts.Line([]string{"["+strings.Split(address,":")[0]+"]:"+strings.Split(address,":")[1]},host.Public)+"\n"
 if err:=os.WriteFile(known,[]byte(line),0600);err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Second)
 defer cancel()
 connected,err:=DialContext(ctx,DialConfig{Address:address,User:"insula",PrivateKeyPath:key,
  KnownHostsPath:known,Timeout:time.Second,HostKeyAlgorithms:[]string{ssh.KeyAlgoED25519}})
 if err!=nil{t.Fatal(err)}
 _=connected.Close()
 <-ready
}
