// Package identity generates per-VM cryptographic identifiers and SSH keys.
package identity

import (
 "crypto/ed25519"
 "crypto/rand"
 "encoding/hex"
 "encoding/pem"
 "errors"
 "fmt"
 "net"
 "os"
 "path/filepath"
 "strings"
 "time"

 "golang.org/x/crypto/ssh"
 "golang.org/x/crypto/ssh/knownhosts"
)

// UUIDv7 uses 48 bits of Unix milliseconds and 74 random bits, per RFC 9562.
// Database uniqueness is enforced by the registry PRIMARY KEY.
func UUIDv7()(string,error){
 var b [16]byte
 if _,err:=rand.Read(b[:]);err!=nil{return "",err}
 millis:=uint64(time.Now().UnixMilli())
 for i:=5;i>=0;i--{b[i]=byte(millis);millis>>=8}
 b[6]=(b[6]&0x0f)|0x70
 b[8]=(b[8]&0x3f)|0x80
 h:=hex.EncodeToString(b[:])
 return fmt.Sprintf("%s-%s-%s-%s-%s",h[:8],h[8:12],h[12:16],h[16:20],h[20:]),nil
}

type Keys struct{
 PrivatePEM []byte
 PublicAuthorized string
 Public ssh.PublicKey
}

// GenerateKey generates an OpenSSH-compatible Ed25519 keypair. The same
// function can be used for the VM login key and separately for the host key.
func GenerateKey()(Keys,error){
 var result Keys
 pub,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{return result,err}
 block,err:=ssh.MarshalPrivateKey(private,"")
 if err!=nil{return result,err}
 sshPub,err:=ssh.NewPublicKey(pub)
 if err!=nil{return result,err}
 result.PrivatePEM=pem.EncodeToMemory(block)
 result.PublicAuthorized=strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
 result.Public=sshPub
 return result,nil
}

// SavePrivateKey writes the per-instance client key; it must not be copied
// into the SQLite registry or exposed through CLI output.
func SavePrivateKey(path string,key []byte)error{
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return err}
 file,err:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err!=nil{return err}
 _,writeErr:=file.Write(key)
 return errors.Join(writeErr,file.Close())
}

// SaveKnownHost pins the host public key generated for the guest. There is
// deliberately no InsecureIgnoreHostKey or SSH keyscan/TOFU fallback.
func SaveKnownHost(path,ip string,pub ssh.PublicKey)error{
 if net.ParseIP(ip)==nil{return fmt.Errorf("invalid guest IP: %q",ip)}
 if pub==nil{return errors.New("missing guest host key")}
 line:=knownhosts.Line([]string{ip},pub)+"\n"
 return os.WriteFile(path,[]byte(line),0600)
}

// Fingerprint returns the pinned SHA-256 fingerprint of a public host key.
func Fingerprint(pub ssh.PublicKey)string{
 if pub==nil{return ""}
 return ssh.FingerprintSHA256(pub)
}
