package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestCLICompressedFreezeUsesNativeQEMU(t *testing.T){
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
 err:=run([]string{"vm","freeze","-config",config,"-id",cliLoopbackID,
  "-name","compressed-reusable","-export-iso=false","-compress",
  "-skip-guest-cleanup","-recover-stopped"})
 if err!=nil{t.Fatalf("CLI compressed freeze failed: %v",err)}
 args,err:=os.ReadFile(capture)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains("\n"+string(args)+"\n","\n-c\n"){t.Fatalf("CLI did not enable qemu-img -c: %s",args)}
 p,err:=engine.GetProfile("compressed-reusable")
 if err!=nil{t.Fatal(err)}
 if _,err:=p.ImagePath();err!=nil{t.Fatalf("CLI output is not a valid reusable QCOW2 profile: %v",err)}
}
