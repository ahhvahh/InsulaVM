package insulavm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestStartVMFromInstallerISOUsesConfigRelativeISO(t *testing.T){
 engine:=testEngine(t,"")
 if _,err:=engine.StartVMFromInstallerISO(context.Background(),"",nil);err==nil{
  t.Fatal("missing installer profile accepted")
 }
 if _,err:=engine.StartVMFromInstallerISO(context.Background(),"missing",nil);err==nil{
  t.Fatal("missing installer profile file accepted")
 }
 pdir:=filepath.Join(engine.Configuration().Storage.Profiles,"alpine-virt")
 if err:=os.MkdirAll(pdir,0700);err!=nil{t.Fatal(err)}
 yaml:=`name: alpine-virt
hardware:
  cpu:
    quantity: 1
  ram:
    mib: 500
disk:
  start_iso: ./iso/not-found.iso
  size_gb: 8
network:
  enabled: false
`
 if err:=os.WriteFile(filepath.Join(pdir,"profile.yaml"),[]byte(yaml),0600);err!=nil{t.Fatal(err)}
 _,err:=engine.StartVMFromInstallerISO(context.Background(),"alpine-virt",nil)
 if err==nil||!strings.Contains(err.Error(),"inspect installer ISO"){
  t.Fatalf("expected missing installer ISO, got %v",err)
 }
 if !strings.Contains(err.Error(),filepath.Join(engine.configDir,"iso","not-found.iso")){
  t.Fatalf("ISO path was not config-relative: %v",err)
 }
}
