package main

import (
 "errors"
 "fmt"
 "path/filepath"
 "strings"
)

// resolveFreezeTarget accepts the last positional argument as the ISO output
// filename, or the equivalent -output flag. A relative path is anchored to
// the caller's current working directory, not to the config file directory.
// -name remains optional when an ISO destination was provided.
func resolveFreezeTarget(name,flagOutput string,positionals []string,exportISO bool)(string,string,error){
 if len(positionals)>1{return "","",errors.New("freeze accepts only one positional ISO output path")}
 if len(positionals)==1{
  if flagOutput!=""{return "","",errors.New("use either -output or a positional ISO output path, not both")}
  flagOutput=positionals[0]
 }
 if flagOutput!=""{
  if !exportISO{return "","",errors.New("cannot provide an ISO destination with -export-iso=false")}
  if !strings.EqualFold(filepath.Ext(flagOutput),".iso"){
   return "","",fmt.Errorf("ISO output filename must end in .iso: %q",flagOutput)
  }
  abs,err:=filepath.Abs(flagOutput)
  if err!=nil{return "","",fmt.Errorf("resolve ISO output path: %w",err)}
  if name==""{
   base:=filepath.Base(abs)
   name=base[:len(base)-len(filepath.Ext(base))]
  }
  return name,abs,nil
 }
 if name==""{return "","",errors.New("freeze requires -name or an output .iso filename")}
 return name,"",nil
}
