package shell

import (
 "context"
 "testing"
 "errors"
 "golang.org/x/crypto/ssh"
)

func TestEnsureWorkspaceRejectsInvalidInputs(t *testing.T) {
 if err:=EnsureWorkspace(nil,nil,"/tmp/workspace");err==nil{t.Fatal("nil context accepted")}
 if err:=EnsureWorkspace(context.Background(),nil,"/tmp/workspace");err==nil{t.Fatal("nil SSH client accepted")}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=EnsureWorkspace(ctx,new(ssh.Client),"/tmp/workspace");!errors.Is(err,context.Canceled){t.Fatalf("cancelled request accepted: %v",err)}
}
