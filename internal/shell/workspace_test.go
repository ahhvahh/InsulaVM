package shell

import (
 "context"
 "testing"
)

func TestEnsureWorkspaceRejectsInvalidInputs(t *testing.T) {
 if err:=EnsureWorkspace(nil,nil,"/tmp/workspace");err==nil{t.Fatal("nil context accepted")}
 if err:=EnsureWorkspace(context.Background(),nil,"/tmp/workspace");err==nil{t.Fatal("nil SSH client accepted")}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=EnsureWorkspace(ctx,nil,"/tmp/workspace");err==nil{t.Fatal("nil SSH client accepted under cancelled context")}
}
