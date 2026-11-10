package transfer

import (
    "context"
    "crypto/ed25519"
    "crypto/rand"
    "encoding/binary"
    "errors"
    "fmt"
    "io"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "golang.org/x/crypto/ssh"
)

// This loopback SSH server runs real POSIX shell commands and TAR extraction.
// It is not a VM/Libvirt mock and needs no privileged ports or KVM.
func localUploadSSH(t *testing.T) *ssh.Client {
    t.Helper()
    _, private, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    signer, err := ssh.NewSignerFromKey(private)
    if err != nil { t.Fatal(err) }
    serverConfig := &ssh.ServerConfig{NoClientAuth: true}
    serverConfig.AddHostKey(signer)
    listener, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Fatal(err) }
    t.Cleanup(func(){ _ = listener.Close() })
    go func(){
        for {
            raw, err := listener.Accept()
            if err != nil { return }
            go func(raw net.Conn){
                connection, incoming, requests, err := ssh.NewServerConn(raw, serverConfig)
                if err != nil { _ = raw.Close(); return }
                defer connection.Close()
                go ssh.DiscardRequests(requests)
                for open := range incoming {
                    if open.ChannelType() != "session" {
                        _ = open.Reject(ssh.UnknownChannelType, "only sessions allowed")
                        continue
                    }
                    channel, sessionRequests, err := open.Accept()
                    if err != nil { continue }
                    go func(){
                        defer channel.Close()
                        for request := range sessionRequests {
                            if request.Type != "exec" {
                                _ = request.Reply(false,nil)
                                continue
                            }
                            if len(request.Payload) < 4 { _ = request.Reply(false,nil); return }
                            length := int(binary.BigEndian.Uint32(request.Payload[:4]))
                            if length > len(request.Payload)-4 { _ = request.Reply(false,nil); return }
                            command := string(request.Payload[4:4+length])
                            _ = request.Reply(true,nil)
                            process := exec.Command("sh","-c",command)
                            process.Stdin = channel
                            process.Stdout = channel
                            process.Stderr = channel.Stderr()
                            runErr := process.Run()
                            var status uint32
                            if runErr != nil {
                                var exit *exec.ExitError
                                if errors.As(runErr,&exit) { status = uint32(exit.ExitCode()) } else { status = 255 }
                            }
                            reply := make([]byte,4)
                            binary.BigEndian.PutUint32(reply,status)
                            _, _ = channel.SendRequest("exit-status",false,reply)
                            return
                        }
                    }()
                }
            }(raw)
        }
    }()
    client, err := ssh.Dial("tcp",listener.Addr().String(),&ssh.ClientConfig{
        User:"unit-test",HostKeyCallback:ssh.FixedHostKey(signer.PublicKey()),Timeout:3*time.Second,
    })
    if err != nil { t.Fatal(err) }
    t.Cleanup(func(){ _ = client.Close() })
    return client
}

func TestUploadRealSSHArchiveAndCleanup(t *testing.T){
    client := localUploadSSH(t)
    root := t.TempDir()
    source := filepath.Join(root,"payload")
    if err:=os.Mkdir(source,0700);err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(filepath.Join(source,"file.txt"),[]byte("InsulaVM payload with quote ' and space"),0600);err!=nil{t.Fatal(err)}
    archive,err:=Compress(context.Background(),source)
    if err!=nil{t.Fatal(err)}
    defer os.Remove(archive.Path)
    destination:=filepath.Join(root,"guest's workspace")
    var stages []string
    var sent int64
    err=Upload(context.Background(),client,archive,destination,func(n,total int64){
        if n>total||n<sent{t.Errorf("invalid transfer progress %d of %d",n,total)}
        sent=n
    },func(stage string){stages=append(stages,stage)})
    if err!=nil{t.Fatalf("real SSH archive transfer failed: %v",err)}
    result,err:=os.ReadFile(filepath.Join(destination,"file.txt"))
    if err!=nil{t.Fatal(err)}
    if string(result)!="InsulaVM payload with quote ' and space"{t.Fatalf("file corrupted: %q",result)}
    if sent!=archive.CompressedBytes{t.Fatalf("progress final=%d expected=%d",sent,archive.CompressedBytes)}
    if got:=strings.Join(stages,",");got!="uploading,verifying,extracting"{
        t.Fatalf("missing SSH import stages: %v",stages)
    }
}

