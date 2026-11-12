package installer

import (
 "bufio"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "fmt"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "time"
)

const debian13InstallerImages = "https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images/"

// DownloadDebianMiniISO downloads the Debian 13 amd64 netboot mini.iso into
// outputDir, verifies it against the official SHA256SUMS and returns its path.
// It never replaces a cached file whose checksum differs without intervention.
// This validates integrity over HTTPS, not a GPG signature.
func DownloadDebianMiniISO(ctx context.Context,outputDir string)(string,error){
 return downloadDebianMiniISO(ctx,&http.Client{Timeout:45*time.Minute},debian13InstallerImages,outputDir)
}

func downloadDebianMiniISO(ctx context.Context,client *http.Client,base,dir string)(string,error){
 if ctx==nil{return "",errors.New("context required")}
 if strings.TrimSpace(dir)==""{return "",errors.New("output directory required")}
 if client==nil{return "",errors.New("HTTP client required")}
 if err:=os.MkdirAll(dir,0700);err!=nil{return "",err}
 dest:=filepath.Join(dir,"mini.iso")
 if stat,err:=os.Lstat(dest);err==nil{
  if !stat.Mode().IsRegular(){return "",errors.New("mini.iso cache is not a regular file")}
 }else if !errors.Is(err,os.ErrNotExist){return "",err}
 fetch:=func(path string)(*http.Response,error){
  req,err:=http.NewRequestWithContext(ctx,http.MethodGet,base+path,nil)
  if err!=nil{return nil,err}
  resp,err:=client.Do(req)
  if err!=nil{return nil,err}
  if resp.StatusCode!=http.StatusOK{
   _=resp.Body.Close()
   return nil,fmt.Errorf("Debian download %s: HTTP %d",path,resp.StatusCode)
  }
  return resp,nil
 }
 sums,err:=fetch("SHA256SUMS")
 if err!=nil{return "",err}
 scan:=bufio.NewScanner(io.LimitReader(sums.Body,2*1024*1024))
 expected:=""
 for scan.Scan(){
  fields:=strings.Fields(scan.Text())
  if len(fields)==2 && strings.TrimPrefix(fields[1],"./")=="netboot/mini.iso"{
   if expected!=""{_ = sums.Body.Close();return "",errors.New("duplicate mini.iso in SHA256SUMS")}
   expected=strings.ToLower(fields[0])
  }
 }
 scanErr:=scan.Err()
 _=sums.Body.Close()
 if scanErr!=nil{return "",fmt.Errorf("read Debian checksums: %w",scanErr)}
 if len(expected)!=64{return "",errors.New("Debian SHA256SUMS lacks mini.iso")}
 if _,err:=hex.DecodeString(expected);err!=nil{return "",fmt.Errorf("invalid mini.iso checksum: %w",err)}
 if _,err:=os.Stat(dest);err==nil{
  checksum,err:=SHA256File(ctx,dest)
  if err!=nil{return "",err}
  if checksum!=expected{return "",errors.New("mini.iso in cache has unexpected checksum; remove it before retrying")}
  return dest,nil
 }
 resp,err:=fetch("netboot/mini.iso")
 if err!=nil{return "",err}
 defer resp.Body.Close()
 tmp,err:=os.CreateTemp(dir,".mini-*.part")
 if err!=nil{return "",err}
 defer os.Remove(tmp.Name())
 sum:=sha256.New()
 _,copyErr:=io.Copy(io.MultiWriter(tmp,sum),io.LimitReader(resp.Body,512*1024*1024+1))
 closeErr:=tmp.Close()
 if copyErr!=nil{return "",copyErr}
 if closeErr!=nil{return "",closeErr}
 if hex.EncodeToString(sum.Sum(nil))!=expected{return "",errors.New("Debian mini.iso SHA256 mismatch")}
 if err:=os.Rename(tmp.Name(),dest);err!=nil{return "",err}
 return dest,nil
}

