package workflow

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Workflow represents a complete workflow definition
type Workflow struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Version     string         `yaml:"version"`
	Steps       []Step         `yaml:"steps"`
	Agents      []AgentBinding `yaml:"agents"`
}

// Step represents a single step in a workflow
type Step struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Agent       string            `yaml:"agent"`
	Input       map[string]any    `yaml:"input"`
	DependsOn   []string          `yaml:"depends_on"`
	Timeout     string            `yaml:"timeout"`
}

// AgentBinding maps an agent ID to its capabilities
type AgentBinding struct {
	ID           string   `yaml:"id"`
	Type         string   `yaml:"type"`
	Capabilities []string `yaml:"capabilities"`
}

// Loader loads workflow definitions from YAML files
type Loader struct{}

// NewLoader creates a new workflow loader
func NewLoader() *Loader {
	return &Loader{}
}

// Load reads a workflow from a YAML file
func (l *Loader) Load(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow file: %w", err)
	}

	var workflow Workflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		return nil, fmt.Errorf("failed to parse workflow YAML: %w", err)
	}

	return &workflow, nil
}

// Validate checks if the workflow is valid
func (l *Loader) Validate(w *Workflow) error {
	if w.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}

	stepIDs := make(map[string]bool)
	for _, step := range w.Steps {
		if stepIDs[step.ID] {
			return fmt.Errorf("duplicate step ID: %s", step.ID)
		}
		stepIDs[step.ID] = true

		// Check dependencies exist
		for _, dep := range step.DependsOn {
			if !stepIDs[dep] {
				return fmt.Errorf("step %s depends on unknown step %s", step.ID, dep)
			}
		}
	}

	return nil
}
