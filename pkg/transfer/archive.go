// Package transfer creates a safe TAR.GZ from a host path and imports it
// into a running Linux guest over an authenticated SSH connection.
package transfer

import (
 "archive/tar"
 "compress/gzip"
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
)

type Archive struct {
 Path string
 Source string
 Files int
 Directories int
 OriginalBytes int64
 CompressedBytes int64
}

// Compress stores the contents of a directory (not the directory itself),
// or a single file by basename. Symlinks, devices, sockets and other special
// entries are rejected rather than followed or reproduced in a guest.
func Compress(ctx context.Context,source string)(result Archive,err error){
 if ctx==nil{return result,errors.New("context is required")}
 if err=ctx.Err();err!=nil{return result,err}
 if strings.TrimSpace(source)==""{return result,errors.New("source path is required")}
 absolute,err:=filepath.Abs(source)
 if err!=nil{return result,err}
 info,err:=os.Lstat(absolute)
 if err!=nil{return result,fmt.Errorf("inspect source: %w",err)}
 if !info.Mode().IsRegular() && !info.IsDir(){
  return result,errors.New("source must be a regular file or directory (symlinks are not supported)")
 }
 temp,err:=os.CreateTemp("","insula-import-*.tar.gz")
 if err!=nil{return result,err}
 result.Path=temp.Name()
 result.Source=absolute
 defer func(){
  closeErr:=temp.Close()
  if err==nil{err=closeErr}
  if err!=nil{_ = os.Remove(result.Path)}
 }()
 gzipWriter:=gzip.NewWriter(temp)
 tarWriter:=tar.NewWriter(gzipWriter)
 root:=absolute
 if info.Mode().IsRegular(){root=filepath.Dir(absolute)}
 walkErr:=filepath.WalkDir(absolute,func(location string,d os.DirEntry,walkErr error)error{
  if walkErr!=nil{return walkErr}
  if err:=ctx.Err();err!=nil{return err}
  if info.IsDir() && location==absolute{return nil}
  entryInfo,err:=os.Lstat(location)
  if err!=nil{return err}
  if !entryInfo.Mode().IsRegular() && !entryInfo.IsDir(){
   return fmt.Errorf("unsupported entry %q (symlinks and special files are forbidden)",location)
  }
  rel,err:=filepath.Rel(root,location)
  if err!=nil{return err}
  if rel=="."||rel==".."||strings.HasPrefix(rel,".."+string(filepath.Separator)){
   return fmt.Errorf("unsafe archive entry: %q",rel)
  }
  header,err:=tar.FileInfoHeader(entryInfo,"")
  if err!=nil{return err}
  header.Name=filepath.ToSlash(rel)
  if entryInfo.IsDir(){header.Name+="/"}
  // Archive has no external user/group identity. File modes are masked to
  // avoid suid/sgid/sticky bits and extraction never needs elevated rights.
  header.Uid=0
  header.Gid=0
  header.Uname=""
  header.Gname=""
  header.Mode=int64(entryInfo.Mode().Perm())
  if err:=tarWriter.WriteHeader(header);err!=nil{return err}
  if entryInfo.IsDir(){result.Directories++;return nil}
  input,err:=os.Open(location)
  if err!=nil{return err}
  n,copyErr:=io.Copy(tarWriter,&contextReader{ctx:ctx,reader:input})
  closeErr:=input.Close()
  if err:=errors.Join(copyErr,closeErr);err!=nil{return fmt.Errorf("archive %s: %w",location,err)}
  if n!=entryInfo.Size(){return fmt.Errorf("source file changed during compression: %s",location)}
  result.OriginalBytes+=n
  result.Files++
  return nil
 })
 closeErr:=tarWriter.Close()
 gzipErr:=gzipWriter.Close()
 if err=errors.Join(walkErr,closeErr,gzipErr);err!=nil{return result,err}
 stat,err:=temp.Stat()
 if err!=nil{return result,err}
 result.CompressedBytes=stat.Size()
 return result,nil
}

type contextReader struct {
 ctx context.Context
 reader io.Reader
}
func (r *contextReader)Read(p []byte)(int,error){
 if err:=r.ctx.Err();err!=nil{return 0,err}
 return r.reader.Read(p)
}
