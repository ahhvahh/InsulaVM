package storage

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestJobLogFlushDurabilityAndClosedErrors(t *testing.T){
    now:=time.Date(2026,11,9,14,30,0,0,time.UTC)
    run,err:=NewRun(t.TempDir(),"repo","ci",now)
    if err!=nil{t.Fatal(err)}
    log,err:=run.OpenJob("verify")
    if err!=nil{t.Fatal(err)}
    if err:=log.Request("echo first\r\necho second");err!=nil{t.Fatal(err)}
    if err:=log.Flush();err!=nil{t.Fatalf("log flush failed: %v",err)}
    before,err:=os.ReadFile(filepath.Join(run.Directory,"verify.log"))
    if err!=nil{t.Fatal(err)}
    if string(before)!=">> echo first\n>> echo second\n"{
        t.Fatalf("flush did not persist normalized command: %q",before)
    }
    if err:=log.Response("last line without newline");err!=nil{t.Fatal(err)}
    if err:=log.Close();err!=nil{t.Fatal(err)}
    if err:=log.Close();err!=nil{t.Fatalf("double close should be idempotent: %v",err)}
    for _,operation:=range []struct{name string;run func()error}{
        {"flush",log.Flush},{"request",func()error{return log.Request("bad")}},
        {"response",func()error{return log.Response("bad")}},
    }{
        if err:=operation.run();!errors.Is(err,os.ErrClosed){
            t.Errorf("%s on closed log should fail with ErrClosed: %v",operation.name,err)
        }
    }
}

func TestInvalidRunAndJobNamesNeverCreateFiles(t *testing.T){
    if _,err:=NewRun("","repo","ci",time.Now());err==nil{t.Fatal("blank storage root accepted")}
    for _,value:=range []string{"",".hidden","..","invalid/name","space name",strings.Repeat("x",129)}{
        if err:=ValidateName(value);err==nil{t.Errorf("invalid name was accepted: %q",value)}
    }
    if _,err:=NewRun(t.TempDir(),"../escape","ci",time.Now());err==nil{t.Fatal("unsafe repository accepted")}
    if _,err:=NewRun(t.TempDir(),"repo","../escape",time.Now());err==nil{t.Fatal("unsafe action accepted")}
    run,err:=NewRun(t.TempDir(),"repo","ci",time.Now())
    if err!=nil{t.Fatal(err)}
    if _,err:=run.OpenJob("../escape");err==nil{t.Fatal("job path traversal accepted")}
    if _,err:=run.OpenJob("no spaces");err==nil{t.Fatal("unsafe job name accepted")}
}
