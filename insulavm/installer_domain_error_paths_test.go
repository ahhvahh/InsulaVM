package insulavm

import (
 "github.com/ahhvahh/InsulaVM/internal/guestfs"
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func fakeInstallerVirsh(t *testing.T,script string){
 t.Helper()
 bin:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte("#!/bin/sh\nset -eu\n"+script+"\n"),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestInstallerDomainVerificationAndSafeCleanup(t *testing.T){
 const name="insula-install-test"
 cases:=[]struct{name,script,checkErr,cleanupErr string}{
  {"stopped","case \"$3\" in domstate) echo 'shut off';; undefine) echo removed;; esac","",""},
  {"running","case \"$3\" in domstate) echo running;; destroy) echo killed;; undefine) echo removed;; esac","still exists","did not stop safely"},
  {"unknown gone","case \"$3\" in domstate) exit 7;; list) exit 0;; esac","read installer",""},
  {"unknown still listed","case \"$3\" in domstate) echo unavailable >&2; exit 7;; list) echo "+name+";; esac","read installer","exists but domstate failed"},
  {"unknown list broken","case \"$3\" in domstate) exit 7;; list) exit 17;; esac","read installer","cannot inspect Libvirt"},
  {"undefine failed","case \"$3\" in domstate) echo 'shut off';; undefine) echo prohibited >&2; exit 31;; esac","","remove installer"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   fakeInstallerVirsh(t,tc.script)
   check:=checkInstallerStopped(context.Background(),"qemu:///system",name)
   if tc.checkErr==""{
    if check!=nil{t.Fatalf("expected stopped installer: %v",check)}
   }else if check==nil||!strings.Contains(check.Error(),tc.checkErr){
    t.Fatalf("installer active/error state not reported (%q): %v",tc.checkErr,check)
   }
   cleanup:=cleanupISOInstallerDomain(context.Background(),"qemu:///system",name)
   if tc.cleanupErr==""{
    if cleanup!=nil{t.Fatalf("safe cleanup failed: %v",cleanup)}
   }else if cleanup==nil||!strings.Contains(cleanup.Error(),tc.cleanupErr){
    t.Fatalf("unsafe installer cleanup was accepted (%q): %v",tc.cleanupErr,cleanup)
   }
  })
 }
}

func TestInstallerCleanupMustStopVMBeforeRemovingDomain(t *testing.T){
 bin:=t.TempDir()
 state:=filepath.Join(bin,"state")
 if err:=os.WriteFile(state,[]byte("running"),0600);err!=nil{t.Fatal(err)}
 script:="#!/bin/sh\nset -eu\ncase \"$3\" in\n"+
  "domstate) if test -f \"$RUNNING_INSTALLER\"; then echo running; else echo 'shut off'; fi ;;\n"+
  "destroy) rm -f \"$RUNNING_INSTALLER\" ;;\n"+
  "undefine) test ! -f \"$RUNNING_INSTALLER\" || exit 93 ;;\n"+
  "*) exit 2 ;;\nesac\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("RUNNING_INSTALLER",state)
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 if err:=cleanupISOInstallerDomain(context.Background(),"qemu:///system","insula-install-test");err!=nil{
  t.Fatalf("installer cleanup should verify destruction before undefine: %v",err)
 }
 if _,err:=os.Stat(state);!os.IsNotExist(err){t.Fatal("installer was not stopped")}
}

func TestDebianInstallerDiskChecksRejectCorruptMetadata(t *testing.T){
 tests:=[]struct{name,info,want string}{
  {"qcow2","{\"format\":\"qcow2\"}",""},
  {"raw","{\"format\":\"raw\"}","standalone QCOW2"},
  {"backing image","{\"format\":\"qcow2\",\"backing-filename\":\"/secret/base.qcow2\"}","standalone QCOW2"},
  {"bad JSON","not-json","invalid character"},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   bin:=t.TempDir()
   script:="#!/bin/sh\nprintf '%s\\n' '"+tc.info+"'\n"
   if err:=os.WriteFile(filepath.Join(bin,"qemu-img"),[]byte(script),0700);err!=nil{t.Fatal(err)}
   t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
   err:=verifyStandaloneQCOW2(context.Background(),"/tmp/safe-test-image.qcow2")
   if tc.want==""{
    if err!=nil{t.Fatalf("standalone QCOW2 check failed: %v",err)}
   }else if err==nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("malformed image accepted (%q): %v",tc.want,err)
   }
  })
 }
}

func TestDebianInstallerVerificationRefusesInvalidGuest(t *testing.T){
 cases:=[]struct{name,guest,unmount,want string}{
  {"valid Debian13","mkdir -p \"$last/etc\" \"$last/usr/bin\" \"$last/usr/sbin\" \"$last/usr/share/initramfs-tools/scripts/live\"; echo 'ID=debian' > \"$last/etc/os-release\"; echo 'VERSION_ID=\"13\"' >> \"$last/etc/os-release\"; touch \"$last/usr/bin/cloud-init\" \"$last/usr/sbin/sshd\" \"$last/usr/bin/sudo\" \"$last/usr/share/initramfs-tools/scripts/live/boot\"","exit 0",""},
  {"missing os-release",":","exit 0","os-release"},
  {"wrong OS","mkdir -p \"$last/etc\"; echo 'ID=ubuntu' > \"$last/etc/os-release\"","exit 0","not Debian 13"},
  {"missing sshd","mkdir -p \"$last/etc\"; printf 'ID=debian\\nVERSION_ID=\"13\"\\n' > \"$last/etc/os-release\"","exit 0","missing required component"},
  {"unmount error","mkdir -p \"$last/etc\" \"$last/usr/bin\" \"$last/usr/sbin\" \"$last/usr/share/initramfs-tools/scripts/live\"; echo 'ID=debian' > \"$last/etc/os-release\"; echo 'VERSION_ID=\"13\"' >> \"$last/etc/os-release\"; touch \"$last/usr/bin/cloud-init\" \"$last/usr/sbin/sshd\" \"$last/usr/bin/sudo\" \"$last/usr/share/initramfs-tools/scripts/live/boot\"","echo 'guestunmount refused' >&2; exit 36","guest filesystem remains mounted"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   bin:=t.TempDir()
   guest:="#!/bin/sh\nset -eu\nfor last; do :; done\n"+tc.guest+"\n"
   if err:=os.WriteFile(filepath.Join(bin,"guestmount"),[]byte(guest),0700);err!=nil{t.Fatal(err)}
   if err:=os.WriteFile(filepath.Join(bin,"guestunmount"),[]byte("#!/bin/sh\n"+tc.unmount+"\n"),0700);err!=nil{t.Fatal(err)}
   t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
   err:=verifyDebianInstalledDisk(context.Background(),"/tmp/installed.qcow2",t.TempDir())
   if tc.want==""{
    if err!=nil{t.Fatalf("valid Debian guest rejected: %v",err)}
   }else if err==nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("guest validation should report %q: %v",tc.want,err)
   }
   if tc.name=="unmount error"&&!errors.Is(err,guestfs.ErrUnmount){
    t.Fatalf("source overlay might be deleted while still mounted: %v",err)
   }
  })
 }
}
