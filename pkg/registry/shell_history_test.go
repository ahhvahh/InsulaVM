package registry

import (
 "bytes"
 "context"
 "errors"
 "path/filepath"
 "testing"
)

func TestShellHistoryPersistsAcrossReopen(t *testing.T){
 ctx:=context.Background()
 db:=filepath.Join(t.TempDir(),"state","insula.db")
 store,err:=Open(db)
 if err!=nil{t.Fatal(err)}
 vmID:="019a5d11-1234-7123-8123-123456789abc"
 err=store.Insert(ctx,Entry{ID:vmID,LibvirtUUID:vmID,Name:"insula-test",Profile:"test",
  MAC:"52:54:00:00:12:34",SSHUser:"insula",InstanceDir:"/tmp/test"})
 if err!=nil{t.Fatal(err)}
 shellID:="019a5d11-4321-7123-8123-123456789abc"
 if err:=store.InsertShellSession(ctx,shellID,vmID);err!=nil{t.Fatal(err)}
 if err:=store.InsertShellSession(ctx,shellID,vmID);err==nil{t.Fatal("duplicate shell ID accepted")}
 commandID:="019a5d11-4321-7123-8123-aaaaaaaaaaaa"
 if err:=store.BeginShellCommand(ctx,ShellCommandEntry{ID:commandID,ShellID:shellID,VMID:vmID,Command:"printf 'a\\000b'"});err!=nil{t.Fatal(err)}
 payload:=[]byte{'a',0,'b','\n'}
 code:=7
 if err:=store.FinishShellCommand(ctx,commandID,payload,&code,nil);err!=nil{t.Fatal(err)}
 if err:=store.FinishShellCommand(ctx,commandID,payload,&code,nil);err==nil{t.Fatal("double finish accepted")}
 failedID:="019a5d11-4321-7123-8123-bbbbbbbbbbbb"
 if err:=store.BeginShellCommand(ctx,ShellCommandEntry{ID:failedID,ShellID:shellID,VMID:vmID,Command:"bad"});err!=nil{t.Fatal(err)}
 if err:=store.FinishShellCommand(ctx,failedID,[]byte("partial"),nil,errors.New("connection lost"));err!=nil{t.Fatal(err)}
 quietID:="019a5d11-4321-7123-8123-cccccccccccc"
 if err:=store.BeginShellCommand(ctx,ShellCommandEntry{ID:quietID,ShellID:shellID,VMID:vmID,Command:"true"});err!=nil{t.Fatal(err)}
 zero:=0
 if err:=store.FinishShellCommand(ctx,quietID,nil,&zero,nil);err!=nil{t.Fatalf("empty response must persist: %v",err)}
 if err:=store.SetShellSessionStatus(ctx,shellID,"disconnected");err!=nil{t.Fatal(err)}
 if err:=store.Close();err!=nil{t.Fatal(err)}
 store,err=Open(db)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 sess,err:=store.GetShellSession(ctx,shellID)
 if err!=nil||sess.Status!="disconnected"||sess.ClosedAt==""{t.Fatalf("shell metadata: %+v %v",sess,err)}
 history,err:=store.ListShellCommands(ctx,shellID)
 if err!=nil||len(history)!=3{t.Fatalf("history: %+v %v",history,err)}
 if history[0].Status!="completed"||history[0].ExitCode==nil||*history[0].ExitCode!=7||!bytes.Equal(history[0].Output,payload){
  t.Fatalf("lost binary output or exit code: %+v",history[0])
 }
 if history[1].Status!="failed"||history[1].ExitCode!=nil||history[1].Error!="connection lost"{
  t.Fatalf("lost failure information: %+v",history[1])
 }
 sessions,err:=store.ListShellSessions(ctx,vmID)
 if err!=nil||len(sessions)!=1||sessions[0].ID!=shellID{t.Fatalf("shells by VM: %+v %v",sessions,err)}
 if history[2].Status!="completed"||history[2].ExitCode==nil||*history[2].ExitCode!=0||len(history[2].Output)!=0{
  t.Fatalf("empty response lost: %+v",history[2])
 }
 if _,err:=store.GetShellSession(ctx,"invalid");err==nil{t.Fatal("missing shell accepted")}
}

func TestShellHistoryRejectsInvalidRequests(t *testing.T){
 store,err:=Open(filepath.Join(t.TempDir(),"insula.db"))
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 ctx:=context.Background()
 if err:=store.InsertShellSession(nil,"id","vm");err==nil{t.Fatal("nil context accepted")}
 if err:=store.InsertShellSession(ctx,"","vm");err==nil{t.Fatal("missing session ID accepted")}
 if err:=store.SetShellSessionStatus(ctx,"unknown","active");err==nil{t.Fatal("invalid status accepted")}
 if err:=store.BeginShellCommand(ctx,ShellCommandEntry{});err==nil{t.Fatal("missing command accepted")}
 if _,err:=store.ListShellCommands(ctx,"");err==nil{t.Fatal("missing session ID accepted")}
}
