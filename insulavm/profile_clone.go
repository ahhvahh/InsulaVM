package insulavm

import (
    "context"
    "errors"
    "fmt"

    "github.com/ahhvahh/InsulaVM/pkg/profile"
    "github.com/ahhvahh/InsulaVM/pkg/storage"
)

// CloneProfile creates an independently stored QCOW2 profile and does not
// modify the source profile. It does not clone a running instance.
func(e *Engine)CloneProfile(ctx context.Context,sourceName,newName string)(*Profile,error){
    if e==nil{return nil,errors.New("engine is required")}
    if ctx==nil{return nil,errors.New("context is required")}
    if err:=ctx.Err();err!=nil{return nil,err}
    if err:=storage.ValidateName(newName);err!=nil{return nil,fmt.Errorf("invalid target profile: %w",err)}
    if sourceName==newName{return nil,errors.New("source and target profiles must differ")}
    original,err:=e.GetProfile(sourceName)
    if err!=nil{return nil,err}
    image,err:=original.ImagePath()
    if err!=nil{return nil,err}
    copy:=*original
    copy.Name=newName
    copy.Version="v001"
    copy.Image="versions/v001/base.qcow2"
    copy.Directory=""
    copy.ISO=""
    copy.SSH=profile.SSH{User:original.SSH.User}
    if err:=e.config.EnsureDirectories();err!=nil{return nil,err}
    if err:=profile.Register(e.config.Storage.Profiles,copy,image);err!=nil{return nil,err}
    return e.GetProfile(newName)
}
