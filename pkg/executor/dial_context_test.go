package executor

import (
 "context"
 "net"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
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
