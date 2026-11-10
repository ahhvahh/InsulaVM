package vm

import (
 "context"
 "encoding/xml"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestLiveISOPersistentEventsAndLibvirtSnapshot(t *testing.T){
 root:=t.TempDir()
 uuid:="01a1c192-e9c6-7064-85c5-73151ded8551"
 log,err:=OpenLiveISOLog(root,uuid)
 if err!=nil{t.Fatal(err)}
 log.Event("boot","started diagnostic VM")
 if err:=log.Close();err!=nil{t.Fatal(err)}
 if st,err:=os.Stat(log.Path);err!=nil||st.Mode().Perm()&0077!=0{
  t.Fatalf("event log missing or too permissive: %v %v",st,err)
 }
 payload,err:=os.ReadFile(log.Path)
 if err!=nil||!strings.Contains(string(payload),"[boot] started diagnostic VM"){
  t.Fatalf("event message not recorded: %v %q",err,payload)
 }
 bin:=t.TempDir()
 script:=`#!/bin/sh
set -eu
case "$3" in
 domstate) echo running ;;
 dominfo) echo "State: running" ;;
 domiflist) echo "vnet0 network default virtio 52:54:00:aa:bb:cc" ;;
 domifaddr) echo "vnet0 52:54:00:aa:bb:cc ipv4 192.168.122.45/24" ;;
 net-info) echo "Active: yes" ;;
 net-dhcp-leases)
   echo "other 52:54:00:ff:ee:dd 192.168.122.100"
   echo "this 52:54:00:aa:bb:cc 192.168.122.45"
   ;;
 *) echo "unexpected call $3" >&2; exit 90 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 snapshot,err:=CaptureLiveISODiagnostics(context.Background(),root,"qemu:///system",uuid,"default","52:54:00:aa:bb:cc")
 if err!=nil{t.Fatal(err)}
 captured,err:=os.ReadFile(snapshot)
 if err!=nil{t.Fatal(err)}
 for _,want:=range []string{"domstate","domiflist","dhcp-lease","network-state","network-leases","192.168.122.45"}{
  if !strings.Contains(string(captured),want){t.Fatalf("snapshot missing %s: %s",want,captured)}
 }
 if strings.Contains(string(captured),"192.168.122.100"){
  t.Fatal("snapshot included an unrelated guest's DHCP lease")
 }
 if _,err:=os.Stat(snapshot);err!=nil{t.Fatal("snapshot was not preserved")}
}

func TestLiveISOConsoleLogXML(t *testing.T){
 // XML permits equivalent encodings such as &#39; and &apos;. Decode the
 // document and verify semantic attributes instead of hardcoding entities.
 const wantPath="/tmp/vm's&console.log"
 spec:=liveISODomainXMLWithLog(
  "01a1c192-e9c6-7064-85c5-73151ded8551","insula-test",1024,1,
  "/tmp/test.iso","/tmp/seed.iso","/tmp/disk.qcow2","default",
  "52:54:00:aa:bb:cc",wantPath)
 var domain struct{
  Devices struct{
   Serial struct{
    Type string `xml:"type,attr"`
    Target struct{Port string `xml:"port,attr"`} `xml:"target"`
    Log struct{
     File string `xml:"file,attr"`
     Append string `xml:"append,attr"`
    } `xml:"log"`
   } `xml:"serial"`
   Console struct{
    Type string `xml:"type,attr"`
    Target struct{
     Type string `xml:"type,attr"`
     Port string `xml:"port,attr"`
    } `xml:"target"`
   } `xml:"console"`
  } `xml:"devices"`
 }
 if err:=xml.Unmarshal([]byte(spec),&domain);err!=nil{
  t.Fatalf("invalid domain XML: %v; document=%s",err,spec)
 }
 serial:=domain.Devices.Serial
 if serial.Type!="pty"||serial.Target.Port!="0"{
  t.Fatalf("unexpected serial configuration: %+v",serial)
 }
 if serial.Log.File!=wantPath||serial.Log.Append!="on"{
  t.Fatalf("serial logging attributes incorrect: %+v",serial.Log)
 }
 console:=domain.Devices.Console
 if console.Type!="pty"||console.Target.Type!="serial"||console.Target.Port!="0"{
  t.Fatalf("unexpected console configuration: %+v",console)
 }
}
