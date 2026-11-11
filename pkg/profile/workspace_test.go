package profile

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestProfileWorkspaceDestinationValidation(t *testing.T){
 for _,tc:=range []struct{name,dest string;valid bool}{
  {"unset","",true},
  {"valid","/tmp/workspace",true},
  {"spaces","/tmp/my workspace",true},
  {"relative","tmp/workspace",false},
  {"root","/",false},
  {"line break","/tmp/line\nnew",false},
  {"NUL","/tmp/work\x00space",false},
 }{
  t.Run(tc.name,func(t *testing.T){
   p:=Profile{Name:"demo",MemoryMiB:1024,VCPUs:1,DiskGB:8,Version:"v001",Image:"versions/v001/base.qcow2"}
   p.Workspace.Destination=tc.dest
   err:=p.Validate()
   if tc.valid && err!=nil{t.Fatalf("unexpected validation: %v",err)}
   if !tc.valid && (err==nil||!strings.Contains(err.Error(),"workspace.destination")){t.Fatalf("expected workspace rejection: %v",err)}
  })
 }
}

func TestProfileWorkspaceMetadataRoundTrip(t *testing.T){
 root:=t.TempDir()
 base:=filepath.Join(t.TempDir(),"base.qcow2")
 if err:=os.WriteFile(base,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 p:=Profile{Name:"alpine",OS:"alpine",MemoryMiB:1024,VCPUs:1,DiskGB:8}
 p.Workspace.Destination="/tmp/workspace"
 if err:=Register(root,p,base);err!=nil{t.Fatal(err)}
 loaded,err:=Read(root,"alpine")
 if err!=nil{t.Fatal(err)}
 if loaded.Workspace.Destination!=p.Workspace.Destination{t.Fatalf("workspace metadata lost: %q",loaded.Workspace.Destination)}
}
