package main

import (
 "path/filepath"
 "strings"
 "testing"
)

func TestResolveFreezeTarget(t *testing.T){
 cases:=[]struct{
  name,flagOutput string
  positional []string
  export bool
  expectedName,expectedOutput string
  wantError bool
 }{
  {name:"legacy-profile",export:true,expectedName:"legacy-profile"},
  {positional:[]string{"./imagens/deb13-abioma-tests-final.iso"},export:true,expectedName:"deb13-abioma-tests-final",expectedOutput:"imagens/deb13-abioma-tests-final.iso"},
  {name:"profile-separado",positional:[]string{"/tmp/minha-vm.iso"},export:true,expectedName:"profile-separado",expectedOutput:"/tmp/minha-vm.iso"},
  {flagOutput:"./saida/meu-ambiente.iso",export:true,expectedName:"meu-ambiente",expectedOutput:"saida/meu-ambiente.iso"},
  {name:"somente-disco",export:false,expectedName:"somente-disco"},
  {positional:[]string{"teste.qcow2"},export:true,wantError:true},
  {positional:[]string{"vm.iso"},export:false,wantError:true},
  {flagOutput:"a.iso",positional:[]string{"b.iso"},export:true,wantError:true},
  {positional:[]string{"a.iso","b.iso"},export:true,wantError:true},
  {export:true,wantError:true},
 }
 for i,tc:=range cases{
  gotName,gotOutput,err:=resolveFreezeTarget(tc.name,tc.flagOutput,tc.positional,tc.export)
  if (err!=nil)!=tc.wantError{t.Fatalf("case %d unexpected error: %v",i,err)}
  if tc.wantError{continue}
  if gotName!=tc.expectedName{t.Errorf("case %d name=%q expected=%q",i,gotName,tc.expectedName)}
  if tc.expectedOutput==""{
   if gotOutput!=""{t.Errorf("case %d output=%q expected empty",i,gotOutput)}
   continue
  }
  suffix:=filepath.FromSlash(tc.expectedOutput)
  if !strings.HasSuffix(gotOutput,suffix)||!filepath.IsAbs(gotOutput){
   t.Errorf("case %d output=%q expected absolute ending in %q",i,gotOutput,suffix)
  }
 }
}
