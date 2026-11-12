package main

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/x509"
 "encoding/binary"
 "encoding/pem"
 "errors"
 "net"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "testing"

 "github.com/ahhvahh/InsulaVM/insulavm"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
 "golang.org/x/crypto/ssh"
 "golang.org/x/crypto/ssh/knownhosts"
)

const cliLoopbackID="019a5d11-1234-7123-8123-123456789abc"

func cliLocalSSH(t *testing.T)(address,key,known string){
 t.Helper()
 _,private,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 signer,err:=ssh.NewSignerFromKey(private)
 if err!=nil{t.Fatal(err)}
 server:=&ssh.ServerConfig{NoClientAuth:true}
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
      _=incoming.Reject(ssh.UnknownChannelType,"session required");continue
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
       cmd:=exec.Command("sh","-c",command)
       cmd.Stdin=ch;cmd.Stdout=ch;cmd.Stderr=ch.Stderr()
       runErr:=cmd.Run()
       status:=uint32(0)
       if runErr!=nil{
        var exit *exec.ExitError
        if errors.As(runErr,&exit){status=uint32(exit.ExitCode())}else{status=255}
       }
       reply:=make([]byte,4);binary.BigEndian.PutUint32(reply,status)
       _,_=ch.SendRequest("exit-status",false,reply)
       return
      }
     }()
    }
   }(raw)
  }
 }()
 data,err:=x509.MarshalPKCS8PrivateKey(private)
 if err!=nil{t.Fatal(err)}
 root:=t.TempDir()
 key=filepath.Join(root,"id_ed25519")
 if err:=os.WriteFile(key,pem.EncodeToMemory(&pem.Block{Type:"PRIVATE KEY",Bytes:data}),0600);err!=nil{t.Fatal(err)}
 known=filepath.Join(root,"known_hosts")
 if err:=os.WriteFile(known,[]byte(knownhosts.Line([]string{listener.Addr().String()},signer.PublicKey())+"\n"),0600);err!=nil{t.Fatal(err)}
 return listener.Addr().String(),key,known
}

func cliConfigFixture(t *testing.T)(string,string){
 t.Helper()
 root:=t.TempDir()
 config:=filepath.Join(root,"config.yaml")
 workspace:=filepath.Join(root,"workspace-guest")
 data:="storage:\n  root: "+filepath.Join(root,"state")+"\nvirtualization:\n  provider: kvm\n  default_disk_gb: 8\nguest:\n  destination: "+workspace+"\n"
 if err:=os.WriteFile(config,[]byte(data),0600);err!=nil{t.Fatal(err)}
 return config,workspace
}

func cliRegisteredGuest(t *testing.T,config,address,key,known string)*insulavm.Engine{
 t.Helper()
 engine,err:=insulavm.New(config)
 if err!=nil{t.Fatal(err)}
 ip,portString,err:=net.SplitHostPort(address)
 if err!=nil{t.Fatal(err)}
 port,err:=strconv.Atoi(portString)
 if err!=nil{t.Fatal(err)}
 cfg:=engine.Configuration()
 dir:=filepath.Join(cfg.Storage.Runs,"insula-vm-"+cliLoopbackID)
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(cfg.Storage.Database)
 if err!=nil{t.Fatal(err)}
 row:=registry.Entry{
  ID:cliLoopbackID,LibvirtUUID:cliLoopbackID,Name:"insula-test-guest",
  Profile:"debian",MAC:"52:54:00:11:22:33",SSHUser:"test",
  SSHPort:port,PrivateKeyPath:key,KnownHostsPath:known,
  SudoMode:"disabled",InstanceDir:dir,
 }
 if err:=store.Insert(context.Background(),row);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(context.Background(),row.ID,ip);err!=nil{t.Fatal(err)}
 if err:=store.Close();err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 script:="#!/bin/sh\ncase \"$3\" in\n"+
  "list) if test -f \"$CLI_VM_RUNNING\"; then echo \"$CLI_VM_ID\"; fi ;;\n"+
  "domstate) echo running ;;\n"+
  "domifaddr) echo 'vnet0 ipv4 127.0.0.1/8 guest' ;;\n"+
  "shutdown) rm -f \"$CLI_VM_RUNNING\" ;;\n"+
  "destroy) rm -f \"$CLI_VM_RUNNING\" ;;\n"+
  "*) echo 'libvirt mock status' ;;\nesac\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 state:=filepath.Join(bin,"running")
 if err:=os.WriteFile(state,[]byte("on"),0600);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("CLI_VM_RUNNING",state)
 t.Setenv("CLI_VM_ID",cliLoopbackID)
 return engine
}
