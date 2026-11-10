package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestIntegral1PRWorkflowContract guards the CI trigger and acceptance
// against accidental removal. It runs in the ordinary Go unit suite and
// requires no KVM, SSH or Libvirt.
func TestIntegral1PRWorkflowContract(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "integral-1-alpine-iso.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			PullRequest struct {
				Branches []string `yaml:"branches"`
				Types    []string `yaml:"types"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs map[string]struct {
			Name   string `yaml:"name"`
			RunsOn string `yaml:"runs-on"`
			Steps []struct {
				Name string `yaml:"name"`
				Uses string `yaml:"uses"`
				Run  string `yaml:"run"`
				If   string `yaml:"if"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("invalid workflow YAML: %v", err)
	}
	for _, target := range []string{"develop", "promolog", "main"} {
		if !integral1Has(workflow.On.PullRequest.Branches, target) {
			t.Errorf("missing mandatory PR target %s", target)
		}
	}
	for _, event := range []string{"opened", "synchronize", "reopened", "ready_for_review", "edited"} {
		if !integral1Has(workflow.On.PullRequest.Types, event) {
			t.Errorf("missing mandatory pull_request trigger %s", event)
		}
	}
	if !strings.Contains(string(data), "on:\n  pull_request:") ||
		strings.Contains(string(data), "pull_request_target:") {
		t.Error("workflow must run as pull_request, not privileged pull_request_target")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Errorf("workflow permissions must be read-only, got: %+v", workflow.Permissions)
	}
	job, ok := workflow.Jobs["integral-1-alpine-iso"]
	if !ok {
		t.Fatal("mandatory named status job integral-1-alpine-iso is missing")
	}
	if job.RunsOn != "ubuntu-24.04" || job.Name != "integral-1-alpine-iso" {
		t.Errorf("required check must use ephemeral GitHub hosted runner and stable name: %+v", job)
	}
	var hasKVMCheck, hasISOTest bool
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "/dev/kvm") && strings.Contains(step.Run, "virsh -c qemu:///system") {
			hasKVMCheck = true
		}
		if strings.Contains(step.Run, "bash scripts/tests/alpine-iso.sh") {
			hasISOTest = true
			if step.If != "" {
				t.Errorf("mandatory ISO step must not be conditional: %q", step.If)
			}
		}
	}
	if !hasKVMCheck || !hasISOTest {
		t.Errorf("mandatory KVM preflight and Alpine end-to-end command required (preflight=%v test=%v)", hasKVMCheck, hasISOTest)
	}
	if strings.Contains(string(data), "continue-on-error: true") || strings.Contains(string(data), "paths-ignore:") || strings.Contains(string(data), "cancel-in-progress: true") {
		t.Error("required check must not hide errors, filter commit paths, or cancel prior commit runs")
	}
}

func integral1Has(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
