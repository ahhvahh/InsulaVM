package integration

import (
    "os"
    "path/filepath"
    "testing"

    "gopkg.in/yaml.v3"
)

// TestUnitWorkflowPushDevelopOnly protects the budget and trigger boundary:
// the complete unit suite only runs after a commit is pushed to develop.
func TestUnitWorkflowPushDevelopOnly(t *testing.T) {
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
    var filters struct {
        Branches []string `yaml:"branches"`
        Tags []string `yaml:"tags"`
        Paths []string `yaml:"paths"`
        PathsIgnore []string `yaml:"paths-ignore"`
    }
    if err := push.Decode(&filters); err != nil {
        t.Fatal(err)
    }
    if len(filters.Branches) != 1 || filters.Branches[0] != "develop" ||
        len(filters.Tags) != 0 || len(filters.Paths) != 0 || len(filters.PathsIgnore) != 0 {
        t.Fatalf("unit tests must run on every develop push only: %+v", filters)
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
