package config

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestReadGlobalSettingsAndDirectories(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"config.yaml")
 doc:="storage:\n  root: ./data\n  profiles: profiles\n  runs: runs\n  logs: logs\nvirtualization:\n  provider: kvm\n  network: default\n  default_disk_gb: 8\nssh:\n  connect_timeout_seconds: 15\n"
 if err:=os.WriteFile(path,[]byte(doc),0600);err!=nil{t.Fatal(err)}
 c,err:=ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if c.Storage.Profiles!=filepath.Join(dir,"data","profiles"){t.Fatalf("profiles: %s",c.Storage.Profiles)}
 if c.Storage.Logs!=filepath.Join(dir,"data","logs"){t.Fatalf("logs: %s",c.Storage.Logs)}
 if c.Virtualization.LibvirtURI!="qemu:///system"{t.Fatal("missing default libvirt URI")}
 if err:=c.EnsureDirectories();err!=nil{t.Fatal(err)}
 for _,p:=range []string{c.Storage.Profiles,c.Storage.Runs,c.Storage.Logs}{
  if _,err:=os.Stat(p);err!=nil{t.Fatal(err)}
 }
}

func TestRejectsInvalidGlobalSettings(t *testing.T){
 dir:=t.TempDir()
 cfg:=filepath.Join(dir,"config.yaml")
 cases:=[]string{
  "storage:\n  root: /\n",
  "storage:\n  root: ./data\n  bad: 42\n",
  "storage:\n  root: ./data\nvirtualization:\n  provider: docker\n",
  "storage:\n  root: ./data\n  profiles: same\n  runs: same\n",
  "storage:\n  root: ./data\nssh:\n  connect_timeout_seconds: -1\n",
 }
 for _,src:=range cases{
  if err:=os.WriteFile(cfg,[]byte(src),0600);err!=nil{t.Fatal(err)}
  if _,err:=ReadFile(cfg);err==nil{t.Fatalf("accepted invalid config: %s",strings.TrimSpace(src))}
 }
}
