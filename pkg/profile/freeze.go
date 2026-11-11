package profile

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"

 "github.com/ahhvahh/InsulaVM/pkg/storage"
 "gopkg.in/yaml.v3"
)

// FreezeOptions controls native QCOW2 base image conversion.
// Compressed clusters remain readable by QEMU and are used as immutable backing
// storage for independent writable overlays.
type FreezeOptions struct {
 Compress bool
}

// FreezeFromOverlay preserves the existing default uncompressed behavior.
func FreezeFromOverlay(ctx context.Context,root string,original Profile,name,overlay string)(string,error){
 return FreezeFromOverlayWithOptions(ctx,root,original,name,overlay,FreezeOptions{})
}

// FreezeFromOverlayWithOptions publishes an independent QCOW2 profile.
// The stopped source overlay is never modified; failed exports are removed.
func FreezeFromOverlayWithOptions(ctx context.Context,root string,original Profile,name,overlay string,options FreezeOptions)(string,error){
 if ctx==nil {return "",errors.New("context is required")}
 if err:=ctx.Err();err!=nil{return "",err}
 if err:=storage.ValidateName(name);err!=nil{return "",fmt.Errorf("freeze profile name: %w",err)}
 if strings.TrimSpace(root)==""{return "",errors.New("profiles root is required")}
 if strings.TrimSpace(overlay)==""{return "",errors.New("VM overlay path is required")}
 st,err:=os.Lstat(overlay)
 if err!=nil{return "",fmt.Errorf("inspect VM overlay: %w",err)}
 if !st.Mode().IsRegular(){return "",errors.New("VM overlay must be a regular file")}
 if err:=os.MkdirAll(root,0700);err!=nil{return "",err}

 target:=filepath.Join(root,name)
 if err:=os.Mkdir(target,0700);err!=nil{
  return "",fmt.Errorf("create frozen profile %q (must not already exist): %w",name,err)
 }
 success:=false
 defer func(){if !success{_ = os.RemoveAll(target)}}()

 saved:=original
 saved.Name=name
 saved.Version="v001"
 saved.Image="versions/v001/base.qcow2"
 saved.ISO=""
 saved.Directory=target
 saved.Guest.Destination=saved.GuestDestination("")
 // Never persist host access paths/identity from an existing source VM.
 saved.SSH=SSH{User:original.SSH.User}
 if err:=saved.Validate();err!=nil{return "",err}

 image:=filepath.Join(target,saved.Image)
 if err:=os.MkdirAll(filepath.Dir(image),0700);err!=nil{return "",err}
 args:=[]string{"convert","-f","qcow2","-O","qcow2"}
 if options.Compress { args=append(args,"-c") }
 args=append(args,overlay,image)
 output,err:=exec.CommandContext(ctx,"qemu-img",args...).CombinedOutput()
 if err!=nil{return "",fmt.Errorf("flatten VM overlay into reusable QCOW2: %w: %s",err,strings.TrimSpace(string(output)))}
 if err:=os.Chmod(image,0600);err!=nil{return "",err}

 infoOutput,err:=exec.CommandContext(ctx,"qemu-img","info","--output=json",image).CombinedOutput()
 if err!=nil{return "",fmt.Errorf("inspect frozen QCOW2: %w: %s",err,strings.TrimSpace(string(infoOutput)))}
 var info struct{
  Format string `json:"format"`
  BackingFilename string `json:"backing-filename"`
 }
 if err:=json.Unmarshal(infoOutput,&info);err!=nil{return "",fmt.Errorf("decode frozen image details: %w",err)}
 if info.Format!="qcow2"||info.BackingFilename!=""{
  return "",fmt.Errorf("frozen image must be standalone QCOW2; format=%q backing=%q",info.Format,info.BackingFilename)
 }
 if _,err:=saved.ImagePath();err!=nil{return "",fmt.Errorf("validate frozen image: %w",err)}
 data,err:=yaml.Marshal(&saved)
 if err!=nil{return "",err}
 if err:=os.WriteFile(filepath.Join(target,"profile.yaml"),data,0600);err!=nil{return "",err}
 success=true
 return image,nil
}
