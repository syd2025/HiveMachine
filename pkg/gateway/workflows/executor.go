// Package workflows implements ComfyUI workflow execution for HiveMachine.
package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Workflow represents a ComfyUI workflow graph.
type Workflow struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Nodes     map[string]Node       `json:"nodes"`
	Edges     []Edge                `json:"edges"`
	ClassType string                `json:"class_type"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Node represents a single node in a workflow graph.
type Node struct {
	ID        string                 `json:"id"`
	ClassType string                 `json:"class_type"`
	Inputs    map[string]interface{}  `json:"inputs"`
	Outputs   []interface{}           `json:"outputs"`
	Widgets    map[string]interface{} `json:"widgets,omitempty"`
}

// Edge represents a connection between two nodes.
type Edge struct {
	Source       string `json:"source"`
	SourceOutput int    `json:"source_output"`
	Target       string `json:"target"`
	TargetInput  string `json:"target_input"`
}

// JobState represents the state of a workflow execution.
type JobState string

const (
	JobStatePending   JobState = "pending"
	JobStateRunning   JobState = "running"
	JobStateCompleted JobState = "completed"
	JobStateFailed    JobState = "failed"
	JobStateCancelled JobState = "cancelled"
)

// Execution represents a running or completed workflow execution.
type Execution struct {
	ID        string
	Workflow  *Workflow
	State     JobState
	CreatedAt time.Time
	StartedAt *time.Time
	FinishedAt *time.Time
	Result    *ExecutionResult
	Error     string
	Progress  float64
	mu        sync.RWMutex
}

// ExecutionResult holds the output of a completed workflow.
type ExecutionResult struct {
	Images   []string            `json:"images,omitempty"`
	Videos   []string            `json:"videos,omitempty"`
	Audio    []string            `json:"audio,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// NodeExecutor executes a single node.
type NodeExecutor interface {
	Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error)
	SupportedTypes() []string
}

// Executor manages workflow execution.
type Executor struct {
	executors map[string]NodeExecutor
	jobs      map[string]*Execution
	jobsMu    sync.RWMutex
	maxJobs   int
}

// NewExecutor creates a new workflow executor.
func NewExecutor(maxJobs int) *Executor {
	if maxJobs <= 0 {
		maxJobs = 10
	}
	return &Executor{
		executors: make(map[string]NodeExecutor),
		jobs:      make(map[string]*Execution),
		maxJobs:   maxJobs,
	}
}

// RegisterExecutor registers a node executor for specific class types.
func (e *Executor) RegisterExecutor(executor NodeExecutor) {
	for _, t := range executor.SupportedTypes() {
		e.executors[t] = executor
	}
}

// ParseWorkflow parses a JSON workflow definition.
func ParseWorkflow(data []byte) (*Workflow, error) {
	var wf Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parsing workflow: %w", err)
	}
	if len(wf.Nodes) == 0 {
		return nil, fmt.Errorf("workflow has no nodes")
	}
	return &wf, nil
}

// Submit submits a workflow for execution.
func (e *Executor) Submit(ctx context.Context, workflow *Workflow) (*Execution, error) {
	e.jobsMu.Lock()
	defer e.jobsMu.Unlock()

	if len(e.jobs) >= e.maxJobs {
		return nil, fmt.Errorf("executor at capacity: %d/%d jobs", len(e.jobs), e.maxJobs)
	}

	exec := &Execution{
		ID:        fmt.Sprintf("wf-%d", time.Now().UnixNano()),
		Workflow:  workflow,
		State:     JobStatePending,
		CreatedAt: time.Now(),
	}

	e.jobs[exec.ID] = exec

	// Run execution in background
	go e.run(context.Background(), exec)

	return exec, nil
}

// run executes the workflow.
func (e *Executor) run(ctx context.Context, exec *Execution) {
	exec.mu.Lock()
	now := time.Now()
	exec.StartedAt = &now
	exec.State = JobStateRunning
	exec.mu.Unlock()

	result, err := e.executeWorkflow(ctx, exec.Workflow)

	exec.mu.Lock()
	exec.FinishedAt = func() *time.Time { t := time.Now(); return &t }()
	if err != nil {
		exec.State = JobStateFailed
		exec.Error = err.Error()
	} else {
		exec.State = JobStateCompleted
		exec.Result = result
	}
	exec.Progress = 100
	exec.mu.Unlock()
}

