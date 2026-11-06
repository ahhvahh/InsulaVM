package identity

import (
 "os"
 "net"
 "path/filepath"
 "strings"
 "testing"

 "golang.org/x/crypto/ssh"
 "golang.org/x/crypto/ssh/knownhosts"
)

func TestUUIDv7AndKeys(t *testing.T){
 ids:=map[string]bool{}
 for i:=0;i<200;i++{
  id,err:=UUIDv7()
  if err!=nil{t.Fatal(err)}
  if len(id)!=36||id[14]!='7'||id[19]!='8'&&id[19]!='9'&&id[19]!='a'&&id[19]!='b'{t.Fatalf("invalid UUIDv7 %q",id)}
  if ids[id]{t.Fatalf("duplicate UUIDv7 %s",id)}
  ids[id]=true
 }
 keys,err:=GenerateKey()
 if err!=nil{t.Fatal(err)}
 if !strings.HasPrefix(keys.PublicAuthorized,"ssh-ed25519 "){t.Fatal("expected Ed25519 public key")}
 signer,err:=ssh.ParsePrivateKey(keys.PrivatePEM)
 if err!=nil{t.Fatal(err)}
 if ssh.FingerprintSHA256(signer.PublicKey())!=Fingerprint(keys.Public){t.Fatal("private and public key mismatch")}
 root:=t.TempDir()
 path:=filepath.Join(root,"client_ed25519")
 if err:=SavePrivateKey(path,keys.PrivatePEM);err!=nil{t.Fatal(err)}
 mode,err:=os.Stat(path)
 if err!=nil{t.Fatal(err)}
 if mode.Mode().Perm()!=0600{t.Fatalf("insecure file mode %v",mode.Mode())}
 if err:=SavePrivateKey(path,keys.PrivatePEM);err==nil{t.Fatal("overwrote existing private key")}
 known:=filepath.Join(root,"known_hosts")
 if err:=SaveKnownHost(known,"127.0.0.1",keys.Public);err!=nil{t.Fatal(err)}
 check,err:=knownhosts.New(known)
 if err!=nil{t.Fatal(err)}
 remote:=&net.TCPAddr{IP:net.ParseIP("127.0.0.1"),Port:22}
 if err:=check("127.0.0.1:22",remote,keys.Public);err!=nil{t.Fatalf("pinned known_hosts was rejected: %v",err)}
 other,err:=GenerateKey()
 if err!=nil{t.Fatal(err)}
 if err:=check("127.0.0.1:22",remote,other.Public);err==nil{t.Fatal("accepted unexpected VM host key")}
}
