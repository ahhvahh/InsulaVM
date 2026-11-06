package vm

import (
 "strings"
 "testing"
)

func TestParseIPv4Lease(t *testing.T){
 table:="Name       MAC address          Protocol Address         Hostname\n"+
  "vnet0      52:54:00:12:34:56  ipv4     192.168.122.27/24  test\n"
 if got:=parseIPv4(table);got!="192.168.122.27"{t.Fatalf("got %q",got)}
 if got:=parseIPv4("no IPv4 lease");got!=""{t.Fatalf("unexpected IP %q",got)}
}

func TestDomainXMLAttributesAreEscaped(t *testing.T){
 result:=domainXML("vm",512,1,"/tmp/guest's.qcow2","default","52:54:00:aa:bb:cc")
 if !strings.Contains(result,"guest&#39;s.qcow2") && !strings.Contains(result,"guest&apos;s.qcow2"){t.Fatalf("unescaped attribute: %s",result)}
 if !strings.Contains(result,"<memory unit='MiB'>512</memory>"){t.Fatal("RAM not present")}
 if !strings.Contains(result,"<vcpu>1</vcpu>"){t.Fatal("vCPU not present")}
}
