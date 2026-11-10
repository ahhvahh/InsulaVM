package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestFreezeVMNativeQCOW2CompressionModes(t *testing.T) {
 cases:=[]struct {
  name string
  compress bool
 }{
  {name:"default-compressed",compress:true},
  {name:"explicit-uncompressed",compress:false},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   e,dir,overlay,bin:=stoppedProfileFixture(t)
   capture:=filepath.Join(t.TempDir(),"convert-args")
   writeISOTool(t,bin,"qemu-img",`case "$1" in
 convert)
  printf '%s\n' "$@" > "$INSULA_QEMU_ARGS"
  for last; do :; done
  printf 'QFI\373standalone' > "$last"
  ;;
 info) printf '{"format":"qcow2"}\n' ;;
 *) exit 17 ;;
esac`)
   t.Setenv("INSULA_QEMU_ARGS",capture)
   result,err:=e.FreezeVM(context.Background(),VMFreezeRequest{
    VMID:coverageFrozenVMID,Name:"reusable",
    DisableQCOW2Compression:!tc.compress,
    AllowStopped:true,SkipGuestCleanup:true,
   })
   if err!=nil{t.Fatal(err)}
   if result==nil||!result.SourceStopped||!result.SourceCleaned||result.ImagePath==""{
    t.Fatalf("freeze did not finish safely: %+v",result)
   }
   args,err:=os.ReadFile(capture)
   if err!=nil{t.Fatal(err)}
   hasCompression:=strings.Contains("\n"+string(args)+"\n","\n-c\n")
   if hasCompression!=tc.compress{t.Fatalf("compress=%t, qemu-img args=%s",tc.compress,args)}
   saved,err:=e.GetProfile("reusable")
   if err!=nil{t.Fatal(err)}
   if image,err:=saved.ImagePath();err!=nil||image!=result.ImagePath{
    t.Fatalf("invalid reusable QCOW2 base: %q %v",image,err)
   }
   if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("transient VM was not cleaned: %v",err)}
   if _,err:=os.Stat(overlay);!os.IsNotExist(err){t.Fatalf("source overlay still exists after success: %v",err)}
  })
 }
}
