package types

import "time"

// Message represents communication between agents
type Message struct {
	ID        string
	From      string
	To        string
	Type      string
	Payload   any
	Timestamp time.Time
}

// Task represents a unit of work to be distributed
type Task struct {
	ID          string
	Type        string
	Payload     any
	AssignedTo  string
	Status      TaskStatus
	CreatedAt   time.Time
	CompletedAt *time.Time
}

// TaskStatus represents the current state of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// AgentCapability represents what an agent can do
type AgentCapability string

const (
	CapabilityPlanner   AgentCapability = "planner"
	CapabilityExecutor  AgentCapability = "executor"
	CapabilityReporter  AgentCapability = "reporter"
	CapabilityOrchestrate AgentCapability = "orchestrate"
)

// AgentInfo holds basic agent metadata
type AgentInfo struct {
	ID          string
	Type        string
	Name        string
	Capabilities []AgentCapability
}
