package task

import (
	"fmt"
	"sync"
	"time"

	"github.com/hivemachine/pkg/types"
)

// Distributor routes tasks to agents based on capabilities
type Distributor struct {
	tasks   map[string]*types.Task
	mu      sync.RWMutex
	runtime interface{ FindByCapability(types.AgentCapability) []*interface{} }
}

// NewDistributor creates a new task distributor
func NewDistributor() *Distributor {
	return &Distributor{
		tasks: make(map[string]*types.Task),
	}
}

// Submit adds a task to the queue
func (d *Distributor) Submit(task *types.Task) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	if task.Status != "" && task.Status != types.TaskStatusPending {
		return fmt.Errorf("task must have pending status")
	}

	task.Status = types.TaskStatusPending
	task.CreatedAt = time.Now()
	d.tasks[task.ID] = task

	return nil
}

// Distribute assigns tasks to available agents based on task type
func (d *Distributor) Distribute(taskType string) (*types.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var assigned *types.Task
	for _, task := range d.tasks {
		if task.Type == taskType && task.Status == types.TaskStatusPending && task.AssignedTo == "" {
			// Find agent with matching capability
			cap := capabilityForTaskType(taskType)
			task.AssignedTo = cap // Simplified - actual impl would query runtime
			task.Status = types.TaskStatusRunning
			assigned = task
			break
		}
	}

	if assigned == nil {
		return nil, fmt.Errorf("no available task of type %s", taskType)
	}

	return assigned, nil
}

// GetTask returns a task by ID
func (d *Distributor) GetTask(id string) (*types.Task, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	task, ok := d.tasks[id]
	return task, ok
}

// ListPending returns all pending tasks
func (d *Distributor) ListPending() []*types.Task {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var pending []*types.Task
	for _, task := range d.tasks {
		if task.Status == types.TaskStatusPending {
			pending = append(pending, task)
		}
	}
	return pending
}

// Complete marks a task as completed
func (d *Distributor) Complete(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	task, ok := d.tasks[id]
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}

	now := time.Now()
	task.Status = types.TaskStatusCompleted
	task.CompletedAt = &now

	return nil
}

// Fail marks a task as failed
func (d *Distributor) Fail(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	task, ok := d.tasks[id]
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}

	task.Status = types.TaskStatusFailed

	return nil
}

// capabilityForTaskType maps task types to required capabilities
func capabilityForTaskType(taskType string) string {
	switch taskType {
	case "plan", "planning":
		return "planner"
	case "execute", "execution":
		return "executor"
	case "report", "reporting":
		return "reporter"
	default:
		return "executor"
	}
}
