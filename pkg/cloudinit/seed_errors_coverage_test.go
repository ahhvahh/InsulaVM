package cloudinit

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
)

func TestNoCloudSeedRejectsUntrustedAndMalformedProvisioning(t *testing.T){
 fakeISOBuilder(t)
 client,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 host,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 id,err:=identity.UUIDv7()
 if err!=nil{t.Fatal(err)}
 original:=host
 tests:=[]struct{
  name string
  ctx context.Context
  id,user,sudo,public,mac string
  host identity.Keys
  script []byte
  want string
 }{
  {"root SSH",context.Background(),id,"root","disabled",client.PublicAuthorized,"",host,nil,"invalid non-root"},
  {"unsafe SSH",context.Background(),id,"-unsafe","disabled",client.PublicAuthorized,"",host,nil,"invalid non-root"},
  {"unsupported sudo",context.Background(),id,"insula","root",client.PublicAuthorized,"",host,nil,"invalid sudo"},
  {"invalid client key",context.Background(),id,"insula","disabled","ssh-rsa old","",host,nil,"Ed25519"},
  {"newline client key",context.Background(),id,"insula","disabled",client.PublicAuthorized+"\n","",host,nil,"Ed25519"},
  {"missing host",context.Background(),id,"insula","disabled",client.PublicAuthorized,"",identity.Keys{},nil,"host identity"},
  {"short instance UUID",context.Background(),"bad","insula","disabled",client.PublicAuthorized,"",host,nil,"UUID"},
  {"invalid Ethernet MAC",context.Background(),id,"insula","disabled",client.PublicAuthorized,"not-mac",host,nil,"invalid Ethernet MAC"},
  {"excessive script",context.Background(),id,"insula","disabled",client.PublicAuthorized,"",host,make([]byte,256*1024+1),"startup script"},
  {"NUL script",context.Background(),id,"insula","disabled",client.PublicAuthorized,"",host,[]byte{0},"startup script"},
  {"nil context",nil,id,"insula","disabled",client.PublicAuthorized,"",host,nil,"context"},
 }
 _=original
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   _,err:=PrepareWithStartupScriptAndNetwork(tc.ctx,t.TempDir(),tc.id,tc.user,tc.sudo,tc.public,tc.host,tc.script,tc.mac)
   if err==nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("expected %q, got %v",tc.want,err)
   }
  })
 }
 cancelled,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=PrepareWithStartupScript(cancelled,t.TempDir(),id,"insula","disabled",client.PublicAuthorized,host,nil);!errors.Is(err,context.Canceled){
  t.Fatalf("cancelled seed generation accepted: %v",err)
 }
}

func TestNoCloudSeedRefusesExistingDirectoryAndBrokenISOGenerator(t *testing.T){
 fakeISOBuilder(t)
 client,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 host,err:=identity.GenerateKey()
 if err!=nil{t.Fatal(err)}
 id,err:=identity.UUIDv7()
 if err!=nil{t.Fatal(err)}
 root:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(root,"cloud-init"),[]byte("do-not-replace"),0600);err!=nil{t.Fatal(err)}
 if _,err:=Prepare(context.Background(),root,id,"insula","disabled",client.PublicAuthorized,host);err==nil{
  t.Fatal("seed generator overwrote existing file")
 }
 other:=t.TempDir()
 bin:=t.TempDir()
 script:="#!/bin/sh\necho iso-generator-rejected >&2\nexit 41\n"
 if err:=os.WriteFile(filepath.Join(bin,"genisoimage"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin)
 if _,err:=Prepare(context.Background(),other,id,"insula","disabled",client.PublicAuthorized,host);err==nil||
  !strings.Contains(err.Error(),"iso-generator-rejected"){
  t.Fatalf("failed ISO builder not reported: %v",err)
 }
 t.Setenv("PATH",t.TempDir())
 if _,err:=Prepare(context.Background(),t.TempDir(),id,"insula","disabled",client.PublicAuthorized,host);err==nil||
  !strings.Contains(err.Error(),"NoCloud ISO generator"){
  t.Fatalf("missing ISO builder accepted: %v",err)
 }
}
