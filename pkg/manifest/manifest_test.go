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
