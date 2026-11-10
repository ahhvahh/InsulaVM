package integration

import (
    "os"
    "path/filepath"
    "testing"

    "gopkg.in/yaml.v3"
)

// TestUnitWorkflowPushAllBranches verifies the required all-branch push policy.
// Tags, PR events, path restrictions and additional triggers remain excluded.
func TestUnitWorkflowPushAllBranches(t *testing.T) {
    path := filepath.Join("..", "..", ".github", "workflows", "go-tests.yml")
    src, err := os.ReadFile(path)
    if err != nil {
        t.Fatal(err)
    }
    var cfg struct {
        On map[string]yaml.Node `yaml:"on"`
        Permissions map[string]string `yaml:"permissions"`
        Jobs map[string]yaml.Node `yaml:"jobs"`
    }
    if err := yaml.Unmarshal(src, &cfg); err != nil {
        t.Fatalf("invalid or duplicate workflow YAML: %v", err)
    }
    if len(cfg.On) != 1 {
        t.Fatalf("only push trigger is allowed: %+v", cfg.On)
    }
    push, ok := cfg.On["push"]
    if !ok {
        t.Fatalf("only push trigger is allowed: %+v", cfg.On)
    }
    // Require only the all-branches pattern; all other keys restrict events
    // or silently add executions outside the intended push policy.
    var filters map[string]yaml.Node
    if err := push.Decode(&filters); err != nil {
        t.Fatal(err)
    }
    if len(filters) != 1 {
        t.Fatalf("unit tests must run on every branch push without other filters: %+v", filters)
    }
    branchesNode, ok := filters["branches"]
    if !ok {
        t.Fatalf("missing all-branches push filter: %+v", filters)
    }
    var branches []string
    if err := branchesNode.Decode(&branches); err != nil {
        t.Fatal(err)
    }
    if len(branches) != 1 || branches[0] != "**" {
        t.Fatalf("unit tests must run on pushes to every branch, excluding tags: %v", branches)
    }
    if len(cfg.Permissions) != 1 || cfg.Permissions["contents"] != "read" {
        t.Fatalf("read-only permission required: %+v", cfg.Permissions)
    }
    if len(cfg.Jobs) != 1 {
        t.Fatalf("expected one unit job: %+v", cfg.Jobs)
    }
    if _, ok := cfg.Jobs["unit"]; !ok {
        t.Fatalf("missing unit job")
    }
}
