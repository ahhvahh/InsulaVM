package manifest

import (
	"strings"
	"testing"
)

const good = `repository:
  name: demo
action: ci
workspace: /app/repo-workspace
setup:
  - echo setup
pipelines:
  - name: tests
    jobs:
      - name: run-tests
        commands:
          - go test ./...
        allow_failure: false
`

func TestDecode(t *testing.T) {
	m, err := Decode(strings.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	if m.GroupName() != "ci" || len(m.Pipelines[0].Jobs) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestUnknownKeysAndRepeatedJobNames(t *testing.T) {
	if _, err := Decode(strings.NewReader(good + "unexpected: true\n")); err == nil {
		t.Fatal("unknown fields must fail")
	}
	duplicate := strings.Replace(good, "name: run-tests", "name: setup", 1)
	if _, err := Decode(strings.NewReader(duplicate)); err == nil {
		t.Fatal("job name must not clobber setup.log")
	}
}

func TestRejectsUnsafePathAndBadCommands(t *testing.T) {
	bad := strings.Replace(good, "workspace: /app/repo-workspace", "workspace: ../escape", 1)
	if _, err := Decode(strings.NewReader(bad)); err == nil {
		t.Fatal("expected workspace error")
	}
	bad = strings.Replace(good, "- go test ./...", "- '  '", 1)
	if _, err := Decode(strings.NewReader(bad)); err == nil {
		t.Fatal("expected empty command error")
	}
}

func TestImportsBeforeJobsSchema(t *testing.T) {
 src := "repository:\n  name: demo\naction: import\nworkspace: /tmp/work\nimports:\n  - source: ./project\n    destination: /tmp/work\n"
 m,err:=Decode(strings.NewReader(src))
 if err!=nil{t.Fatal(err)}
 if len(m.Imports)!=1||m.Imports[0].Destination!="/tmp/work"{t.Fatalf("imports: %+v",m.Imports)}
 for _,bad:=range []string{
  strings.Replace(src,"/tmp/work\n","../relative\n",1),
  strings.Replace(src,"destination: /tmp/work","destination: /",1),
  strings.Replace(src,"source: ./project","source: ''",1),
 }{
  if _,err:=Decode(strings.NewReader(bad));err==nil{t.Fatalf("accepted invalid import YAML: %s",bad)}
 }
}
