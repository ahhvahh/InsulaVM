package shell

import (
 "context"
 "errors"
 "fmt"

 "golang.org/x/crypto/ssh"
)

// EnsureWorkspace prepares the configured working directory as the
// authenticated, non-root SSH user; no implicit sudo is performed.
// Callers who choose privileged locations must provision permissions first.
func EnsureWorkspace(ctx context.Context,client *ssh.Client,destination string)error{
 if ctx==nil||client==nil{return errors.New("context and SSH client are required")}
 if err:=ctx.Err();err!=nil{return err}
 session,err:=client.NewSession()
 if err!=nil{return err}
 defer session.Close()
 stop:=context.AfterFunc(ctx,func(){_ = session.Close()})
 defer stop()
 output,err:=session.CombinedOutput("mkdir -p -- "+QuoteGuest(destination))
 if ctx.Err()!=nil{return ctx.Err()}
 if err!=nil{return fmt.Errorf("prepare VM directory %q: %w: %s",destination,err,string(output))}
 return nil
}
