package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestFreezeVMPropagatesNativeQCOW2Compression(t *testing.T){
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
  VMID:coverageFrozenVMID,Name:"compressed-profile",
  CompressQCOW2:true,AllowStopped:true,SkipGuestCleanup:true,
 })
 if err!=nil{t.Fatal(err)}
 if result==nil||!result.SourceStopped||!result.SourceCleaned||result.ImagePath==""{
  t.Fatalf("freeze did not finish safely: %+v",result)
 }
 args,err:=os.ReadFile(capture)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains("\n"+string(args)+"\n","\n-c\n"){t.Fatalf("qemu-img was not called with native compression: %s",args)}
 saved,err:=e.GetProfile("compressed-profile")
 if err!=nil{t.Fatal(err)}
 if image,err:=saved.ImagePath();err!=nil||image!=result.ImagePath{t.Fatalf("invalid reusable compressed base: %q %v",image,err)}
 if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("transient VM was not cleaned: %v",err)}
 if _,err:=os.Stat(overlay);!os.IsNotExist(err){t.Fatalf("source overlay still exists after success: %v",err)}
}