func TestUploadSSHFailuresDoNotReportSuccess(t *testing.T){
    client:=localUploadSSH(t)
    ctx:=context.Background()
    file:=filepath.Join(t.TempDir(),"payload.bin")
    if err:=os.WriteFile(file,[]byte("abc"),0600);err!=nil{t.Fatal(err)}
    tests:=[]struct{name string;archive Archive;dest string;want string}{
        {"missing archive",Archive{Path:filepath.Join(t.TempDir(),"absent.tar.gz"),CompressedBytes:3},t.TempDir(),"transfer compressed content"},
        {"incorrect compressed size",Archive{Path:file,CompressedBytes:999},t.TempDir(),"archive read size changed"},
        {"unsafe destination",Archive{Path:file,CompressedBytes:3},"/","destination"},
        {"newline in destination",Archive{Path:file,CompressedBytes:3},"/tmp/unsafe\npath","destination"},
    }
    for _,tc:=range tests{
        t.Run(tc.name,func(t *testing.T){
            err:=Upload(ctx,client,tc.archive,tc.dest,nil,nil)
            if err==nil||!strings.Contains(err.Error(),tc.want){
                t.Fatalf("expected error containing %q, got %v",tc.want,err)
            }
        })
    }
    cancelled,cancel:=context.WithCancel(ctx)
    cancel()
    if err:=Upload(cancelled,client,Archive{Path:file,CompressedBytes:3},t.TempDir(),nil,nil);!errors.Is(err,context.Canceled){
        t.Fatalf("cancellation ignored: %v",err)
    }
}

func TestSSHCommandResponseBoundsAndErrorText(t *testing.T){
    client:=localUploadSSH(t)
    ctx:=context.Background()
    output,err:=runCommand(ctx,client,"printf 'hello-ssh'")
    if err!=nil||output!="hello-ssh"{t.Fatalf("unexpected ssh output: %q %v",output,err)}
    _,err=runCommand(ctx,client,"echo explicit-guest-error >&2; exit 23")
    if err==nil||!strings.Contains(err.Error(),"explicit-guest-error"){t.Fatalf("lost stderr: %v",err)}
    _,err=runCommand(ctx,client,"head -c 20000 /dev/zero")
    if err==nil||!strings.Contains(err.Error(),"large guest command output"){t.Fatalf("stdout not bounded: %v",err)}
    cancelled,cancel:=context.WithCancel(ctx)
    cancel()
    _,err=runCommand(cancelled,client,"echo not-executed")
    if !errors.Is(err,context.Canceled){t.Fatalf("cancelled SSH command: %v",err)}
}

func TestRemoteTransferHelpers(t *testing.T){
    if got:=shellQuote("a'b");got!="'a'\"'\"'b'"{t.Fatalf("unsafe shell quote: %q",got)}
    reader:=&countingReader{source:strings.NewReader("example"),read:func(n int){}}
    buf:=make([]byte,20)
    n,err:=reader.Read(buf)
    if err!=nil||string(buf[:n])!="example"{t.Fatalf("counting reader: %d %v",n,err)}
    var count int
    reader=&countingReader{source:strings.NewReader("abc"),read:func(n int){count+=n}}
    _,_=io.Copy(io.Discard,reader)
    if count!=3{t.Fatalf("reader did not count bytes: %d",count)}
    bounded:=&limitedOutput{remaining:4}
    if n,err:=fmt.Fprint(bounded,"abcdef");err!=nil||n!=6{t.Fatalf("write result: %d %v",n,err)}
    if !bounded.truncated||bounded.String()!="abcd"{t.Fatalf("output was not bounded: %+v",bounded)}
    if n,err:=fmt.Fprint(bounded,"xyz");err!=nil||n!=3{t.Fatalf("write after cap: %d %v",n,err)}
    if bounded.String()!="abcd"{t.Fatalf("data past cap retained: %q",bounded.String())}
}
