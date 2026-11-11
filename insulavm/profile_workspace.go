package insulavm

import (
 "errors"
 "os"
)

// ResolveVMWorkspace returns the per-profile guest destination, falling back
// to the global configuration for old profiles and absent profile metadata.
// It does not require the destination to exist inside the running VM.
func (e *Engine) ResolveVMWorkspace(profileName string)(string,error){
 if e==nil{return "",errors.New("engine is required")}
 fallback:=e.config.Guest.Destination
 if profileName==""{return fallback,nil}
 p,err:=e.GetProfile(profileName)
 if err!=nil{
  if errors.Is(err,os.ErrNotExist){return fallback,nil}
  return "",err
 }
 if p.Workspace.Destination!=""{return p.Workspace.Destination,nil}
 return fallback,nil
}
