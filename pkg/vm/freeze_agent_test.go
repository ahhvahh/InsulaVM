package vm

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// Tests exercise the public freeze shutdown engine using a fake Libvirt
// command. A real guest boot remains a separate KVM integration requirement.
func TestShutdownInstallerForFreezeAgentAndACPIFallback(t *testing.T) {
 tests:=[]struct{
  name string
  agentAvailable bool
  acpiWorks bool
  wantSuccess bool
  wantAgent bool
  wantACPI bool
 }{
  {"QGA poweroff",true,false,true,true,false},
  {"QGA unavailable, ACPI works",false,true,true,true,true},
  {"QGA and ACPI unavailable",false,false,false,true,true},
 }
 for _,tc:=range tests {
  t.Run(tc.name,func(t *testing.T){
   bin:=t.TempDir()
   state:=filepath.Join(t.TempDir(),"domain.state")
   events:=filepath.Join(t.TempDir(),"events")
   if err:=os.WriteFile(state,[]byte("running"),0600);err!=nil{t.Fatal(err)}
   const uuid="019a5d11-1234-7123-8123-123456789abc"
   script:=`#!/bin/sh
set -eu
case "$3" in
 list) printf '%s\n' "$MOCK_UUID" ;;
 domstate) cat "$MOCK_STATE" ;;
 shutdown)
  case "$6" in
   agent)
    echo agent >> "$MOCK_EVENTS"
    if [ "$MOCK_AGENT" = 1 ]; then echo 'shut off' > "$MOCK_STATE"; exit 0; fi
    echo 'QEMU guest agent not connected' >&2; exit 22 ;;
   acpi)
    echo acpi >> "$MOCK_EVENTS"
    if [ "$MOCK_ACPI" = 1 ]; then echo 'shut off' > "$MOCK_STATE"; fi ;;
   *) echo "unexpected shutdown mode $6" >&2; exit 23 ;;
  esac ;;
 destroy) echo destroy >> "$MOCK_EVENTS"; exit 24 ;;
 *) echo "unexpected virsh command $3" >&2; exit 25 ;;
esac
`
   if err:=os.WriteFile(filepath.Join(bin,"virsh"),[]byte(script),0700);err!=nil{t.Fatal(err)}
   t.Setenv("PATH",bin+string(os.PathListSeparator)+os.Getenv("PATH"))
   t.Setenv("MOCK_UUID",uuid)
   t.Setenv("MOCK_STATE",state)
   t.Setenv("MOCK_EVENTS",events)
   if tc.agentAvailable{t.Setenv("MOCK_AGENT","1")}else{t.Setenv("MOCK_AGENT","0")}
   if tc.acpiWorks{t.Setenv("MOCK_ACPI","1")}else{t.Setenv("MOCK_ACPI","0")}
   var messages []string
   forced,err:=ShutdownInstallerForFreeze(context.Background(),"qemu:///system",uuid,80*time.Millisecond,false,
    func(msg string){messages=append(messages,msg)})
   if (err==nil)!=tc.wantSuccess||forced{
    t.Fatalf("unexpected graceful stop: forced=%v err=%v events=%v",forced,err,messages)
   }
   trace,err:=os.ReadFile(events)
   if err!=nil{t.Fatal(err)}
   steps:=string(trace)
   if strings.Contains(steps,"agent")!=tc.wantAgent||strings.Contains(steps,"acpi")!=tc.wantACPI{
    t.Fatalf("unexpected methods: %s",steps)
   }
   if strings.Contains(steps,"destroy"){t.Fatalf("non-consensual destroy: %s",steps)}
   if tc.wantSuccess {
    actual,err:=os.ReadFile(state)
    if err!=nil||strings.TrimSpace(string(actual))!="shut off"{
     t.Fatalf("guest never powered off, state=%s error=%v",actual,err)
    }
   }
  })
 }
}

func TestFreezeDiskSafeState(t *testing.T){
 cases:=[]struct{state string; safe bool}{
  {"absent",true},{"shut off",true},{"running",false},
  {"blocked",false},{"paused",false},{"unknown",false},{"in shutdown",false},
 }
 for _,tc:=range cases{
  t.Run(tc.state,func(t *testing.T){
   if freezeDiskSafeState(tc.state)!=tc.safe{
    t.Fatalf("state %s unexpectedly classified",tc.state)
   }
  })
 }
}
