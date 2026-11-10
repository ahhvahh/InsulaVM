package vm

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestLiveISOHostRejectsInvalidImageAndResourcesBeforeLibvirt(t *testing.T){
 cfg,iso:=isolatedLiveISOHost(t)
 empty:=filepath.Join(t.TempDir(),"empty.iso")
 if err:=os.WriteFile(empty,nil,0600);err!=nil{t.Fatal(err)}
 tests:=[]struct{name,path string;memory,cpus int;want string}{
  {"empty ISO",empty,512,1,"nonempty regular"},
  {"directory ISO",t.TempDir(),512,1,"nonempty regular"},
  {"missing ISO",filepath.Join(t.TempDir(),"missing.iso"),512,1,"no such file"},
  {"too little memory",iso,64,1,"invalid memory"},
  {"zero CPUs",iso,512,0,"invalid memory"},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   result,err:=StartLiveISOWithOptions(context.Background(),cfg,"profile",tc.path,tc.memory,tc.cpus,nil,LiveISOStartupOptions{})
   if err==nil||result!=nil||!strings.Contains(err.Error(),tc.want){
    t.Fatalf("unsafe boot input %s accepted: %+v %v",tc.name,result,err)
   }
  })
 }
}

func TestLiveISOHostRetainsForensicLogsWhenQEMUImageCreationFails(t *testing.T){
 cfg,iso:=isolatedLiveISOHost(t)
 bin:=strings.Split(os.Getenv("PATH"),string(os.PathListSeparator))[0]
 failure:="#!/bin/sh\necho 'qcow2 allocation refused' >&2\nexit 51\n"
 if err:=os.WriteFile(filepath.Join(bin,"qemu-img"),[]byte(failure),0700);err!=nil{t.Fatal(err)}
 instance,err:=StartLiveISOWithOptions(context.Background(),cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{})
 if err==nil||instance!=nil||!strings.Contains(err.Error(),"qcow2 allocation refused"){
  t.Fatalf("qemu-img failure lost: %+v %v",instance,err)
 }
 rows,readErr:=os.ReadDir(cfg.Storage.Runs)
 if readErr!=nil||len(rows)!=0{
  t.Fatalf("failed boot left transient storage: %v %v",rows,readErr)
 }
}

func TestLiveISOHostDoesNotReportSuccessForLibvirtCreateFailure(t *testing.T){
 cfg,iso:=isolatedLiveISOHost(t)
 bin:=strings.Split(os.Getenv("PATH"),string(os.PathListSeparator))[0]
 failure:="#!/bin/sh\ncase \"$3\" in\n"+
  "create) echo 'libvirt refused domain' >&2; exit 55 ;;\n"+
  "list) exit 0 ;;\n"+
  "*) echo no-domain ;;\nesac\n"
 if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(failure),0700);err!=nil{t.Fatal(err)}
 result,err:=StartLiveISOWithOptions(context.Background(),cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{
  PreserveOnFailure:true,
 })
 if err==nil||result!=nil||!strings.Contains(err.Error(),"libvirt refused domain"){
  t.Fatalf("libvirt create failure was reported as success: %+v %v",result,err)
 }
 entries,readErr:=os.ReadDir(cfg.Storage.Logs)
 if readErr!=nil||len(entries)==0{t.Fatalf("failed boot lost diagnostic directory: %v %v",entries,readErr)}
}

func TestLiveISOHostRefusesRootSSHAndOversizedStartupScript(t *testing.T){
 cfg,iso:=isolatedLiveISOHost(t)
 cfg.SSH.User="root"
 if _,err:=StartLiveISOWithOptions(context.Background(),cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{});err==nil||
  !strings.Contains(err.Error(),"non-root"){
  t.Fatalf("root Cloud-Init account allowed: %v",err)
 }
 cfg.SSH.User="insula"
 tooBig:=make([]byte,256*1024+1)
 if _,err:=StartLiveISOWithOptions(context.Background(),cfg,"prepared",iso,512,1,tooBig,LiveISOStartupOptions{});err==nil||
  !strings.Contains(err.Error(),"startup script"){
  t.Fatalf("oversized guest root script allowed: %v",err)
 }
 cancelled,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=StartLiveISOWithOptions(cancelled,cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{});!errors.Is(err,context.Canceled){
  t.Fatalf("cancelled boot did not stop before create: %v",err)
 }
 if _,err:=StartLiveISOWithOptions(context.Background(),cfg,"prepared",iso,512,1,nil,LiveISOStartupOptions{
  ReadinessTimeout:-time.Millisecond,
 });err==nil{
  t.Fatal("negative readiness timeout accepted")
 }
}
