package vm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/config"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func resumeFixture(t *testing.T)(*config.Config,string,string,string){
 t.Helper()
 root:=t.TempDir()
 c:=&config.Config{}
 c.Storage.Runs=filepath.Join(root,"runs")
 c.Storage.Database=filepath.Join(root,"registry.db")
 c.Virtualization.LibvirtURI="qemu:///system"
 dir:=filepath.Join(c.Storage.Runs,"insula-vm-1")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 const id="019a5d11-1234-7123-8123-123456789abc"
 disk:=filepath.Join(dir,"installer.qcow2")
 if err:=os.WriteFile(disk,[]byte("test-disk"),0600);err!=nil{t.Fatal(err)}
 name:="insula-alpine-virt-"+id
 xml:=installerISODomainXML(id,name,500,1,disk,"/tmp/alpine.iso","52:54:00:11:22:33",true,"default")
 if err:=os.WriteFile(filepath.Join(dir,"domain.xml"),[]byte(xml),0600);err!=nil{t.Fatal(err)}
 store,err:=registry.Open(c.Storage.Database);if err!=nil{t.Fatal(err)}
 entry:=registry.Entry{ID:id,LibvirtUUID:id,Name:name,Profile:"alpine-virt",MAC:"52:54:00:11:22:33",InstanceDir:dir}
 if err:=store.Insert(context.Background(),entry);err!=nil{t.Fatal(err)}
 if err:=store.SetRunning(context.Background(),id,"192.0.2.40");err!=nil{t.Fatal(err)}
 _=store.Close()
 bin:=t.TempDir()
 script:=`#!/bin/sh
case "$3" in
 list)
  if [ -n "$MOCK_LIST_UUID" ]; then echo "$MOCK_LIST_UUID"; fi ;;
 domstate) echo running ;;
 create)
  cat "$4" > "$MOCK_CAPTURE"
  echo "Domain created" ;;
 *) echo "unexpected virsh command $3" >&2; exit 11 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 capture:=filepath.Join(root,"created.xml")
 t.Setenv("MOCK_CAPTURE",capture)
 t.Setenv("MOCK_LIST_UUID","")
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 return c,id,disk,capture
}

func TestResumeByIDReusesIDAndBootsInstallerDiskFirst(t *testing.T){
 c,id,disk,capture:=resumeFixture(t)
 got,err:=ResumeByID(context.Background(),c,id)
 if err!=nil{t.Fatal(err)}
 if got.ID!=id||got.LibvirtUUID!=id||got.IP!=""||got.Status!="running"{
  t.Fatalf("wrong resumed identity or stale IP: %+v",got)
 }
 created,err:=os.ReadFile(capture);if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(created),"<boot dev='hd'/><boot dev='cdrom'/>"){
  t.Fatalf("installer should boot existing installed disk: %s",created)
 }
 if !strings.Contains(string(created),disk){t.Fatal("preserved disk was not used")}
 original,err:=os.ReadFile(filepath.Join(filepath.Dir(disk),"domain.xml"));if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(original),"<boot dev='cdrom'/><boot dev='hd'/>"){
  t.Fatal("original install-iso XML was modified")
 }
 store,err:=registry.Open(c.Storage.Database);if err!=nil{t.Fatal(err)}
 defer store.Close()
 entry,err:=store.Get(context.Background(),id)
 if err!=nil||entry.IP!=""||entry.Status!="running"{t.Fatalf("SQLite not updated: %+v %v",entry,err)}
}

func TestResumeByIDRefusesUnsafeOrMissingVM(t *testing.T){
 cases:=[]struct{name string;modify func(*testing.T,*config.Config,string,string);want string}{
  {"already running",func(t *testing.T,c *config.Config,id,disk string){t.Setenv("MOCK_LIST_UUID",id)},"state=running"},
  {"missing disk",func(t *testing.T,c *config.Config,id,disk string){if err:=os.Remove(disk);err!=nil{t.Fatal(err)}},"preserved VM disk"},
  {"symlink disk",func(t *testing.T,c *config.Config,id,disk string){
    if err:=os.Remove(disk);err!=nil{t.Fatal(err)}
    if err:=os.Symlink("/etc/passwd",disk);err!=nil{t.Fatal(err)}
   },"not a regular file"},
  {"mismatched XML",func(t *testing.T,c *config.Config,id,disk string){
    path:=filepath.Join(filepath.Dir(disk),"domain.xml")
    data,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(path,[]byte(strings.Replace(string(data),id,"other-uuid",1)),0600);err!=nil{t.Fatal(err)}
   },"does not match"},
  {"destroyed",func(t *testing.T,c *config.Config,id,disk string){
    s,err:=registry.Open(c.Storage.Database);if err!=nil{t.Fatal(err)}
    if err:=s.SetDestroyed(context.Background(),id);err!=nil{t.Fatal(err)}
    _=s.Close()
   },"destroyed"},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  c,id,disk,capture:=resumeFixture(t)
  tc.modify(t,c,id,disk)
  _,err:=ResumeByID(context.Background(),c,id)
  if err==nil||!strings.Contains(err.Error(),tc.want){t.Fatalf("want %q got %v",tc.want,err)}
  if _,err:=os.Stat(capture);!errors.Is(err,os.ErrNotExist){t.Fatal("unexpected domain creation")}
 })}
}

func TestResumeByIDCancellationAndUnknownVM(t *testing.T){
 c,id,_,_:=resumeFixture(t)
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=ResumeByID(ctx,c,id);!errors.Is(err,context.Canceled){t.Fatalf("cancel not propagated: %v",err)}
 if _,err:=ResumeByID(context.Background(),c,"not-registered");err==nil{t.Fatal("unknown VM was resumed")}
}
