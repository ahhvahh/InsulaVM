package profile

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestFreezeDirectBootInstallerDiskCompressed(t *testing.T) {
 bin:=t.TempDir()
 cmd:=`#!/bin/sh
set -eu
case "$1" in
 convert)
  printf '%s\n' "$@" > "$MOCK_QEMU_ARGS"
  shift
  while [ "$#" -gt 2 ]; do shift; done
  cp "$1" "$2"
  ;;
 info) printf '{"format":"qcow2"}\n' ;;
 *) exit 21 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"qemu-img"),[]byte(cmd),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 argsFile:=filepath.Join(t.TempDir(),"qemu-args")
 t.Setenv("MOCK_QEMU_ARGS",argsFile)
 source:=filepath.Join(t.TempDir(),"installer.qcow2")
 if err:=os.WriteFile(source,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 root:=t.TempDir()
 p:=Profile{Name:"alpine-virt",OS:"alpine",MemoryMiB:500,VCPUs:1,DiskGB:8,BootMode:"direct"}
 image,err:=FreezeFromOverlayWithOptions(context.Background(),root,p,"abioma-tests",source,FreezeOptions{Compress:true})
 if err!=nil{t.Fatal(err)}
 saved,err:=Read(root,"abioma-tests")
 if err!=nil{t.Fatal(err)}
 if saved.BootMode!="direct"||saved.MemoryMiB!=500||saved.VCPUs!=1||saved.SSH.User!=""{
  t.Fatalf("invalid direct boot profile: %+v",saved)
 }
 args,err:=os.ReadFile(argsFile)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains("\n"+string(args)+"\n","\n-c\n"){t.Fatalf("compression flag absent: %s",args)}
 if _,err:=os.Stat(source);err!=nil{t.Fatalf("source installer disk missing: %v",err)}
 if image!=filepath.Join(root,"abioma-tests","versions","v001","base.qcow2"){t.Fatalf("wrong image path: %s",image)}
}
