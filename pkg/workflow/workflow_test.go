package workflow

import (
	"os"
	"testing"
)

func TestLoader_Load(t *testing.T) {
	// Create temp workflow file
	content := `
name: test-workflow
description: Test workflow
version: "1.0"
agents:
  - id: agent-1
    type: planner
    capabilities:
      - planner
steps:
  - id: step-1
    name: Test Step
    type: plan
    agent: agent-1
    input:
      key: value
`
	tmpfile, err := os.CreateTemp("", "workflow-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	loader := NewLoader()
	wf, err := loader.Load(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}

	if wf.Name != "test-workflow" {
		t.Errorf("Expected name 'test-workflow', got '%s'", wf.Name)
	}

	if len(wf.Steps) != 1 {
		t.Errorf("Expected 1 step, got %d", len(wf.Steps))
	}

	if len(wf.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(wf.Agents))
	}
}

func TestLoader_Validate(t *testing.T) {
	loader := NewLoader()

	tests := []struct {
		name    string
		workflow *Workflow
		wantErr bool
	}{
		{
			name: "valid workflow",
			workflow: &Workflow{
				Name: "test",
				Steps: []Step{{ID: "step-1", Name: "Step 1", Type: "plan", Agent: "a1"}},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			workflow: &Workflow{
				Name: "",
				Steps: []Step{{ID: "step-1", Name: "Step 1", Type: "plan", Agent: "a1"}},
			},
			wantErr: true,
		},
		{
			name: "no steps",
			workflow: &Workflow{
				Name:  "test",
				Steps: []Step{},
			},
			wantErr: true,
		},
		{
			name: "duplicate step ID",
			workflow: &Workflow{
				Name: "test",
				Steps: []Step{
					{ID: "step-1", Name: "Step 1", Type: "plan", Agent: "a1"},
					{ID: "step-1", Name: "Step 2", Type: "plan", Agent: "a1"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid dependency",
			workflow: &Workflow{
				Name: "test",
				Steps: []Step{
					{ID: "step-2", Name: "Step 2", Type: "plan", Agent: "a1", DependsOn: []string{"step-1"}},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := loader.Validate(tt.workflow)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
