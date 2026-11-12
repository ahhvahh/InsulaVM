package shell

import (
 "bufio"
 "bytes"
 "context"
 "io"
 "strings"
 "testing"
)

func TestReadShellReplyPreservesBytesAndExitCode(t *testing.T){
 marker:="INSULAVM:unique:"
 tests:=[]struct{
  name string
  stream []byte
  want []byte
  code int
  fail bool
  limit int
 }{
  {"success",[]byte("listing\n\x1e"+marker+"0\x1f"),[]byte("listing\n"),0,false,1024},
  {"nonzero",[]byte("missing\n\x1e"+marker+"2\x1f"),[]byte("missing\n"),2,false,1024},
  {"binary",append([]byte{'a',0,'b','\x1e'},append([]byte("OTHER"),[]byte("\x1e"+marker+"0\x1f")...)...),append([]byte{'a',0,'b','\x1e'},[]byte("OTHER")...),0,false,1024},
  {"unexpected EOF",[]byte("partial"),[]byte("partial"),-1,true,1024},
  {"bounded output",[]byte("123456\x1e"+marker+"0\x1f"),[]byte("1234"),-1,true,4},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   got,code,err:=readShellReply(bufio.NewReader(bytes.NewReader(tc.stream)),marker,tc.limit)
   if (err!=nil)!=tc.fail{t.Fatalf("error=%v, expected fail=%t",err,tc.fail)}
   if !bytes.Equal(got,tc.want)||code!=tc.code{t.Fatalf("got %q exit %d, wanted %q exit %d",got,code,tc.want,tc.code)}
  })
 }
}

func TestShellExchangeMultipleCommandsOnOneConnection(t *testing.T){
 inputReader,inputWriter:=io.Pipe()
 outputReader,outputWriter:=io.Pipe()
 defer inputReader.Close()
 defer inputWriter.Close()
 defer outputWriter.Close()
 sess:=&Session{stdin:inputWriter,stdout:bufio.NewReader(outputReader)}
 done:=make(chan error,1)
 go func(){
  defer outputWriter.Close()
  r:=bufio.NewReader(inputReader)
  for i:=0;i<2;i++{
   command,err:=r.ReadString('\n')
   if err!=nil{done<-err;return}
   markerLine,err:=r.ReadString('\n')
   if err!=nil{done<-err;return}
   start:=strings.Index(markerLine,"INSULAVM:")
   if start<0{done<-io.ErrUnexpectedEOF;return}
   end:=strings.Index(markerLine[start:],":%d")
   if end<0{done<-io.ErrUnexpectedEOF;return}
   marker:=markerLine[start:start+end+1]
   if _,err:=io.WriteString(outputWriter,strings.TrimSuffix(command,"\n")+"\n\x1e"+marker+"0\x1f");err!=nil{done<-err;return}
  }
  done<-nil
 }()
 for _,command:=range []string{"pwd","ls"}{
  output,code,err:=Exchange(context.Background(),sess,command)
  if err!=nil||code!=0||string(output)!=command+"\n"{t.Fatalf("exchange %q: %q %d %v",command,output,code,err)}
 }
 if err:=<-done;err!=nil{t.Fatal(err)}
}

func TestShellCommandValidation(t *testing.T){
 for _,bad:=range []string{"","   ","echo\x00foo",strings.Repeat("x",maxShellCommandBytes+1)}{
  if err:=ValidateCommand(bad);err==nil{t.Errorf("accepted invalid command of length %d",len(bad))}
 }
 if err:=ValidateCommand("cd /tmp && ls");err!=nil{t.Fatal(err)}
}
