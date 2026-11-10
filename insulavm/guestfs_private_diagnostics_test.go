package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

// When the private-kernel retry fails, preserve the second attempt's real
// libguestfs error rather than reporting only a generic supermin status.
func TestGuestfsPrivateKernelFailureReportsSecondDiagnostic(t *testing.T) {
 bin:=t.TempDir()
 guestfsStub(t,bin,"guestmount",`
if [ -n "${SUPERMIN_KERNEL:-}" ]; then
 if [ "${LIBGUESTFS_DEBUG:-}" = 1 ]; then
  echo 'supermin: detailed private kernel failure reason from appliance builder' >&2
 fi
 echo 'libguestfs: error: /usr/bin/supermin exited with error status 1' >&2
 exit 49
fi
if [ "${LIBGUESTFS_DEBUG:-}" = 1 ]; then
 echo "cp: cannot open '/boot/vmlinuz-7.0.0-1017-azure' for reading: Permission denied" >&2
fi
echo 'libguestfs: error: /usr/bin/supermin exited with error status 1' >&2
exit 1`)
 guestfsStub(t,bin,"apt-cache",`case "$1:$2" in
 depends:linux-image-generic) printf 'linux-image-generic\n Depends: linux-image-6.8.0-99-generic\n' ;;
 show:linux-modules-6.8.0-99-generic) echo 'Package: linux-modules-6.8.0-99-generic' ;;
 *) exit 17 ;;
esac`)
 guestfsStub(t,bin,"apt-get",`test "$1" = download
case "$2" in
 linux-image-6.8.0-99-generic|linux-modules-6.8.0-99-generic) touch "$2.deb" ;;
 *) exit 21 ;;
esac`)
 guestfsStub(t,bin,"dpkg-deb",`test "$1" = -x
case "$2" in
 *linux-image-*) mkdir -p "$3/boot"; printf kernel > "$3/boot/vmlinuz-6.8.0-99-generic" ;;
 *linux-modules-*) mkdir -p "$3/lib/modules/6.8.0-99-generic" ;;
 *) exit 22 ;;
esac`)
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("SUPERMIN_KERNEL","")
 t.Setenv("SUPERMIN_MODULES","")
 mountpoint:=filepath.Join(t.TempDir(),"mount")
 if err:=os.Mkdir(mountpoint,0700);err!=nil{t.Fatal(err)}
 cleanup,err:=mountGuestDiskReadOnly(context.Background(),"/tmp/guest.qcow2",mountpoint)
 if err==nil||cleanup!=nil||!strings.Contains(err.Error(),"detailed private kernel failure reason") {
  t.Fatalf("second supermin failure was not diagnosed: cleanup=%v err=%v",cleanup!=nil,err)
 }
}
