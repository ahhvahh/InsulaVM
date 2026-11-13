package profile

import "testing"

func TestProfileBootModeValidation(t *testing.T) {
 tests := []struct {
  mode string
  valid bool
 }{
  {"", true},
  {"cloud-init", true},
  {"direct", true},
  {"none", false},
  {"direct\nunsafe", false},
 }
 for _, tc := range tests {
  t.Run(tc.mode, func(t *testing.T) {
   p := Profile{
    Name: "alpine-prepared", Version: "v001",
    Image: "versions/v001/base.qcow2",
    MemoryMiB: 500, VCPUs: 1, DiskGB: 8,
    BootMode: tc.mode,
   }
   err := p.Validate()
   if (err == nil) != tc.valid {
    t.Fatalf("mode=%q valid=%v error=%v", tc.mode, tc.valid, err)
   }
  })
 }
}
