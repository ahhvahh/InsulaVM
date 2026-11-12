package installer

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestDownloadDebianMiniISOAndCache(t *testing.T){
 iso:=[]byte("fake Debian installer for checksum test")
 sum:=sha256.Sum256(iso)
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path{
  case "/SHA256SUMS":fmt.Fprintf(w,"%x  ./netboot/mini.iso\n",sum)
  case "/netboot/mini.iso":_,_=w.Write(iso)
  default:http.NotFound(w,r)
  }
 }))
 defer server.Close()
 dir:=t.TempDir()
 for i:=0;i<2;i++{
  path,err:=downloadDebianMiniISO(context.Background(),server.Client(),server.URL+"/",dir)
  if err!=nil{t.Fatal(err)}
  if filepath.Base(path)!="mini.iso"{t.Fatalf("unexpected path: %s",path)}
  data,err:=os.ReadFile(path)
  if err!=nil||string(data)!=string(iso){t.Fatalf("bad cached bytes: %v %q",err,data)}
 }
 if err:=os.WriteFile(filepath.Join(dir,"mini.iso"),[]byte("corrupted"),0600);err!=nil{t.Fatal(err)}
 if _,err:=downloadDebianMiniISO(context.Background(),server.Client(),server.URL+"/",dir);err==nil||!strings.Contains(err.Error(),"unexpected checksum"){
  t.Fatalf("corrupt cache accepted: %v",err)
 }
}

func TestDownloadDebianMiniISORejectsWrongChecksum(t *testing.T){
 bad:=strings.Repeat("0",64)
 if _,err:=hex.DecodeString(bad);err!=nil{t.Fatal(err)}
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/SHA256SUMS"{fmt.Fprintf(w,"%s  ./netboot/mini.iso\n",bad);return}
  _,_=w.Write([]byte("not a matching ISO"))
 }))
 defer server.Close()
 dir:=t.TempDir()
 _,err:=downloadDebianMiniISO(context.Background(),server.Client(),server.URL+"/",dir)
 if err==nil||!strings.Contains(err.Error(),"SHA256 mismatch"){t.Fatalf("wrong checksum accepted: %v",err)}
 if _,err:=os.Stat(filepath.Join(dir,"mini.iso"));!os.IsNotExist(err){t.Fatalf("corrupt ISO published: %v",err)}
}
