package guestfs

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func guestfsExtractedKernelStubs(t *testing.T,bin string){
 t.Helper()
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
 guestfsStub(t,bin,"depmod",`test "$1" = -b
test "$3" = 6.8.0-99-generic
test -d "$2/lib/modules/$3"
printf 'text dependencies' > "$2/lib/modules/$3/modules.dep"
printf 'binary dependencies' > "$2/lib/modules/$3/modules.dep.bin"`)
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPrepareGuestfsKernelRunsDepmodInPrivateDirectory(t *testing.T){
 bin:=t.TempDir()
 guestfsExtractedKernelStubs(t,bin)
 log:=filepath.Join(t.TempDir(),"depmod-path")
 t.Setenv("INSULA_TEST_DEPMOD_PATH",log)
 guestfsStub(t,bin,"depmod",`test "$1" = -b
test "$3" = 6.8.0-99-generic
test -d "$2/lib/modules/$3"
printf '%s' "$2" > "$INSULA_TEST_DEPMOD_PATH"
printf 'kernel/module.ko: kernel/dependency.ko\n' > "$2/lib/modules/$3/modules.dep"
printf 'binary dependencies' > "$2/lib/modules/$3/modules.dep.bin"`)
 env,cleanup,err:=prepareGuestfsKernel(context.Background())
 if err!=nil||cleanup==nil||len(env)!=3{t.Fatalf("private depmod preparation failed: env=%v err=%v",env,err)}
 rootBytes,err:=os.ReadFile(log)
 if err!=nil{t.Fatal(err)}
 root:=string(rootBytes)
 if root==""||!strings.Contains(env[1],root){t.Fatalf("depmod escaped private tree: root=%q env=%v",root,env)}
 index:=filepath.Join(root,"lib","modules","6.8.0-99-generic","modules.dep")
 contents,err:=os.ReadFile(index)
 if err!=nil||!strings.Contains(string(contents),"kernel/module.ko"){
  t.Fatalf("missing generated modules.dep: %v %q",err,contents)
 }
 if _,err:=os.Stat(index+".bin");err!=nil{t.Fatalf("missing modules.dep.bin: %v",err)}
 cleanup()
 if _,err:=os.Stat(root);!errors.Is(err,os.ErrNotExist){t.Fatalf("private kernel staging not removed: %v",err)}
}

func TestPrepareGuestfsKernelRejectsDepmodFailures(t *testing.T){
 cases:=[]struct{name,script,want string}{
  {"failed depmod","echo 'depmod rejected private modules' >&2; exit 67","depmod rejected private modules"},
  {"missing index despite successful command","exit 0","did not generate modules.dep"},
  {"missing binary index","printf text > \"$2/lib/modules/$3/modules.dep\"","did not generate modules.dep.bin"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   bin:=t.TempDir()
   guestfsExtractedKernelStubs(t,bin)
   guestfsStub(t,bin,"depmod",tc.script)
   env,cleanup,err:=prepareGuestfsKernel(context.Background())
   if err==nil||cleanup!=nil||env!=nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("invalid kernel module index accepted: env=%v cleanup=%v err=%v",env,cleanup!=nil,err)
   }
  })
 }
}


func TestPrepareGuestfsKernelHandlesUsrLibModules(t *testing.T) {
 bin:=t.TempDir()
 guestfsExtractedKernelStubs(t,bin)
 guestfsStub(t,bin,"dpkg-deb",`test "$1" = -x
case "$2" in
 *linux-image-*) mkdir -p "$3/boot"; printf kernel > "$3/boot/vmlinuz-6.8.0-99-generic" ;;
 *linux-modules-*) mkdir -p "$3/usr/lib/modules/6.8.0-99-generic" ;;
 *) exit 22 ;;
esac`)
 guestfsStub(t,bin,"depmod",`test "$1" = -b
test "$3" = -m
test "$4" = /usr/lib/modules
test "$5" = 6.8.0-99-generic
test -d "$2/usr/lib/modules/$5"
printf 'text dependencies' > "$2/usr/lib/modules/$5/modules.dep"
printf 'binary dependencies' > "$2/usr/lib/modules/$5/modules.dep.bin"`)
 env, cleanup, err:=prepareGuestfsKernel(context.Background())
 if err!=nil||cleanup==nil||len(env)!=3{t.Fatalf("usr/lib private modules failure: env=%v err=%v",env,err)}
 defer cleanup()
 if !strings.Contains(env[1],"/usr/lib/modules/6.8.0-99-generic"){
  t.Fatalf("wrong module directory: %v",env)
 }
 if _,err:=os.Stat(strings.TrimPrefix(env[1],"SUPERMIN_MODULES=")+"/modules.dep.bin");err!=nil{
  t.Fatalf("missing usr/lib binary index: %v",err)
 }
}
