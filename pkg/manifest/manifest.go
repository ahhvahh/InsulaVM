// Package manifest decodes and validates the currently supported insula.yaml
// execution subset. VM creation and cloning are separate future workflows.
package manifest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/ahhvahh/InsulaVM/pkg/storage"
	"github.com/ahhvahh/InsulaVM/pkg/identity"
	"gopkg.in/yaml.v3"
)

type Repository struct {
	Name string `yaml:"name"`
}

type Job struct {
	Name         string   `yaml:"name"`
	Commands     []string `yaml:"commands"`
	AllowFailure bool     `yaml:"allow_failure"`
}

type Pipeline struct {
	Name string `yaml:"name"`
	Jobs []Job  `yaml:"jobs"`
}

// ImportSpec copies one host file or directory into the VM before setup/jobs.
// Source may be absolute or relative to the host process working directory.
type ImportSpec struct {
 Source string `yaml:"source"`
}

// SSHOptions chooses a provisioned, non-root account for this Action.
// Omit ssh entirely to use the host-wide ssh.user (default: insula).
// Keys never belong in the Action manifest.
type SSHOptions struct {
 User string `yaml:"user"`
}

type Manifest struct {
	Profile    string     `yaml:"profile"`
	SSH        SSHOptions `yaml:"ssh"`
	Repository Repository `yaml:"repository"`
	Action     string     `yaml:"action"`
	Workspace  string     `yaml:"workspace"`
	Setup      []string   `yaml:"setup"`
	Imports    []ImportSpec `yaml:"imports"`
	Pipelines  []Pipeline `yaml:"pipelines"`
}

func ReadFile(filename string) (*Manifest, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Decode(file)
}

// Decode rejects unknown YAML fields and extra YAML documents.
func Decode(reader io.Reader) (*Manifest, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var value Manifest
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode insula.yaml: %w", err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("only one YAML document is supported")
		}
		return nil, fmt.Errorf("decode trailing YAML: %w", err)
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &value, nil
}

// GroupName selects the action when present, otherwise the single pipeline.
func (m *Manifest) GroupName() string {
	if m.Action != "" {
		return m.Action
	}
	if len(m.Pipelines) == 1 {
		return m.Pipelines[0].Name
	}
	return ""
}

func (m *Manifest) Validate() error {
	if m.Profile != "" {
		if err := storage.ValidateName(m.Profile); err != nil {
			return fmt.Errorf("profile: %w", err)
		}
	}
	if m.SSH.User != "" && !identity.ValidUser(m.SSH.User) {
        return fmt.Errorf("ssh.user %q must be a valid, non-root Linux username", m.SSH.User)
    }
    if err := storage.ValidateName(m.Repository.Name); err != nil {
		return fmt.Errorf("repository: %w", err)
	}
	if m.Action != "" {
		if err := storage.ValidateName(m.Action); err != nil {
			return fmt.Errorf("action: %w", err)
		}
	}
	if m.Workspace != "" && (!path.IsAbs(m.Workspace) ||
		strings.ContainsAny(m.Workspace, string([]byte{13, 10, 0}))) {
		return errors.New("workspace must be an absolute Linux path")
	}
	if len(m.Pipelines) == 0 && len(m.Setup) == 0 && len(m.Imports) == 0 {
		return errors.New("at least one setup command or pipeline is required")
	}
	if m.GroupName() == "" {
		return errors.New("action is required when multiple or zero pipelines are specified")
	}
	for i, item := range m.Imports {
		if strings.TrimSpace(item.Source)=="" || strings.ContainsAny(item.Source,string([]byte{0,10,13})){
			return fmt.Errorf("imports[%d].source must be a local file or directory path",i)
		}
	}
	used := make(map[string]bool)
	if len(m.Setup) != 0 {
		used["setup"] = true
	}
	for _, command := range m.Setup {
		if err := checkCommand(command); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
	}
	for _, pipeline := range m.Pipelines {
		if err := storage.ValidateName(pipeline.Name); err != nil {
			return fmt.Errorf("pipeline: %w", err)
		}
		if len(pipeline.Jobs) == 0 {
			return fmt.Errorf("pipeline %s has no jobs", pipeline.Name)
		}
		for _, job := range pipeline.Jobs {
			if err := storage.ValidateName(job.Name); err != nil {
				return fmt.Errorf("job: %w", err)
			}
			if used[job.Name] {
				return fmt.Errorf("duplicate job log name %q", job.Name)
			}
			used[job.Name] = true
			if len(job.Commands) == 0 {
				return fmt.Errorf("job %s has no commands", job.Name)
			}
			for _, command := range job.Commands {
				if err := checkCommand(command); err != nil {
					return fmt.Errorf("job %s: %w", job.Name, err)
				}
			}
		}
	}
	return nil
}

func checkCommand(cmd string) error {
	if strings.TrimSpace(cmd) == "" || strings.ContainsRune(cmd, 0) {
		return errors.New("command is empty or contains a NUL byte")
	}
	return nil
}
