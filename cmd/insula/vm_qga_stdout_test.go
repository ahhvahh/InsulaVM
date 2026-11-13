package main

import (
 "bytes"
 "context"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

// CLI output must remain exactly the guest process stdout/stderr for QGA.
// Unit mocks do not prove that a real KVM guest is ready.
func TestVMExecQGAOutputsRawStreamsWithoutLogMessage(t *testing.T){
 const id="019a5d11-1234-7123-8123-123456789abc"
 root:=t.TempDir()
 cfg:=filepath.Join(root,"config.yaml")
 db:=filepath.Join(root,"data","insula.db")
 if err:=os.MkdirAll(filepath.Dir(db),0700);err!=nil{t.Fatal(err)}
 configData:=fmt.Sprintf("storage:\n  root: %s\nvirtualization:\n  provider: kvm\n  libvirt_uri: qemu:///system\n",filepath.Join(root,"data"))
 if err:=os.WriteFile(cfg,[]byte(configData),0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(db)
 if err!=nil{t.Fatal(err)}
 entry:=registry.Entry{
  ID:id,LibvirtUUID:id,Name:"insula-qga-"+id,Profile:"alpine-virt",
  MAC:"52:54:00:22:33:44",SSHPort:22,InstanceDir:root,
 }
 if err:=store.Insert(context.Background(),entry);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(context.Background(),id,"");err!=nil{t.Fatal(err)}
 if err:=store.Close();err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 script:=`#!/bin/sh
case "$3" in
 list) printf '%s\n' "$MOCK_UUID" ;;
 domstate) printf 'running\n' ;;
 qemu-agent-command)
  case "$7" in
   *guest-exec-status*) printf '%s\n' '{"return":{"exited":true,"exitcode":0,"out-data":"AP9PSwo=","err-data":"RQBS"}}' ;;
   *guest-exec*) printf '%s\n' '{"return":{"pid":733}}' ;;
   *) exit 9 ;;
  esac ;;
 *) exit 10 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("MOCK_UUID",id)
 outFile,err:=os.CreateTemp(t.TempDir(),"stdout-*")
 if err!=nil{t.Fatal(err)}
 defer outFile.Close()
 errFile,err:=os.CreateTemp(t.TempDir(),"stderr-*")
 if err!=nil{t.Fatal(err)}
 defer errFile.Close()
 oldOut,oldErr:=os.Stdout,os.Stderr
 os.Stdout,os.Stderr=outFile,errFile
 defer func(){os.Stdout,os.Stderr=oldOut,oldErr}()
 runErr:=vmCommand([]string{"exec","-config",cfg,"-qga","-id",id,"binary"})
 if runErr!=nil{t.Fatalf("QGA CLI exec failed: %v",runErr)}
 if _,err:=outFile.Seek(0,io.SeekStart);err!=nil{t.Fatal(err)}
 if _,err:=errFile.Seek(0,io.SeekStart);err!=nil{t.Fatal(err)}
 out,err:=io.ReadAll(outFile);if err!=nil{t.Fatal(err)}
 stderr,err:=io.ReadAll(errFile);if err!=nil{t.Fatal(err)}
 if !bytes.Equal(out,[]byte{0,255,'O','K','\n'}){
  t.Fatalf("CLI stdout modified native bytes: %v",out)
 }
 if !bytes.Equal(stderr,[]byte{'E',0,'R'}){
  t.Fatalf("CLI stderr includes InsulaVM metadata: %v",stderr)
 }
 if strings.Contains(string(out),"Logs:")||strings.Contains(string(stderr),"Logs:"){
  t.Fatal("QGA CLI must not print log paths")
 }
 if _,err:=os.Stat(filepath.Join(root,"data","logs"));!os.IsNotExist(err){
  t.Fatalf("QGA created a persistent output log: %v",err)
 }
}
