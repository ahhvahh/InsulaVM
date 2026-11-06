package cloudinit

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
)

func fakeISOBuilder(t *testing.T){
 t.Helper()
 path:=t.TempDir()
 script:="#!/bin/sh\nout=''\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = '-output' ]; then shift; out=\"$1\"; fi\n  shift\ndone\ntest -n \"$out\" || exit 1\nprintf 'fakeISO' > \"$out\"\n"
 if err:=os.WriteFile(filepath.Join(path,"genisoimage"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",path+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSeedProvisioningSudoAndSSH(t *testing.T){
 fakeISOBuilder(t)
 client,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 host,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 id,err:=identity.UUIDv7()
 if err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 seed,err:=Prepare(context.Background(),dir,id,"insula","passwordless",client.PublicAuthorized,host)
 if err!=nil{t.Fatal(err)}
 config,err:=os.ReadFile(seed.UserData)
 if err!=nil{t.Fatal(err)}
 data:=string(config)
 for _,want:=range []string{
  "#cloud-config","ssh_pwauth: false","disable_root: true",
  "ssh_deletekeys: true","name: insula","lock_passwd: true",
  "ALL=(ALL) NOPASSWD:ALL","ssh_authorized_keys:",
  client.PublicAuthorized,host.PublicAuthorized,
  "ed25519_private: |","-----BEGIN OPENSSH PRIVATE KEY-----",
 }{
  if !strings.Contains(data,want){t.Errorf("missing %s in cloud config",want)}
 }
 meta,err:=os.ReadFile(seed.MetaData)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(meta),id){t.Fatal("UUID missing from NoCloud meta-data")}
 iso,err:=os.Stat(seed.ISO)
 if err!=nil{t.Fatal(err)}
 if iso.Mode().Perm()!=0600{t.Fatalf("seed ISO must be restricted: %v",iso.Mode())}
}

func TestDisabledSudoAndRejectRoot(t *testing.T){
 fakeISOBuilder(t)
 client,_:=identity.GenerateKey()
 host,_:=identity.GenerateKey()
 id,_:=identity.UUIDv7()
 seed,err:=Prepare(context.Background(),t.TempDir(),id,"insula","disabled",client.PublicAuthorized,host)
 if err!=nil{t.Fatal(err)}
 data,_:=os.ReadFile(seed.UserData)
 if !strings.Contains(string(data),"sudo: false"){t.Fatal("sudo was not disabled")}
 if _,err:=Prepare(context.Background(),t.TempDir(),id,"root","passwordless",client.PublicAuthorized,host);err==nil{t.Fatal("root SSH account accepted")}
 if _,err:=Prepare(context.Background(),t.TempDir(),id,"!bad","passwordless",client.PublicAuthorized,host);err==nil{t.Fatal("invalid username accepted")}
}
