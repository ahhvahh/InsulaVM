package executor

import (
    "context"
    "errors"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

type brokenRead struct{sent bool}
func (r *brokenRead)Read(buf []byte)(int,error){
    if !r.sent {
        r.sent=true
        copy(buf,[]byte("before-error\n"))
        return len("before-error\n"),nil
    }
    return 0,errors.New("simulated SSH stream failure")
}

func TestSSHClientDialRefusesUnverifiedOrMissingCredentials(t *testing.T){
    if _,err:=Dial(DialConfig{});err==nil{t.Fatal("empty SSH configuration was accepted")}
    if _,err:=DialContext(nil,DialConfig{});err==nil{t.Fatal("nil context accepted")}
    cancelled,cancel:=context.WithCancel(context.Background())
    cancel()
    if _,err:=DialContext(cancelled,DialConfig{});!errors.Is(err,context.Canceled){
        t.Fatalf("cancelled SSH dial: %v",err)
    }
    cfg:=DialConfig{
        Address:"127.0.0.1:1",User:"insula",
        PrivateKeyPath:filepath.Join(t.TempDir(),"missing-private-key"),
        KnownHostsPath:filepath.Join(t.TempDir(),"missing-known-hosts"),
        Timeout:100*time.Millisecond,
    }
    if _,err:=DialContext(context.Background(),cfg);err==nil||!strings.Contains(err.Error(),"private key"){
        t.Fatalf("missing private key was not rejected: %v",err)
    }
    if err:=os.WriteFile(cfg.PrivateKeyPath,[]byte("invalid SSH private key"),0600);err!=nil{t.Fatal(err)}
    if _,err:=DialContext(context.Background(),cfg);err==nil||!strings.Contains(err.Error(),"parse private key"){
        t.Fatalf("malformed private key accepted: %v",err)
    }
}

func TestExecutorRefusesUnsafeWorkspaceAndNilRecorder(t *testing.T){
    engine:=&Executor{Workspace:"/tmp"}
    if err:=engine.Run(context.Background(),"job",[]string{"echo ignored"},false,nil);err==nil{
        t.Fatal("nil SSH connection / recorder accepted")
    }
    client:=startTestSSH(t)
    engine.Client=client
    for _,workspace:=range []string{"","/tmp/bad\npath","/tmp/bad\x00path"}{
        engine.Workspace=workspace
        if err:=engine.Run(context.Background(),"job",nil,false,&safeRecorder{});err==nil{
            t.Fatalf("unsafe workspace %q accepted",workspace)
        }
    }
    engine.Workspace=t.TempDir()
    cancelled,cancel:=context.WithCancel(context.Background())
    cancel()
    if err:=engine.Run(cancelled,"job",[]string{"echo should-not-run"},false,&safeRecorder{});!errors.Is(err,context.Canceled){
        t.Fatalf("cancelled SSH executor accepted: %v",err)
    }
}

type safeRecorder struct{}
func (safeRecorder)Request(string)error{return nil}
func (safeRecorder)Response(string)error{return nil}
func (safeRecorder)Flush()error{return nil}

func TestSSHOutputStreamEmitsPartialLineAndStopsOnError(t *testing.T){
    var received []string
    err:=streamLines(&brokenRead{},func(line string)error{
        received=append(received,line)
        return nil
    })
    if err==nil||!strings.Contains(err.Error(),"simulated SSH stream failure"){
        t.Fatalf("read failure lost: %v",err)
    }
    if len(received)!=1||received[0]!="before-error"{
        t.Fatalf("output before error was discarded: %v",received)
    }
    sentinel:=errors.New("log recording interrupted")
    err=streamLines(strings.NewReader("line-1\nline-2\n"),func(line string)error{return sentinel})
    if !errors.Is(err,sentinel){t.Fatalf("recorder failure not propagated: %v",err)}
    err=streamLines(strings.NewReader("line-without-newline"),func(line string)error{
        if line!="line-without-newline"{t.Errorf("bad final line: %q",line)}
        return nil
    })
    if err!=nil{t.Fatalf("unterminated last line rejected: %v",err)}
    _,_=io.Copy(io.Discard,strings.NewReader(""))
}
