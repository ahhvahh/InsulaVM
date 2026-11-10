package profile

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestFreezeFromOverlayNativeCompression(t *testing.T) {
 cases:=[]struct{
  name string
  compress bool
 }{
  {name:"legacy-uncompressed",compress:false},
  {name:"native-compressed",compress:true},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   root,overlay,original:=testFreezeBase(t)
   bin:=t.TempDir()
   capture:=filepath.Join(t.TempDir(),"qemu-args")
   script:=`#!/bin/sh
set -eu
case "$1" in
 convert)
  printf '%s\n' "$@" > "$INSULA_QEMU_ARGS"
  for last; do :; done
  printf 'QFI\373compressed-fixture' > "$last"
  ;;
 info) printf '{"format":"qcow2"}\n' ;;
 *) exit 17 ;;
esac
`
   if err:=os.WriteFile(filepath.Join(bin,"qemu-img"),[]byte(script),0700);err!=nil{t.Fatal(err)}
   t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
   t.Setenv("INSULA_QEMU_ARGS",capture)
   var image string
   var err error
   if tc.compress{
    image,err=FreezeFromOverlayWithOptions(context.Background(),root,original,tc.name,overlay,FreezeOptions{Compress:true})
   }else{
    image,err=FreezeFromOverlay(context.Background(),root,original,tc.name,overlay)
   }
   if err!=nil{t.Fatalf("freeze failed: %v",err)}
   args,err:=os.ReadFile(capture)
   if err!=nil{t.Fatal(err)}
   hasCompression:=false
   for _,arg:=range strings.Split(strings.TrimSpace(string(args)),"\n"){
    if arg=="-c"{hasCompression=true}
   }
   if hasCompression!=tc.compress{t.Fatalf("unexpected qemu-img -c for compress=%t: %s",tc.compress,args)}
   if _,err:=os.Stat(overlay);err!=nil{t.Fatalf("source overlay changed/removed: %v",err)}
   if _,err:=os.Stat(image);err!=nil{t.Fatalf("frozen disk not published: %v",err)}
   p,err:=Read(root,tc.name)
   if err!=nil{t.Fatal(err)}
   if _,err:=p.ImagePath();err!=nil{t.Fatalf("frozen QCOW2 not reusable: %v",err)}
  })
 }
}

func TestFreezeCompressedConvertFailurePreservesOverlay(t *testing.T){
 root,overlay,p:=testFreezeBase(t)
 bin:=t.TempDir()
 script:="#!/bin/sh\nset -eu\nif [ \"$1\" = convert ]; then echo compression-convert-error >&2; exit 42; fi\nexit 43\n"
 if err:=os.WriteFile(filepath.Join(bin,"qemu-img"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 _,err:=FreezeFromOverlayWithOptions(context.Background(),root,p,"failed-compression",overlay,FreezeOptions{Compress:true})
 if err==nil||!strings.Contains(err.Error(),"compression-convert-error"){t.Fatalf("conversion failure lost: %v",err)}
 if _,err:=os.Stat(overlay);err!=nil{t.Fatalf("source overlay lost: %v",err)}
 if _,err:=os.Stat(filepath.Join(root,"failed-compression"));!os.IsNotExist(err){t.Fatalf("partial profile survived: %v",err)}
}
