package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestStopByIDShutOffDomainCleanup(t *testing.T){
 tests:=[]struct{name string; badXML bool; failUndefine bool; wantError bool}{
  {"verified defined domain",false,false,false},
  {"mismatched Libvirt XML",true,false,true},
  {"undefine failed",false,true,true},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   c,id,disk,_:=resumeFixture(t)
   marker:=filepath.Join(t.TempDir(),"defined")
   if err:=os.WriteFile(marker,[]byte("1"),0600);err!=nil{t.Fatal(err)}
   xmlPath:=filepath.Join(filepath.Dir(disk),"domain.xml")
   if tc.badXML{
    original,err:=os.ReadFile(xmlPath);if err!=nil{t.Fatal(err)}
    invalid:=strings.Replace(string(original),id,"different-uuid",1)
    if err:=os.WriteFile(xmlPath,[]byte(invalid),0600);err!=nil{t.Fatal(err)}
   }
   bin:=t.TempDir()
   events:=filepath.Join(t.TempDir(),"events")
   script:=`#!/bin/sh
set -eu
case "$3" in
 list) if [ -f "$MOCK_DEFINED" ]; then echo "$MOCK_UUID"; fi ;;
 domstate) echo 'shut off' ;;
 dumpxml) cat "$MOCK_DOMAIN_XML" ;;
 undefine)
  echo undefine >> "$MOCK_EVENTS"
  if [ "$MOCK_FAIL_UNDEFINE" = 1 ]; then echo 'simulated failure' >&2; exit 19; fi
  rm -f "$MOCK_DEFINED" ;;
 destroy) echo destroy >> "$MOCK_EVENTS"; exit 20 ;;
 *) echo "unexpected action $3" >&2; exit 21 ;;
esac
`
   if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
   t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
   t.Setenv("MOCK_DEFINED",marker)
   t.Setenv("MOCK_UUID",id)
   t.Setenv("MOCK_DOMAIN_XML",xmlPath)
   t.Setenv("MOCK_EVENTS",events)
   if tc.failUndefine{t.Setenv("MOCK_FAIL_UNDEFINE","1")}else{t.Setenv("MOCK_FAIL_UNDEFINE","0")}
   err:=StopByID(context.Background(),c,id)
   if (err!=nil)!=tc.wantError{t.Fatalf("unexpected stop outcome: %v",err)}
   _,diskErr:=os.Stat(disk)
   _,definedErr:=os.Stat(marker)
   store,openErr:=registry.Open(c.Storage.Database)
   if openErr!=nil{t.Fatal(openErr)}
   rec,getErr:=store.Get(context.Background(),id)
   _=store.Close()
   if getErr!=nil{t.Fatal(getErr)}
   if tc.wantError{
    if diskErr!=nil||definedErr!=nil||rec.Status!="running"{
     t.Fatalf("failure lost VM: disk=%v defined=%v status=%s",diskErr,definedErr,rec.Status)
    }
   }else{
    if !os.IsNotExist(diskErr)||!os.IsNotExist(definedErr)||rec.Status!="destroyed"{
     t.Fatalf("cleanup incomplete: disk=%v defined=%v status=%s",diskErr,definedErr,rec.Status)
    }
   }
   trace,_:=os.ReadFile(events)
   if strings.Contains(string(trace),"destroy"){t.Fatalf("powered-off VM must not be forcibly destroyed: %s",trace)}
   if tc.badXML&&strings.Contains(string(trace),"undefine"){
    t.Fatalf("mismatched domain was undefined: %s",trace)
   }
  })
 }
}
