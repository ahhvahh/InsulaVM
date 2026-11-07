package vm

import (
 "context"
 "os"
 "path/filepath"
 "testing"
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
