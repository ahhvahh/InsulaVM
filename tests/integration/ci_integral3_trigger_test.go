package integration

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "gopkg.in/yaml.v3"
)

// TestIntegral3PRTriggerContract ensures expensive ISO installation cannot
// accidentally be started by pushes, edits, schedules, or manual dispatch.
func TestIntegral3PRTriggerContract(t *testing.T) {
 path := filepath.Join("..", "..", ".github", "workflows", "integral-3-iso-workspace.yml")
 content, err := os.ReadFile(path)
 if err != nil { t.Fatal(err) }
 var workflow struct {
  On map[string]struct {
   Branches []string `yaml:"branches"`
   Types []string `yaml:"types"`
  } `yaml:"on"`
  Concurrency struct {
   Group string `yaml:"group"`
   Cancel bool `yaml:"cancel-in-progress"`
  } `yaml:"concurrency"`
  Permissions map[string]string `yaml:"permissions"`
  Jobs map[string]yaml.Node `yaml:"jobs"`
 }
 // yaml.v3 reports duplicate mappings; a previous workflow accidentally
 // contained two copies of its expensive test job.
 if err := yaml.Unmarshal(content, &workflow); err != nil {
  t.Fatalf("invalid or duplicated Actions YAML: %v", err)
 }
 if len(workflow.On) != 1 {
  t.Fatalf("only pull_request trigger is permitted: %v", workflow.On)
 }
 trigger, ok := workflow.On["pull_request"]
 if !ok { t.Fatalf("pull_request trigger missing: %v", workflow.On) }
 verifySet := func(label string, got, want []string) {
  t.Helper()
  if len(got) != len(want) { t.Fatalf("%s: got=%v want=%v",label,got,want) }
  gotSet := make(map[string]int, len(got))
  for _, value := range got { gotSet[value]++ }
  for _, value := range want {
   if gotSet[value] != 1 { t.Fatalf("%s: got=%v want=%v",label,got,want) }
  }
 }
 verifySet("allowed PR bases",trigger.Branches,[]string{"develop","homolog","main"})
 verifySet("allowed PR events",trigger.Types,[]string{"opened","synchronize","reopened"})
 if !workflow.Concurrency.Cancel ||
  !strings.Contains(workflow.Concurrency.Group,"github.event.pull_request.number") {
  t.Fatalf("outdated PR runs must be cancelled: %+v",workflow.Concurrency)
 }
 if len(workflow.Permissions)!=1 || workflow.Permissions["contents"]!="read" {
  t.Fatalf("workflow token must be read-only: %+v", workflow.Permissions)
 }
 if len(workflow.Jobs)!=1 {
  t.Fatalf("expected only the expensive integral 3 job, got %v",workflow.Jobs)
 }
 if _, ok := workflow.Jobs["integral-3-iso-workspace-persistence"]; !ok {
  t.Fatalf("stable required-check job missing: %v",workflow.Jobs)
 }
 for _, required := range []string{
  "go vet ./...", "scripts/tests/integral-3-iso-persistence.sh",
 } {
  if !strings.Contains(string(content),required) {
   t.Errorf("missing preflight or integration test %q",required)
  }
 }
}
