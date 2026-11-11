package profile

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func mockQEMUImageForFreeze(t *testing.T, infoJSON string, failConvert bool) {
 t.Helper()
 bin:=t.TempDir()
 script:="#!/bin/sh\nset -eu\n"+
  "case \"$1\" in\n"+
  "  convert)\n"+
  "    if [ \"$MOCK_QEMU_FAIL\" = 1 ]; then exit 14; fi\n"+
  "    cp \"$6\" \"$7\" ;;\n"+
  "  info) printf '%s\\n' \"$MOCK_QEMU_INFO\" ;;\n"+
  "  *) exit 15 ;;\n"+
  "esac\n"
 if err:=os.WriteFile(filepath.Join(bin,"qemu-img"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("MOCK_QEMU_INFO",infoJSON)
 if failConvert{t.Setenv("MOCK_QEMU_FAIL","1")}else{t.Setenv("MOCK_QEMU_FAIL","0")}
}

func testFreezeBase(t *testing.T)(string,string,Profile){
 t.Helper()
 root:=t.TempDir()
 overlay:=filepath.Join(t.TempDir(),"overlay.qcow2")
 if err:=os.WriteFile(overlay,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 p:=Profile{Name:"original",Version:"v001",Image:"versions/v001/base.qcow2",
  OS:"debian",MemoryMiB:2048,VCPUs:2,DiskGB:8,
  SSH:SSH{User:"insula",PrivateKey:"/secret/old",KnownHosts:"/secret/hosts"},
 }
 return root,overlay,p
}

func TestFreezeFromOverlayCreatesStandaloneReusableProfile(t *testing.T){
 root,overlay,p:=testFreezeBase(t)
 p.Guest=Guest{Destination:"/srv/frozen-workspace"}
 mockQEMUImageForFreeze(t,`{"format":"qcow2"}`,false)
 image,err:=FreezeFromOverlay(context.Background(),root,p,"my-build-v1",overlay)
 if err!=nil{t.Fatal(err)}
 if !strings.HasSuffix(image,"my-build-v1/versions/v001/base.qcow2"){t.Fatalf("unexpected image %s",image)}
 saved,err:=Read(root,"my-build-v1")
 if err!=nil{t.Fatal(err)}
 if saved.MemoryMiB!=2048||saved.VCPUs!=2||saved.Name!="my-build-v1"{
  t.Fatalf("profile changed unexpectedly: %+v",saved)
 }
 if saved.Guest.Destination!="/srv/frozen-workspace"{t.Fatalf("frozen profile lost guest workspace: %+v",saved.Guest)}
 if saved.SSH.PrivateKey!=""||saved.SSH.KnownHosts!=""||saved.SSH.User!="insula"{
  t.Fatalf("source identity leaked into new profile: %+v",saved.SSH)
 }
 if _,err:=os.Stat(overlay);err!=nil{t.Fatalf("freeze destroyed source overlay: %v",err)}
 if _,err:=FreezeFromOverlay(context.Background(),root,p,"my-build-v1",overlay);err==nil{
  t.Fatal("overwrote previously frozen image")
 }
}

func TestFreezeFromOverlayFailFastAndRollback(t *testing.T){
 root,overlay,p:=testFreezeBase(t)
 mockQEMUImageForFreeze(t,`{"format":"qcow2"}`,true)
 _,err:=FreezeFromOverlay(context.Background(),root,p,"broken",overlay)
 if err==nil{t.Fatal("conversion error lost")}
 if _,err:=os.Stat(filepath.Join(root,"broken"));!os.IsNotExist(err){
  t.Fatalf("partial frozen profile persisted: %v",err)
 }
 if _,err:=os.Stat(overlay);err!=nil{t.Fatalf("source overlay was deleted: %v",err)}
}

func TestFreezeFromOverlayRejectsBackingAndTraversal(t *testing.T){
 root,overlay,p:=testFreezeBase(t)
 mockQEMUImageForFreeze(t,`{"format":"qcow2","backing-filename":"/old/disk.qcow2"}`,false)
 if _,err:=FreezeFromOverlay(context.Background(),root,p,"unsafe",overlay);err==nil{
  t.Fatal("accepted image dependent on backing file")
 }
 if _,err:=os.Stat(filepath.Join(root,"unsafe"));!os.IsNotExist(err){t.Fatalf("profile remained: %v",err)}
 if _,err:=FreezeFromOverlay(context.Background(),root,p,"../escape",overlay);err==nil{
  t.Fatal("accepted traversal as profile name")
 }
}
