package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestProfileRegisterCLIStoresGuestDestination(t *testing.T) {
 root:=t.TempDir()
 cfg:=filepath.Join(root,"config.yaml")
 if err:=os.WriteFile(cfg,[]byte("storage:\n  root: ./data\n"),0600);err!=nil{t.Fatal(err)}
 disk:=filepath.Join(root,"installed.qcow2")
 if err:=os.WriteFile(disk,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
 args:=[]string{"profile","register","-config",cfg,"-name","per-vm","-image",disk,
  "-memory-mib","512","-vcpus","1","-disk-gb","2","-destination","/srv/testing/per-vm"}
 if err:=run(args);err!=nil{t.Fatal(err)}
 saved:=filepath.Join(root,"data","profiles","per-vm","profile.yaml")
 data,err:=os.ReadFile(saved)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(data),"destination: /srv/testing/per-vm") {
  t.Fatalf("CLI did not persist profile destination: %s",data)
 }
 invalid:=[]string{"profile","register","-config",cfg,"-name","invalid","-image",disk,
  "-memory-mib","512","-vcpus","1","-disk-gb","2","-destination","relative/work"}
 if err:=run(invalid);err==nil||!strings.Contains(err.Error(),"destination") {
  t.Fatalf("CLI accepted unsafe destination: %v",err)
 }
}
