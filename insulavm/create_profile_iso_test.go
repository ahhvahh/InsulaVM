package insulavm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// These tests exercise the public API through the real command-execution
// adapter using host-tool shims. They are NOT proof of real Debian installer
// boot; that requires the opt-in KVM integration suite.
func installerFixture(t *testing.T)(*Engine,string,string){
 t.Helper()
 engine:=testEngine(t,"ssh:\n  sudo_mode: passwordless\n")
 iso:=filepath.Join(t.TempDir(),"debian-13.iso")
 data:=make([]byte,40*1024)
 copy(data[16*2048+1:],"CD001")
 if err:=os.WriteFile(iso,data,0600);err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 argsLog:=filepath.Join(t.TempDir(),"virt-install-args")
 t.Setenv("INSULA_TEST_INSTALL_ARGS",argsLog)
 scripts:=map[string]string{
  "openssl":`test "$1" = passwd
echo '$6$mock$hash'`,
  "qemu-img":`case "$1" in
 create) printf 'QFI\373test-disk' > "$4" ;;
 info) echo '{"format":"qcow2"}' ;;
 *) exit 78 ;;
esac`,
  "virt-install":`printf '%s\n' "$@" > "$INSULA_TEST_INSTALL_ARGS"
test "$1" = --connect`,
  "virsh":`case "$3" in
 domstate) echo 'shut off' ;;
 undefine) echo 'domain undefined' ;;
 *) exit 79 ;;
esac`,
  "guestmount":`for last; do :; done
mkdir -p "$last/etc" "$last/usr/bin" "$last/usr/sbin" "$last/usr/share/initramfs-tools/scripts/live"
printf 'ID=debian\nVERSION_ID="13"\n' > "$last/etc/os-release"
touch "$last/usr/bin/cloud-init" "$last/usr/sbin/sshd" "$last/usr/bin/sudo"
touch "$last/usr/share/initramfs-tools/scripts/live/boot"`,
  "guestunmount":"exit 0",
 }
 for name,script:=range scripts{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte("#!/bin/sh\nset -eu\n"+script+"\n"),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 return engine,iso,argsLog
}

func TestCreateProfileFromISOPublishesOnlyVerifiedDebian(t *testing.T){
 engine,iso,argsLog:=installerFixture(t)
 ctx:=context.Background()
 var events []string
 result,err:=engine.CreateProfileFromISO(ctx,CreateProfileFromISORequest{
  Name:"debian-iso",ISOPath:iso,MemoryMiB:2048,VCPUs:2,DiskGB:12,
  OnProgress:func(p ProfileISOProgress){events=append(events,p.Stage)},
 })
 if err!=nil{t.Fatal(err)}
 if !result.Installed||result.Profile.Name!="debian-iso"||len(result.ISOChecksumSHA256)!=64{
  t.Fatalf("invalid result: %+v",result)
 }
 if _,err:=os.Stat(result.ImagePath);err!=nil{t.Fatal(err)}
 stored,err:=engine.GetProfile("debian-iso")
 if err!=nil||stored.ISO!=iso||stored.DiskGB!=12{t.Fatalf("bad published metadata: %+v %v",stored,err)}
 args,err:=os.ReadFile(argsLog)
 if err!=nil{t.Fatal(err)}
 for _,flag:=range []string{"--location\n"+iso,"--initrd-inject\n","--noautoconsole","--noreboot","--wait\n-1","format=qcow2,bus=virtio"}{
  if !strings.Contains(string(args),flag){t.Errorf("virt-install did not receive %q: %s",flag,args)}
 }
 if len(events)<4||events[len(events)-1]!="ready"{t.Errorf("progress missing success: %v",events)}
 temp,err:=filepath.Glob(filepath.Join(engine.config.Storage.Runs,"insula-install-*"))
 if err!=nil||len(temp)!=0{t.Fatalf("staging should be removed: %v %v",temp,err)}
 if _,err:=engine.CreateProfileFromISO(ctx,CreateProfileFromISORequest{Name:"debian-iso",ISOPath:iso,MemoryMiB:2048,VCPUs:2,DiskGB:12});err==nil{
  t.Fatal("duplicate installed profile was overwritten")
 }
}

func TestCreateProfileFromISORejectsUnsafeInput(t *testing.T){
 e,iso,_:=installerFixture(t)
 cases:=[]struct{name string;req CreateProfileFromISORequest}{
  {"missing name",CreateProfileFromISORequest{ISOPath:iso,MemoryMiB:2048,VCPUs:2,DiskGB:12}},
  {"relative ISO",CreateProfileFromISORequest{Name:"new",ISOPath:"relative.iso",MemoryMiB:2048,VCPUs:2,DiskGB:12}},
  {"insufficient ram",CreateProfileFromISORequest{Name:"new",ISOPath:iso,MemoryMiB:128,VCPUs:2,DiskGB:12}},
  {"invalid CPUs",CreateProfileFromISORequest{Name:"new",ISOPath:iso,MemoryMiB:2048,VCPUs:0,DiskGB:12}},
  {"small disk",CreateProfileFromISORequest{Name:"new",ISOPath:iso,MemoryMiB:2048,VCPUs:2,DiskGB:4}},
  {"negative timeout",CreateProfileFromISORequest{Name:"new",ISOPath:iso,MemoryMiB:2048,VCPUs:2,DiskGB:12,Timeout:-time.Second}},
  {"long timeout",CreateProfileFromISORequest{Name:"new",ISOPath:iso,MemoryMiB:2048,VCPUs:2,DiskGB:12,Timeout:7*time.Hour}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=e.CreateProfileFromISO(context.Background(),tc.req);err==nil{t.Fatal("unsafe installer request was accepted")}
  })
 }
 link:=filepath.Join(t.TempDir(),"link.iso")
 if err:=os.Symlink(iso,link);err!=nil{t.Fatal(err)}
 if _,err:=e.CreateProfileFromISO(context.Background(),CreateProfileFromISORequest{Name:"unsafe",ISOPath:link,MemoryMiB:2048,VCPUs:2,DiskGB:12});err==nil{
  t.Fatal("symlink ISO accepted")
 }
 invalid:=filepath.Join(t.TempDir(),"not-iso.iso")
 if err:=os.WriteFile(invalid,make([]byte,40000),0600);err!=nil{t.Fatal(err)}
 if _,err:=e.CreateProfileFromISO(context.Background(),CreateProfileFromISORequest{Name:"unsafe",ISOPath:invalid,MemoryMiB:2048,VCPUs:2,DiskGB:12});err==nil{
  t.Fatal("non-ISO accepted")
 }
 if _,err:=e.CreateProfileFromISO(nil,CreateProfileFromISORequest{});err==nil{t.Fatal("nil context accepted")}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=e.CreateProfileFromISO(ctx,CreateProfileFromISORequest{});!errors.Is(err,context.Canceled){t.Fatalf("canceled request: %v",err)}
}

