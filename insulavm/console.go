package insulavm

import (
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "os/exec"
)

// OpenConsole attaches the caller's streams to the Libvirt serial console.
// The guest must have a working serial console. This is NOT an SSH shell.
func(e *Engine)OpenConsole(ctx context.Context,id string,stdin io.Reader,stdout,stderr io.Writer)error{
 if e==nil{return errors.New("engine is required")}
 if ctx==nil{return errors.New("context is required")}
 if id==""{return errors.New("VM ID is required")}
 checked,err:=e.GetVMStatus(ctx,id)
 if err!=nil{return err}
 if checked.Record.ID!=id||checked.Record.LibvirtUUID!=id{
  return errors.New("VM identity mismatch; console refused")
 }
 if !checked.IsRunning(){return fmt.Errorf("VM is not running (%s)",checked.LibvirtState)}
 cmd:=exec.CommandContext(ctx,"virsh","-c",e.config.Virtualization.LibvirtURI,"console",id)
 cmd.Stdin=stdin
 cmd.Stdout=stdout
 cmd.Stderr=stderr
 cmd.Env=append(os.Environ(),"LANG=C","LC_ALL=C")
 return cmd.Run()
}
