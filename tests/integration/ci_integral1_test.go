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

        On map[string]struct {
            Branches []string `yaml:"branches"`
            Types []string `yaml:"types"`
        } `yaml:"on"`
        Concurrency struct {
            Group string `yaml:"group"`
            Cancel bool `yaml:"cancel-in-progress"`
        } `yaml:"concurrency"`
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

    if len(workflow.On) != 1 {
        t.Fatalf("only pull_request may trigger the real Alpine E2E: %+v", workflow.On)
    }
    pr, ok := workflow.On["pull_request"]
    if !ok {
        t.Fatalf("pull_request trigger required: %+v", workflow.On)
    }
    expectedBranches := []string{"develop", "homolog", "main"}
    if len(pr.Branches) != len(expectedBranches) {
        t.Errorf("exact PR target set required: want=%v got=%v", expectedBranches, pr.Branches)
    }
    for _, target := range expectedBranches {
        if !integral1Has(pr.Branches, target) {
            t.Errorf("missing mandatory PR target %s", target)
        }
    }
    expectedEvents := []string{"opened", "synchronize", "reopened"}
    if len(pr.Types) != len(expectedEvents) {
        t.Errorf("exact PR event set required: want=%v got=%v", expectedEvents, pr.Types)
    }
    for _, event := range expectedEvents {
        if !integral1Has(pr.Types, event) {
            t.Errorf("missing mandatory pull_request trigger %s", event)
        }
    }
    if !workflow.Concurrency.Cancel || !strings.Contains(workflow.Concurrency.Group,"github.event.pull_request.number") {
        t.Errorf("older runs for the same PR must be cancelled to reduce cost: %+v",workflow.Concurrency)
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

    if strings.Contains(string(data),"continue-on-error: true") || strings.Contains(string(data),"paths-ignore:") || strings.Contains(string(data),"paths:") {
        t.Error("required check must not hide errors or filter PR changed paths")
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
