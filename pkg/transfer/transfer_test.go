package transfer

import (
 "archive/tar"
 "compress/gzip"
 "context"
 "errors"
 "io"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func archiveNames(t *testing.T,archive Archive)[]string{
 t.Helper()
 f,err:=os.Open(archive.Path)
 if err!=nil{t.Fatal(err)}
 defer f.Close()
 z,err:=gzip.NewReader(f)
 if err!=nil{t.Fatal(err)}
 defer z.Close()
 tr:=tar.NewReader(z)
 var names []string
 for {
  h,err:=tr.Next()
  if errors.Is(err,io.EOF){break}
  if err!=nil{t.Fatal(err)}
  names=append(names,h.Name)
  if filepath.IsAbs(h.Name)||strings.Contains(h.Name,"../"){
   t.Fatalf("unsafe archive member %q",h.Name)
  }
  if h.Typeflag!=tar.TypeDir && h.Typeflag!=tar.TypeReg && h.Typeflag!=tar.TypeRegA{
   t.Fatalf("unexpected archive type %q",h.Typeflag)
  }
 }
 return names
}

func TestCompressDirectoryContents(t *testing.T){
 root:=t.TempDir()
 if err:=os.Mkdir(filepath.Join(root,"sub"),0755);err!=nil{t.Fatal(err)}
 data:=strings.Repeat("hello world",4000)
 if err:=os.WriteFile(filepath.Join(root,"sub","data.txt"),[]byte(data),0600);err!=nil{t.Fatal(err)}
 archive,err:=Compress(context.Background(),root)
 if err!=nil{t.Fatal(err)}
 defer os.Remove(archive.Path)
 if archive.Files!=1||archive.Directories!=1||archive.OriginalBytes!=int64(len(data)){
  t.Fatalf("archive counts %+v",archive)
 }
 names:=archiveNames(t,archive)
 if len(names)!=2||names[0]!="sub/"||names[1]!="sub/data.txt"{
  t.Fatalf("directory archive includes wrong roots: %v",names)
 }
}

func TestCompressSingleFile(t *testing.T){
 root:=t.TempDir()
 file:=filepath.Join(root,"file with spaces.txt")
 if err:=os.WriteFile(file,[]byte("payload"),0600);err!=nil{t.Fatal(err)}
 a,err:=Compress(context.Background(),file)
 if err!=nil{t.Fatal(err)}
 defer os.Remove(a.Path)
 names:=archiveNames(t,a)
 if len(names)!=1||names[0]!="file with spaces.txt"{
  t.Fatalf("single-file archive: %v",names)
 }
}

func TestCompressRejectsSymlinksAndCancelledContext(t *testing.T){
 root:=t.TempDir()
 if err:=os.Symlink("/etc/passwd",filepath.Join(root,"external"));err!=nil{t.Fatal(err)}
 if _,err:=Compress(context.Background(),root);err==nil{
  t.Fatal("source directory symlink was accepted")
 }
 if _,err:=Compress(context.Background(),filepath.Join(root,"external"));err==nil{
  t.Fatal("source symlink was accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=Compress(ctx,root);!errors.Is(err,context.Canceled){t.Fatalf("expected cancellation: %v",err)}
}

func TestUploadValidatesDestinationBeforeSSH(t *testing.T){
 for _,bad:=range []string{"","relative/path","/","/tmp/unsafe\nnewline"}{
  err:=Upload(context.Background(),nil,Archive{},bad,nil,nil)
  if err==nil{t.Fatalf("allowed invalid import: %q",bad)}
 }
}