func TestRenderDebianPreseedUsesEphemeralHashAndLockedRoot(t *testing.T){
 p:=renderDebianPreseed("insula","$6$test$mock",true)
 for _,want:=range []string{
  "d-i passwd/root-login boolean false",
  "d-i passwd/user-password-crypted password $6$test$mock",
  "d-i pkgsel/include string openssh-server sudo cloud-init",
  "d-i partman-auto/disk string /dev/vda",
  "NOPASSWD:ALL",
  "d-i debian-installer/exit/poweroff boolean true",
 }{
  if !strings.Contains(p,want){t.Fatalf("missing Debian preseed directive: %s",want)}
 }
 if strings.Contains(renderDebianPreseed("insula","$6$test$mock",false),"NOPASSWD:ALL"){
  t.Fatal("sudo privilege unexpectedly enabled")
 }
}

func TestExportVMToLiveISORejectsInvalidRequest(t *testing.T){
 e:=testEngine(t,"")
 if _,err:=e.ExportVMToLiveISO(nil,VMExportISORequest{});err==nil{t.Fatal("nil context accepted")}
 if _,err:=e.ExportVMToLiveISO(context.Background(),VMExportISORequest{});err==nil{t.Fatal("missing VMID/name accepted")}
 if _,err:=e.ExportVMToLiveISO(context.Background(),VMExportISORequest{VMID:"unknown",Name:"export",CommandTimeout:-time.Second});err==nil{t.Fatal("negative timeout accepted")}
 if _,err:=e.ExportVMToLiveISO(context.Background(),VMExportISORequest{VMID:"unknown",Name:"export"});err==nil{t.Fatal("unknown VM accepted")}
}
