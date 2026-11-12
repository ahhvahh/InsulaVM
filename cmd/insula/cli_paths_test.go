package main

import (
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
)

func chdirForCLIPathTest(t *testing.T, dir string){
 t.Helper()
 old,err:=os.Getwd()
 if err!=nil{t.Fatal(err)}
 if err:=os.Chdir(dir);err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ = os.Chdir(old)})
}

func TestRelativeOutputPathsAndProgress(t *testing.T){
 project:=t.TempDir()
 chdirForCLIPathTest(t,project)
 root:=filepath.Join(project,"data")
 image:=filepath.Join(root,"profiles","debian13","versions","v001","base.qcow2")
 key:=filepath.Join(root,"runs","insula-vm-123","client_ed25519")
 if got,want:=localPathForCLI(image),filepath.Join("data","profiles","debian13","versions","v001","base.qcow2");got!=want{
  t.Fatalf("image displayed as %q, expected %q",got,want)
 }
 if got,want:=localPathForCLI(key),filepath.Join("data","runs","insula-vm-123","client_ed25519");got!=want{
  t.Fatalf("SSH key displayed as %q, expected %q",got,want)
 }
 message:="Imagem-base validada: "+image
 got:=localMessageForCLI(message,root)
 if got!="Imagem-base validada: "+filepath.Join("data","profiles","debian13","versions","v001","base.qcow2"){
  t.Fatalf("unexpected progress message %q",got)
 }
 if strings.Contains(got,project){
  t.Fatalf("absolute local project directory leaked: %q",got)
 }
 // A similarly prefixed directory must not be changed by accident.
 outside:=root+"-backup/image.qcow2"
 if got:=localMessageForCLI(outside,root);got!=outside{
  t.Fatalf("unrelated path changed: %q",got)
 }
}

func TestCLIErrorRelativeWhileRetainingCause(t *testing.T){
 project:=t.TempDir()
 chdirForCLIPathTest(t,project)
 root:=filepath.Join(project,"data")
 file:=filepath.Join(root,"profiles","debian13","base.qcow2")
 cause:=fmt.Errorf("resolve profile image: %w",&os.PathError{Op:"open",Path:file,Err:os.ErrPermission})
 outputErr:=presentCLIError(cause,root)
 if !errors.Is(outputErr,os.ErrPermission){t.Fatal("error cause was lost")}
 if strings.Contains(outputErr.Error(),project){t.Fatalf("unexpected absolute path: %s",outputErr)}
 if !strings.Contains(outputErr.Error(),filepath.Join("data","profiles","debian13","base.qcow2")){
  t.Fatalf("expected relative path: %s",outputErr)
 }
}

func TestSSHCommandIsRelativeAndSafelyQuoted(t *testing.T){
 project:=t.TempDir()
 chdirForCLIPathTest(t,project)
 key:=filepath.Join(project,"data","runs","special $USER's dir","client_ed25519")
 known:=filepath.Join(project,"data","runs","special $USER's dir","known_hosts")
 if err:=os.MkdirAll(filepath.Dir(key),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(key,[]byte("dummy"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(known,[]byte("dummy"),0600);err!=nil{t.Fatal(err)}
 command:=sshCommandForCLI(key,known,"insula","192.168.122.107")
 if strings.Contains(command,project){t.Fatalf("absolute prefix leaked: %s",command)}
 // Parse as a POSIX command, without attempting an SSH connection.
 parsed,err:=exec.Command("sh","-c","set -- "+command+"; printf '%s\\n' \"$@\"").CombinedOutput()
 if err!=nil{t.Fatalf("SSH command is not valid shell syntax: %v: %s",err,parsed)}
 args:=strings.Split(strings.TrimSuffix(string(parsed),"\n"),"\n")
 expected:=[]string{"ssh","-i",localPathForCLI(key),"-o","UserKnownHostsFile="+localPathForCLI(known),"insula@192.168.122.107"}
 if len(args)!=len(expected){t.Fatalf("invalid SSH argument count: %q",args)}
 for i,want:=range expected{
  if args[i]!=want{t.Fatalf("SSH argument %d = %q, expected %q",i,args[i],want)}
 }
 if _,err:=os.Stat(args[2]);err!=nil{t.Fatalf("relative SSH identity file does not resolve: %v",err)}
}
