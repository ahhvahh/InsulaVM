package vm

import (
 "context"
 "os"
 "path/filepath"
 "testing"
 "strings"
 "time"
)

func TestShutdownForFreezeWaitsForDomainToDisappear(t *testing.T){
 bin:=t.TempDir()
 const uuid="01a11111-1111-7111-8111-111111111111"
 state:=filepath.Join(bin,"running")
 if err:=os.WriteFile(state,[]byte("yes"),0600);err!=nil{t.Fatal(err)}
 script:="#!/bin/sh\nset -eu\n"+
  "case \"$3\" in\n"+
  " list) if [ -f \"$MOCK_RUNNING\" ]; then echo \"$MOCK_UUID\"; fi ;;\n"+
  " domstate) echo running ;;\n"+
  " shutdown) rm -f \"$MOCK_RUNNING\" ;;\n"+
  " *) echo unexpected >&2; exit 77 ;;\n"+
  "esac\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("MOCK_UUID",uuid)
 t.Setenv("MOCK_RUNNING",state)
 err:=ShutdownForFreeze(context.Background(),"qemu:///system",uuid,2*time.Second)
 if err!=nil{t.Fatal(err)}
 if _,err:=os.Stat(state);!os.IsNotExist(err){t.Fatalf("domain still listed: %v",err)}
}

func TestShutdownForFreezeRefusesMissingDomain(t *testing.T){
 bin:=t.TempDir()
 script:="#!/bin/sh\n"+
  "if [ \"$3\" = list ]; then exit 0; fi\n"+
  "echo unexpected >&2; exit 88\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 if err:=ShutdownForFreeze(context.Background(),"qemu:///system","01a11111-1111-7111-8111-111111111111",time.Second);err==nil{
  t.Fatal("attempted shutdown when domain is absent")
 }
}

func TestShutdownForFreezeDoesNotForceWithoutOptIn(t *testing.T){
 bin:=t.TempDir()
 uuid:="01a11111-1111-7111-8111-222222222222"
 state:=filepath.Join(bin,"running")
 if err:=os.WriteFile(state,[]byte("yes"),0600);err!=nil{t.Fatal(err)}
 log:=filepath.Join(bin,"virsh.log")
 script:=`#!/bin/sh
set -eu
case "$3" in
 list) if [ -f "$MOCK_RUNNING" ]; then echo "$MOCK_UUID"; fi ;;
 domstate) echo running ;;
 shutdown) echo graceful >> "$MOCK_LOG" ;;
 destroy) echo forced >> "$MOCK_LOG"; rm -f "$MOCK_RUNNING" ;;
 *) echo unexpected >&2; exit 77 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("MOCK_UUID",uuid)
 t.Setenv("MOCK_RUNNING",state)
 t.Setenv("MOCK_LOG",log)
 forced,err:=ShutdownForFreezeWithOptions(context.Background(),"qemu:///system",uuid,50*time.Millisecond,false)
 if err==nil||forced{t.Fatalf("expected safe timeout: forced=%t err=%v",forced,err)}
 if _,err:=os.Stat(state);err!=nil{t.Fatal("domain was destroyed without permission")}
 forced,err=ShutdownForFreezeWithOptions(context.Background(),"qemu:///system",uuid,50*time.Millisecond,true)
 if err!=nil||!forced{t.Fatalf("expected explicit force shutdown: forced=%t err=%v",forced,err)}
 if _,err:=os.Stat(state);!os.IsNotExist(err){t.Fatalf("domain still running: %v",err)}
 trace,err:=os.ReadFile(log)
 if err!=nil{t.Fatal(err)}
 if strings.Count(string(trace),"forced")!=1{t.Fatalf("incorrect force count: %s",trace)}
}

func TestShutdownForFreezeToleratesGuestAlreadyPoweredOffDuringRequest(t *testing.T){
 bin:=t.TempDir()
 const uuid="01a11111-1111-7111-8111-333333333333"
 state:=filepath.Join(bin,"running")
 if err:=os.WriteFile(state,[]byte("on"),0600);err!=nil{t.Fatal(err)}
 script:=`#!/bin/sh
set -eu
case "$3" in
 list) if [ -f "$MOCK_RUNNING" ]; then echo "$MOCK_UUID"; fi ;;
 domstate) echo running ;;
 shutdown) rm -f "$MOCK_RUNNING"; exit 27 ;;
 *) exit 88 ;;
esac
`
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
 t.Setenv("MOCK_UUID",uuid)
 t.Setenv("MOCK_RUNNING",state)
 forced,err:=ShutdownForFreezeWithOptions(context.Background(),"qemu:///system",uuid,2*time.Second,false)
 if err!=nil||forced{t.Fatalf("guest powered itself off; err=%v forced=%t",err,forced)}
 if _,err:=os.Stat(state);!os.IsNotExist(err){t.Fatalf("domain still listed: %v",err)}
}
