package insulavm

import (
 "os"
 "path/filepath"
 "testing"
)

// fakeRunningLibvirt lets SSH integration tests exercise the real managed-VM
// status verification without requiring host virtualization on GitHub Actions.
func fakeRunningLibvirt(t *testing.T, id string) {
 t.Helper()
 dir:=t.TempDir()
 script:="#!/bin/sh\n"+
  "case \"$3\" in\n"+
  "  list) printf '%s\\n' '"+id+"' ;;\n"+
  "  domstate) printf 'running\\n' ;;\n"+
  "  *) exit 8 ;;\n"+
  "esac\n"
 if err:=os.WriteFile(filepath.Join(dir,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
