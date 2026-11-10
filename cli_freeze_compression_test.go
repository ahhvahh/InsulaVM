package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestCLIFreezeNativeQCOW2CompressionModes(t *testing.T){
 for _,tc:=range []struct{
  name string
  compress bool
 }{
  {name:"default-compressed",compress:true},
  {name:"explicit-uncompressed",compress:false},
 }{
  t.Run(tc.name,func(t *testing.T){
   config,engine,_:=cliStoppedVMForFreeze(t)
   bin:=t.TempDir()
   capture:=filepath.Join(t.TempDir(),"convert-args")
   script:=`#!/bin/sh
set -eu
case "$1" in
 convert)
  printf '%s\n' "$@" > "$INSULA_QEMU_ARGS"
  for last; do :; done
  printf 'QFI\373standalone' > "$last"
  ;;
 info) printf '{"format":"qcow2"}\n' ;;
 *) exit 17 ;;
esac
`
   if err:=os.WriteFile(filepath.Join(bin,"qemu-img"),[]byte(script),0700);err!=nil{t.Fatal(err)}
   t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
   t.Setenv("INSULA_QEMU_ARGS",capture)
   args:=[]string{"vm","freeze","-config",config,"-id",cliLoopbackID,
    "-name","reusable","-skip-guest-cleanup","-recover-stopped"}
   if !tc.compress {args=append(args,"-compress=false")}
   if err:=run(args);err!=nil{t.Fatalf("CLI freeze failed: %v",err)}
   output,err:=os.ReadFile(capture)
   if err!=nil{t.Fatal(err)}
   hasCompression:=strings.Contains("\n"+string(output)+"\n","\n-c\n")
   if hasCompression!=tc.compress{t.Fatalf("compress=%t, qemu-img args=%s",tc.compress,output)}
   p,err:=engine.GetProfile("reusable")
   if err!=nil{t.Fatal(err)}
   if _,err:=p.ImagePath();err!=nil{t.Fatalf("CLI output not reusable: %v",err)}
  })
 }
}
