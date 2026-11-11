package profile

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestGuestDestinationValidation(t *testing.T) {
 for _, tc := range []struct {
  destination string
  valid bool
 }{
  {"", true},
  {"/tmp/guest/work", true},
  {"/tmp/guest/../workspace", true},
  {"relative/work", false},
  {"/", false},
  {"/tmp/..", false},
  {"/tmp/work\nmore", false},
  {"/tmp/work\rmore", false},
  {"/tmp/work\x00more", false},
 } {
  err := ValidateGuestDestination(tc.destination)
  if (err == nil) != tc.valid {
   t.Errorf("destination %q: err=%v valid=%v", tc.destination, err, tc.valid)
  }
 }
}

func TestProfileDestinationPersistedAndIndependent(t *testing.T) {
 root := t.TempDir()
 source := filepath.Join(t.TempDir(), "base.qcow2")
 qcow2(t, source)
 for _, tc := range []struct{name, destination string}{
  {"one", "/srv/workspace/one"},
  {"two", "/srv/workspace/two"},
 } {
  p := Profile{Name:tc.name, MemoryMiB:512, VCPUs:1, DiskGB:2,
   Guest:Guest{Destination:tc.destination}}
  if err := Register(root,p,source);err != nil {t.Fatal(err)}
  got,err:=Read(root,tc.name)
  if err!=nil {t.Fatal(err)}
  if got.GuestDestination("")!=tc.destination {
   t.Fatalf("profile %s destination %q, want %q",tc.name,got.GuestDestination(""),tc.destination)
  }
  data,err:=os.ReadFile(filepath.Join(root,tc.name,"profile.yaml"))
  if err!=nil {t.Fatal(err)}
  if !strings.Contains(string(data),"destination: "+tc.destination){
   t.Fatalf("guest destination missing from saved profile %s: %s",tc.name,data)
  }
 }
}

func TestLegacyProfileDestinationFallback(t *testing.T) {
 root:=t.TempDir()
 dir:=filepath.Join(root,"legacy")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 meta:="name: legacy\nmemory_mib: 512\nvcpus: 1\ndisk_gb: 2\nversion: v001\nimage: versions/v001/base.qcow2\n"
 if err:=os.WriteFile(filepath.Join(dir,"profile.yaml"),[]byte(meta),0600);err!=nil{t.Fatal(err)}
 p,err:=Read(root,"legacy")
 if err!=nil{t.Fatal(err)}
 if got:=p.GuestDestination("/srv/legacy");got!="/srv/legacy"{t.Fatalf("legacy fallback %q",got)}
 if got:=p.GuestDestination("");got!=DefaultGuestDestination{t.Fatalf("default fallback %q",got)}
}
