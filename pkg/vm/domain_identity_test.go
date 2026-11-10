package vm

import (
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/pkg/identity"
)

func TestDomainNameIsUniqueEvenWhenUUIDv7TimestampPrefixMatches(t *testing.T) {
 ids:=[]string{
  "019a5d11-1234-7123-8123-123456789abc",
  "019a5d11-1234-7123-8123-123456789abd",
  "019a5d11-1234-7123-8123-123456789abe",
 }
 seen:=make(map[string]bool)
 for _,id:=range ids{
  name:=vmDomainName("shared-frozen-profile",id)
  if !strings.Contains(name,id){t.Fatalf("domain %q does not contain full UUID %q",name,id)}
  if seen[name]{t.Fatalf("colliding names for same-profile VMs: %q",name)}
  seen[name]=true
 }
 if ids[0][:8]!=ids[1][:8] {t.Fatal("fixture no longer models shared timestamp prefix")}
}

func TestDomainNameIsUniqueForParallelLikeUUIDv7Generation(t *testing.T) {
 names:=make(map[string]bool)
 for i:=0;i<500;i++{
  id,err:=identity.UUIDv7()
  if err!=nil{t.Fatal(err)}
  name:=vmDomainName("debian-test",id)
  if names[name]{t.Fatalf("generated duplicate Libvirt name: %s",name)}
  names[name]=true
 }
}
