package main

import (
 "errors"
 "os"
 "path/filepath"
 "strings"
)

// localPathForCLI presents host paths relative to the caller's current
// directory. Internal file access and the Go API retain absolute paths.
// If the current directory cannot be determined, retain the original path.
func localPathForCLI(path string) string {
 if path=="" || !filepath.IsAbs(path) {return path}
 cwd,err:=os.Getwd()
 if err!=nil{return path}
 relative,err:=filepath.Rel(cwd,path)
 if err!=nil{return path}
 return relative
}

// localMessageForCLI rewrites the configured private storage prefix in
// human-readable VM messages. Structured errors retain their original paths.
func localMessageForCLI(message, storageRoot string) string {
 if storageRoot=="" || !filepath.IsAbs(storageRoot) {return message}
 relative:=localPathForCLI(storageRoot)
 if relative==storageRoot{return message}
 root:=filepath.Clean(storageRoot)
 sep:=string(filepath.Separator)
 // The boundary prevents matching a sibling directory, e.g. data-old.
 message=strings.ReplaceAll(message,root+sep,relative+sep)
 return message
}

// localErrorForCLI preserves errors.Is/As for callers while shortening only
// the text emitted by the terminal.
type localErrorForCLI struct{
 cause error
 storageRoot string
}

func (e localErrorForCLI) Error()string{return localMessageForCLI(e.cause.Error(),e.storageRoot)}
func (e localErrorForCLI) Unwrap()error{return e.cause}
func presentCLIError(err error,storageRoot string)error{
 if err==nil{return nil}
 if storageRoot==""{return err}
 return localErrorForCLI{cause:err,storageRoot:storageRoot}
}

// shellQuote safely wraps a path for a POSIX shell (spaces, $ and quotes).
func shellQuote(value string) string{
 return "'"+strings.ReplaceAll(value,"'","'\\''")+"'"
}

// sshCommandForCLI produces a paste-ready command for the current terminal
// directory. Both files are still looked up by SSH exactly as before.
func sshCommandForCLI(key,knownHosts,user,host string)string{
 return "ssh -i "+shellQuote(localPathForCLI(key))+
  " -o "+shellQuote("UserKnownHostsFile="+localPathForCLI(knownHosts))+
  " "+shellQuote(user+"@"+host)
}

var _ error = localErrorForCLI{}
var _ interface{Unwrap()error} = localErrorForCLI{}
var _ = errors.Is
