package insulavm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func writeISOTool(t *testing.T,dir,name,script string){
 t.Helper()
 if err:=os.WriteFile(filepath.Join(dir,name),[]byte("#!/bin/sh\nset -eu\n"+script+"\n"),0700);err!=nil{t.Fatal(err)}
}

// The ISO toolchain is stubbed only here. The real ISO must still pass the
// KVM boot + SSH + guest script acceptance in scripts/tests/integration.sh.
func TestExportProfileLiveISOStagesAndNoOverwrite(t *testing.T){
 e:=testEngine(t,"")
 base:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(base,[]byte{'Q','F','I',0xfb,0,0,0,0},0600);err!=nil{t.Fatal(err)}
 if err:=e.RegisterProfile(Profile{Name:"iso-test",MemoryMiB:1024,VCPUs:1},base);err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 toolset:=map[string]string{
  "guestmount": `for last; do :; done
mkdir -p "$last/boot"
printf kernel > "$last/boot/vmlinuz-6.12-test"
printf initrd > "$last/boot/initrd.img-6.12-test"`,
  "guestunmount":"exit 0",
  "lsinitramfs":"echo scripts/live",
  "mksquashfs":`test -r "$1/boot/vmlinuz-6.12-test"
printf squashfs > "$2"`,
  "grub-mkrescue":`test "$1" = -o
test -s "$3/live/filesystem.squashfs"
test -s "$3/live/vmlinuz"
test -s "$3/live/initrd.img"
test -r "$3/boot/grub/grub.cfg"
printf fake-iso-for-unit-test > "$2"`,
 }
 for name,body:=range toolset{writeISOTool(t,bin,name,body)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 iso,err:=e.ExportProfileLiveISO(context.Background(),"iso-test")
 if err!=nil{t.Fatal(err)}
 if !strings.HasSuffix(iso,"/exports/iso-test-live.iso"){t.Fatalf("unexpected exported path: %s",iso)}
 contents,err:=os.ReadFile(iso)
 if err!=nil||string(contents)!="fake-iso-for-unit-test"{
  t.Fatalf("ISO mock not published: %q %v",contents,err)
 }
 if _,err:=e.ExportProfileLiveISO(context.Background(),"iso-test");err==nil{
  t.Fatal("export unexpectedly overwrote existing image")
 }
}

func TestExportProfileLiveISORejectsMissingLiveBoot(t *testing.T){
 e:=testEngine(t,"")
 base:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(base,[]byte{'Q','F','I',0xfb,0,0,0,0},0600);err!=nil{t.Fatal(err)}
 if err:=e.RegisterProfile(Profile{Name:"no-live",MemoryMiB:1024,VCPUs:1},base);err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 toolset:=map[string]string{
  "guestmount":`for last; do :; done
mkdir -p "$last/boot"
printf kernel > "$last/boot/vmlinuz-6.12-test"
printf initrd > "$last/boot/initrd.img-6.12-test"`,
  "guestunmount":"exit 0",
  "lsinitramfs":"echo usr/sbin/init",
  "mksquashfs":"exit 88",
  "grub-mkrescue":"exit 89",
 }
 for name,body:=range toolset{writeISOTool(t,bin,name,body)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 iso,err:=e.ExportProfileLiveISO(context.Background(),"no-live")
 if err==nil||!strings.Contains(err.Error(),"live-boot")||iso!=""{
  t.Fatalf("unsafe export accepted: iso=%s err=%v",iso,err)
 }
 target:=filepath.Join(e.config.Storage.Profiles,"no-live","exports","no-live-live.iso")
 if _,err:=os.Stat(target);!errors.Is(err,os.ErrNotExist){
  t.Fatalf("published an ISO without live-boot: %v",err)
 }
}
