package main

import (
 "os"
 "strings"
 "path/filepath"
 "testing"
)

func TestRedundantVMShowAliasRemoved(t *testing.T) {
 dir := t.TempDir()
 configFile := filepath.Join(dir, "config.yaml")
 data := []byte("storage:\n  root: ./data\nvirtualization:\n  provider: kvm\n  default_disk_gb: 8\n")
 if err := os.WriteFile(configFile, data, 0600); err != nil { t.Fatal(err) }
 err := run([]string{"vm", "show", "-config", configFile, "-id", "x"})
 if err == nil || !strings.Contains(err.Error(), "usage: insula vm") {
  t.Fatalf("vm show should be unsupported; got: %v", err)
 }
 err = run([]string{"vm", "status", "-config", configFile, "-id", "x"})
 if err == nil || strings.Contains(err.Error(), "usage: insula vm") {
  t.Fatalf("vm status must remain routed to status lookup; got: %v", err)
 }
}

func TestConfigCheckAndProfileCommands(t *testing.T){
 dir:=t.TempDir()
 configFile:=filepath.Join(dir,"config.yaml")
 data:=[]byte("storage:\n  root: ./data\nvirtualization:\n  provider: kvm\n  default_disk_gb: 8\n")
 if err:=os.WriteFile(configFile,data,0600);err!=nil{t.Fatal(err)}
 if err:=run([]string{"config","check","-config",configFile});err!=nil{t.Fatal(err)}
 if err:=run([]string{"profile","list","-config",configFile});err!=nil{t.Fatal(err)}
 disk:=filepath.Join(dir,"installed.qcow2")
 if err:=os.WriteFile(disk,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 if err:=run([]string{"profile","register","-config",configFile,"-name","debian-go",
  "-image",disk,"-memory-mib","1024","-vcpus","1"});err!=nil{t.Fatal(err)}
 if err:=run([]string{"profile","show","-config",configFile,"-name","debian-go"});err!=nil{t.Fatal(err)}
 if err:=run([]string{"profile","list","-config",configFile});err!=nil{t.Fatal(err)}
}