// executeWorkflow executes all nodes in topological order.
func (e *Executor) executeWorkflow(ctx context.Context, wf *Workflow) (*ExecutionResult, error) {
	// Build dependency graph
	order, err := topologicalSort(wf)
	if err != nil {
		return nil, err
	}

	// Track node outputs
	outputs := make(map[string]map[int]interface{})

	for i, nodeID := range order {
		node, ok := wf.Nodes[nodeID]
		if !ok {
			continue
		}

		// Resolve inputs from previous node outputs
		resolvedInputs := e.resolveInputs(node.Inputs, wf.Edges, outputs)

		exec, ok := e.executors[node.ClassType]
		if !ok {
			return nil, fmt.Errorf("unsupported node type: %s", node.ClassType)
		}

		output, err := exec.Execute(ctx, &node, resolvedInputs)
		if err != nil {
			return nil, fmt.Errorf("node %s (%s) failed: %w", nodeID, node.ClassType, err)
		}

		outputs[nodeID] = output

		// Update progress
		progress := float64(i+1) / float64(len(order)) * 100
		_ = progress // Could update execution progress here
	}

	// Collect final outputs
	result := &ExecutionResult{
		Metadata: make(map[string]interface{}),
	}

	// Find output nodes and collect their results
	for _, node := range wf.Nodes {
		switch node.ClassType {
		case "SaveImage", "VAEDecode":
			if out, ok := outputs[node.ID]; ok {
				for _, v := range out {
					if s, ok := v.(string); ok && len(s) > 0 {
						result.Images = append(result.Images, s)
					}
				}
			}
		case "VAESave":
			if out, ok := outputs[node.ID]; ok {
				for _, v := range out {
					if s, ok := v.(string); ok && len(s) > 0 {
						result.Images = append(result.Images, s)
					}
				}
			}
		}
	}

	return result, nil
}

// resolveInputs resolves node inputs from edge connections.
func (e *Executor) resolveInputs(inputs map[string]interface{}, edges []Edge, outputs map[string]map[int]interface{}) map[string]interface{} {
	resolved := make(map[string]interface{})
	for k, v := range inputs {
		resolved[k] = v
	}

	for _, edge := range edges {
		if _, ok := inputs[edge.TargetInput]; ok {
			if nodeOutputs, ok := outputs[edge.Source]; ok {
				if out, ok := nodeOutputs[edge.SourceOutput]; ok {
					resolved[edge.TargetInput] = out
				}
			}
		}
	}

	return resolved
}

// Get returns an execution by ID.
func (e *Executor) Get(id string) (*Execution, bool) {
	e.jobsMu.RLock()
	defer e.jobsMu.RUnlock()
	exec, ok := e.jobs[id]
	return exec, ok
}

// Cancel cancels a running execution.
func (e *Executor) Cancel(id string) error {
	e.jobsMu.Lock()
	defer e.jobsMu.Unlock()
	if exec, ok := e.jobs[id]; ok {
		if exec.State == JobStateRunning {
			exec.State = JobStateCancelled
		}
		return nil
	}
	return fmt.Errorf("execution not found: %s", id)
}

// topologicalSort returns nodes in execution order.
func topologicalSort(wf *Workflow) ([]string, error) {
	inDegree := make(map[string]int)
	for id := range wf.Nodes {
		inDegree[id] = 0
	}

	// Count incoming edges
	for _, edge := range wf.Edges {
		inDegree[edge.Target]++
	}

	// Start with nodes that have no dependencies
	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	var result []string
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		result = append(result, nodeID)

		// Reduce in-degree for dependent nodes
		for _, edge := range wf.Edges {
			if edge.Source == nodeID {
				inDegree[edge.Target]--
				if inDegree[edge.Target] == 0 {
					queue = append(queue, edge.Target)
				}
			}
		}
	}

	if len(result) != len(wf.Nodes) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return result, nil
}
