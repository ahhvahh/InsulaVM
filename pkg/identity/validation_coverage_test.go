package identity

import (
    "strings"
    "testing"
)

func TestUserValidationForCloudInitAndSSH(t *testing.T){
    valid:=[]string{"insula","_service","dev-ops","a","u123","a_b"}
    for _,user:=range valid{
        if !ValidUser(user){t.Errorf("rejected valid non-root Linux user %q",user)}
    }
    invalid:=[]string{"root","","Root","User","-user","9user","a.b","a;b","a b","a\nroot","à","a/b",strings.Repeat("a",33)}
    for _,user:=range invalid{
        if ValidUser(user){t.Errorf("allowed unsafe Linux user %q",user)}
    }
    if got:=Fingerprint(nil);got!=""{t.Fatalf("nil SSH key should not yield fingerprint: %q",got)}
    if err:=SaveKnownHost(t.TempDir()+"/hosts","invalid-ip",nil);err==nil{
        t.Fatal("known_hosts accepted an invalid VM IP")
    }
    if err:=SaveKnownHost(t.TempDir()+"/hosts","192.0.2.2",nil);err==nil{
        t.Fatal("known_hosts accepted missing pinned public key")
    }
}
