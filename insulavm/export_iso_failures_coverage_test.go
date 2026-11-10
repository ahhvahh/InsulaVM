package insulavm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func isoFailureFixture(t *testing.T)(*Engine,string,string){
 t.Helper()
 e:=testEngine(t,"")
 base:=filepath.Join(t.TempDir(),"installed.qcow2")
 if err:=os.WriteFile(base,[]byte{'Q','F','I',0xfb,0,0,0,3},0600);err!=nil{t.Fatal(err)}
 if err:=e.RegisterProfile(Profile{Name:"isotest",MemoryMiB:1024,VCPUs:1,DiskGB:8},base);err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 return e,bin,filepath.Join(t.TempDir(),"result.iso")
}

func isoToolScripts(t *testing.T,bin string,overrides map[string]string){
 t.Helper()
 scripts:=map[string]string{
  "guestmount":"for last; do :; done\nmkdir -p \"$last/boot\"\n"+
   "printf kernel > \"$last/boot/vmlinuz-6.12-test\"\n"+
   "printf initrd > \"$last/boot/initrd.img-6.12-test\"",
  "guestunmount":"exit 0",
  "lsinitramfs":"echo scripts/live",
  "mksquashfs":"printf squashfs > \"$2\"",
  "grub-mkrescue":"printf iso > \"$2\"",
 }
 for name,body:=range overrides{scripts[name]=body}
 for name,body:=range scripts{
  if err:=os.WriteFile(filepath.Join(bin,name),[]byte("#!/bin/sh\nset -eu\n"+body+"\n"),0700);err!=nil{t.Fatal(err)}
 }
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestISOExportReportsExactFailedStageWithoutPublishing(t *testing.T){
 cases:=[]struct{
  name,tool,script,expected string
 }{
  {"guestmount","guestmount","echo mount-rejected >&2; exit 39","mount-rejected"},
  {"missing-kernel","guestmount","for last; do :; done; mkdir -p \"$last/boot\"","matching /boot/vmlinuz"},
  {"missing-initrd","guestmount","for last; do :; done; mkdir -p \"$last/boot\"; touch \"$last/boot/vmlinuz-6.12-test\"","matching /boot/vmlinuz"},
  {"invalid-initramfs","lsinitramfs","echo initramfs-rejected >&2; exit 51","inspect live initramfs"},
  {"squashfs-failed","mksquashfs","echo squashfs-rejected >&2; exit 53","squashfs-rejected"},
  {"grub-failed","grub-mkrescue","echo grub-rejected >&2; exit 55","grub-rejected"},
  {"empty-ISO","grub-mkrescue",": > \"$2\"","empty or invalid ISO"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   e,bin,destination:=isoFailureFixture(t)
   isoToolScripts(t,bin,map[string]string{tc.tool:tc.script})
   got,err:=e.ExportProfileLiveISOTo(context.Background(),"isotest",destination)
   if err==nil||got!=""||!strings.Contains(err.Error(),tc.expected){
    t.Fatalf("ISO export did not report %q: iso=%q err=%v",tc.expected,got,err)
   }
   if _,err:=os.Lstat(destination);!os.IsNotExist(err){
    t.Fatalf("failed export published ISO destination: %v",err)
   }
   p,err:=e.GetProfile("isotest")
   if err!=nil{t.Fatal(err)}
   if _,err:=p.ImagePath();err!=nil{t.Fatalf("failed ISO export damaged installed profile: %v",err)}
  })
 }
}

func TestISODestinationRejectsSymlinksAndInvalidNames(t *testing.T){
 e,bin,destination:=isoFailureFixture(t)
 isoToolScripts(t,bin,nil)
 if _,err:=e.liveISODestination("../escape",destination);err==nil{t.Fatal("unsafe profile name accepted")}
 if _,err:=e.liveISODestination("isotest","file.tar.gz");err==nil{t.Fatal("invalid ISO extension accepted")}
 if err:=checkISODestinationVacant("");err==nil{t.Fatal("empty ISO destination accepted")}
 existing:=filepath.Join(t.TempDir(),"existing.iso")
 if err:=os.WriteFile(existing,[]byte("original"),0600);err!=nil{t.Fatal(err)}
 if _,err:=e.ExportProfileLiveISOTo(context.Background(),"isotest",existing);err==nil{
  t.Fatal("existing ISO was overwritten")
 }
 original,err:=os.ReadFile(existing)
 if err!=nil||string(original)!="original"{t.Fatal("existing ISO bytes were modified")}
 link:=filepath.Join(t.TempDir(),"linked.iso")
 if err:=os.Symlink(existing,link);err!=nil{t.Fatal(err)}
 if _,err:=e.ExportProfileLiveISOTo(context.Background(),"isotest",link);err==nil{
  t.Fatal("symlink ISO destination was accepted")
 }
 if _,err:=e.ExportProfileLiveISOTo(context.Background(),"missing",destination);err==nil{
  t.Fatal("missing profile accepted")
 }
 if _,err:=e.ExportProfileLiveISOTo(nil,"isotest",destination);err==nil{
  t.Fatal("nil context accepted")
 }
 cancelled,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=e.ExportProfileLiveISOTo(cancelled,"isotest",destination);!errors.Is(err,context.Canceled){
  t.Fatalf("cancelled ISO export accepted: %v",err)
 }
}

func TestLiveISOSelectionPrefersNewestMatchingKernel(t *testing.T){
 root:=t.TempDir()
 boot:=filepath.Join(root,"boot")
 if err:=os.MkdirAll(boot,0700);err!=nil{t.Fatal(err)}
 kernel,initrd,err:=selectLiveKernel(root)
 if err==nil||kernel!=""||initrd!=""{t.Fatal("missing boot files accepted")}
 for _,version:=range []string{"6.1.0-test","6.12.0-test"}{
  for _,file:=range []string{"vmlinuz-"+version,"initrd.img-"+version}{
   if err:=os.WriteFile(filepath.Join(boot,file),[]byte("kernel-test"),0600);err!=nil{t.Fatal(err)}
  }
 }
 kernel,initrd,err=selectLiveKernel(root)
 if err!=nil||!strings.HasSuffix(kernel,"vmlinuz-6.12.0-test")||
  !strings.HasSuffix(initrd,"initrd.img-6.12.0-test"){
  t.Fatalf("wrong kernel/initramfs pair: %s %s %v",kernel,initrd,err)
 }
 output:=filepath.Join(t.TempDir(),"copy.img")
 if err:=copyISOFile(kernel,output);err!=nil{t.Fatal(err)}
 if _,err:=copyISOFile(kernel,output);err==nil{t.Fatal("ISO staging overwrote existing file")}
 if _,err:=os.Stat(output);err!=nil{t.Fatal(err)}
}
