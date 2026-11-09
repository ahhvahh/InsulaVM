package vm

import (
 "strings"
 "testing"
)

func TestLiveISODomainXML(t *testing.T){
 raw:=liveISODomainXML("00000000-0000-0000-0000-000000000001","test",1024,1,"/tmp/a.iso","/tmp/seed.iso","/tmp/scratch.qcow2","default","52:54:00:00:00:00")
 for _,fragment:=range []string{"<boot dev='cdrom'/>","/tmp/a.iso","/tmp/seed.iso","/tmp/scratch.qcow2"}{
  if !strings.Contains(raw,fragment){t.Fatalf("missing %q from domain XML",fragment)}
 }
 if strings.Count(raw,"device='cdrom'")!=2{t.Fatalf("expected 2 read-only CDROMs: %s",raw)}
}

func TestLiveISODomainHasGracefulShutdownDevices(t *testing.T){
 spec:=liveISODomainXML("01a11111-1111-7111-8111-111111111111","insula-live",1024,1,
  "/tmp/live.iso","/tmp/seed.iso","/tmp/scratch.qcow2","default","52:54:00:aa:bb:cc")
 for _,want:=range []string{"<features><acpi/></features>","org.qemu.guest_agent.0"}{
  if !strings.Contains(spec,want){t.Fatalf("missing %s in XML: %s",want,spec)}
 }
}
