package guestfs

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

// The kernel fallback must not chmod /boot or install packages on the host.
// It selects and extracts a private kernel only after a specific supermin
// diagnostic confirms the host kernel could not be read.
func TestGuestfsMountRecoversUnreadableHostKernel(t *testing.T){
 bin:=t.TempDir()
 guestfsStub(t,bin,"guestmount",`
if [ -n "${SUPERMIN_KERNEL:-}" ] && [ -r "$SUPERMIN_KERNEL" ] && [ -d "${SUPERMIN_MODULES:-/missing}" ]; then
  printf '%s\n' "$SUPERMIN_KERNEL" > "$INSULA_TEST_KERNEL_PATH"
  exit 0
fi
if [ "${LIBGUESTFS_DEBUG:-}" = 1 ]; then
  echo 'supermin: kernel: picked vmlinuz /boot/vmlinuz-7.0.0-1017-azure' >&2
  echo "cp: cannot open '/boot/vmlinuz-7.0.0-1017-azure' for reading: Permission denied" >&2
fi
echo 'libguestfs: error: /usr/bin/supermin exited with error status 1.' >&2
exit 1`)
 guestfsExtractedKernelStubs(t, bin)
 log:=filepath.Join(t.TempDir(),"kernel")
 t.Setenv("INSULA_TEST_KERNEL_PATH",log)
 t.Setenv("SUPERMIN_KERNEL","")
 t.Setenv("SUPERMIN_MODULES","")
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 cleanup,err:=MountReadOnly(context.Background(),"/tmp/guest.qcow2",t.TempDir())
 if err!=nil||cleanup==nil{t.Fatalf("failed private kernel recovery: %v",err)}
 pathBytes,err:=os.ReadFile(log)
 if err!=nil{t.Fatal(err)}
 kernel:=strings.TrimSpace(string(pathBytes))
 if _,err:=os.Stat(kernel);err!=nil{t.Fatalf("private kernel unavailable before unmount: %v",err)}
 cleanup()
 if _,err:=os.Stat(kernel);!errors.Is(err,os.ErrNotExist){t.Fatalf("temporary kernel not removed: %v",err)}
}

func TestGuestfsMountDoesNotTreatUnrelatedPermissionFailureAsKernelError(t *testing.T){
 bin:=t.TempDir()
 marker:=filepath.Join(t.TempDir(),"apt-invoked")
 t.Setenv("INSULA_TEST_APT_MARKER",marker)
 guestfsStub(t,bin,"guestmount",`if [ "${LIBGUESTFS_DEBUG:-}" = 1 ]; then
 echo "cp: cannot open '/etc/passwd' for reading: Permission denied" >&2
fi
echo 'libguestfs: error: /usr/bin/supermin exited with error status 1' >&2
exit 1`)
 guestfsStub(t,bin,"apt-get",`touch "$INSULA_TEST_APT_MARKER"; exit 1`)
 t.Setenv("SUPERMIN_KERNEL","")
 t.Setenv("SUPERMIN_MODULES","")
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 cleanup,err:=MountReadOnly(context.Background(),"/tmp/guest.qcow2",t.TempDir())
 if err==nil||cleanup!=nil||!strings.Contains(err.Error(),"Permission denied"){
  t.Fatalf("unrelated access failure should not trigger recovery: cleanup=%v err=%v",cleanup!=nil,err)
 }
 if _,err:=os.Stat(marker);!errors.Is(err,os.ErrNotExist){t.Fatalf("unrelated failure invoked APT: %v",err)}
}

func TestGuestfsMountRespectsExplicitSuperminConfiguration(t *testing.T){
 bin:=t.TempDir()
 marker:=filepath.Join(t.TempDir(),"apt-invoked")
 t.Setenv("INSULA_TEST_APT_MARKER",marker)
 guestfsStub(t,bin,"guestmount",`if [ "${LIBGUESTFS_DEBUG:-}" = 1 ]; then
 echo "cp: cannot open '/boot/vmlinuz-7.0.0-1017-azure' for reading: Permission denied" >&2
fi
echo 'libguestfs: error: /usr/bin/supermin exited with error status 1' >&2
exit 1`)
 guestfsStub(t,bin,"apt-get",`touch "$INSULA_TEST_APT_MARKER"; exit 1`)
 t.Setenv("SUPERMIN_KERNEL","/configured/kernel")
 t.Setenv("SUPERMIN_MODULES","/configured/modules")
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 cleanup,err:=MountReadOnly(context.Background(),"/tmp/guest.qcow2",t.TempDir())
 if err==nil||cleanup!=nil||!strings.Contains(err.Error(),"explicitly configured"){
  t.Fatalf("explicit supermin settings must not be overridden: cleanup=%v err=%v",cleanup!=nil,err)
 }
 if _,err:=os.Stat(marker);!errors.Is(err,os.ErrNotExist){t.Fatalf("explicit settings caused unexpected APT call: %v",err)}
}
