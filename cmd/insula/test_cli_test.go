package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestTestCLIValidatesArgumentsWithoutKVM(t *testing.T){
 dir:=t.TempDir()
 cfg:=filepath.Join(dir,"config.yaml")
 if err:=os.WriteFile(cfg,[]byte("storage:\n  root: "+filepath.Join(dir,"state")+"\nvirtualization:\n  provider: kvm\n"),0600);err!=nil{t.Fatal(err)}
 cases:=[]struct{args []string;want string}{
  {[]string{"ssh","-config",cfg},"requires -id"},
  {[]string{"boot","-config",cfg},"requires -profile"},
  {[]string{"install","-config",cfg},"requires -profile"},
  {[]string{"iso","-config",cfg},"requires -id and -name"},
  {[]string{"iso","-config",cfg,"-id","uuid","-name","frozen"},"-confirm"},
  {[]string{"verify","-config",cfg},"requires -profile"},
  {[]string{"cycle","-config",cfg},"requires -profile and -name"},
  {[]string{"shell","-config",cfg},"requires -id"},
  {[]string{"bad","-config",cfg},"usage"},
 }
 for _,tc:=range cases{
  err:=testCommand(tc.args)
  if err==nil||!strings.Contains(err.Error(),tc.want){t.Fatalf("%v expected %q; got %v",tc.args,tc.want,err)}
 }
}
