package task

import (
	"testing"

	"github.com/hivemachine/pkg/types"
)

func TestDistributor_Submit(t *testing.T) {
	d := NewDistributor()

	task := &types.Task{
		ID:      "task-1",
		Type:    "plan",
		Payload: map[string]any{"key": "value"},
	}

	err := d.Submit(task)
	if err != nil {
		t.Fatalf("Failed to submit task: %v", err)
	}

	got, ok := d.GetTask("task-1")
	if !ok {
		t.Fatal("Task not found after submit")
	}

	if got.Status != types.TaskStatusPending {
		t.Errorf("Expected status Pending, got %s", got.Status)
	}
}

func TestDistributor_Submit_Duplicate(t *testing.T) {
	d := NewDistributor()

	task := &types.Task{
		ID:      "task-1",
		Type:    "plan",
		Status:  types.TaskStatusPending,
		Payload: nil,
	}

	d.Submit(task)

	// Submitting same task again just updates it (idempotent)
	err := d.Submit(task)
	if err != nil {
		t.Errorf("Submit should be idempotent: %v", err)
	}
}

func TestDistributor_Distribute(t *testing.T) {
	d := NewDistributor()

	d.Submit(&types.Task{
		ID:   "task-1",
		Type: "plan",
	})

	task, err := d.Distribute("plan")
	if err != nil {
		t.Fatalf("Failed to distribute: %v", err)
	}

	if task.ID != "task-1" {
		t.Errorf("Expected task-1, got %s", task.ID)
	}

	if task.Status != types.TaskStatusRunning {
		t.Errorf("Expected Running status, got %s", task.Status)
	}
}

func TestDistributor_Distribute_NoTask(t *testing.T) {
	d := NewDistributor()

	_, err := d.Distribute("plan")
	if err == nil {
		t.Error("Expected error when no tasks available")
	}
}

func TestDistributor_ListPending(t *testing.T) {
	d := NewDistributor()

	d.Submit(&types.Task{ID: "task-1", Type: "plan"})
	d.Submit(&types.Task{ID: "task-2", Type: "execute"})
	d.Submit(&types.Task{ID: "task-3", Type: "plan"})

	pending := d.ListPending()
	if len(pending) != 3 {
		t.Errorf("Expected 3 pending tasks, got %d", len(pending))
	}
}

func TestDistributor_Complete(t *testing.T) {
	d := NewDistributor()

	d.Submit(&types.Task{ID: "task-1", Type: "plan"})

	err := d.Complete("task-1")
	if err != nil {
		t.Fatalf("Failed to complete: %v", err)
	}

	task, _ := d.GetTask("task-1")
	if task.Status != types.TaskStatusCompleted {
		t.Errorf("Expected Completed, got %s", task.Status)
	}

	if task.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
}

func TestDistributor_Fail(t *testing.T) {
	d := NewDistributor()

	d.Submit(&types.Task{ID: "task-1", Type: "plan"})

	err := d.Fail("task-1")
	if err != nil {
		t.Fatalf("Failed to fail: %v", err)
	}

	task, _ := d.GetTask("task-1")
	if task.Status != types.TaskStatusFailed {
		t.Errorf("Expected Failed, got %s", task.Status)
	}
}

func TestDistributor_GetTask_NotFound(t *testing.T) {
	d := NewDistributor()

	_, ok := d.GetTask("nonexistent")
	if ok {
		t.Error("Expected not found for nonexistent task")
	}
}
