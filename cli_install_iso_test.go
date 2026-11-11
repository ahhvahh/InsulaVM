package main

import (
 "strings"
 "testing"
)

func TestCLIInstallerISORequiresProfile(t *testing.T){
 cfg,_:=cliConfigFixture(t)
 err:=run([]string{"vm","install-iso","-config",cfg})
 if err==nil||!strings.Contains(err.Error(),"requires -profile"){
  t.Fatalf("installer ISO without profile should fail, got %v",err)
 }
}
