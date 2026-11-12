package main

import (
 "bytes"
 "strings"
 "testing"

 "github.com/ahhvahh/InsulaVM/insulavm"
 "github.com/ahhvahh/InsulaVM/pkg/registry"
)

func TestVMListPrintsNameProfileUUIDInRequestedOrder(t *testing.T){
 const id="019a5d11-1234-7123-8123-123456789abc"
 name:="insula-alpine-virt-"+id
 entries:=[]insulavm.VMLiveInfo{{
  Record:registry.Entry{ID:id,Name:name,Profile:"alpine-virt",Status:"running",IP:""},
  LibvirtState:"absent",
 }}
 var buf bytes.Buffer
 writeVMList(&buf,entries)
 lines:=strings.Split(strings.TrimSpace(buf.String()),"\n")
 if len(lines)!=2{t.Fatalf("expected header and one row: %q",buf.String())}
 header:=strings.Fields(lines[0])
 if len(header)<3||header[0]!="Nome"||header[1]!="Perfil"||header[2]!="UUID"{
  t.Fatalf("incorrect column order: %q",lines[0])
 }
 row:=strings.Fields(lines[1])
 if len(row)<5||row[0]!=name||row[1]!="alpine-virt"||row[2]!=id||row[3]!="running"||row[4]!="absent"{
  t.Fatalf("wrong list row: %q",lines[1])
 }
}
