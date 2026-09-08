package agent

import (
	"testing"
	"time"

	"github.com/hivemachine/pkg/types"
)

func TestRuntime_Spawn(t *testing.T) {
	runtime := NewRuntime()

	agent, err := runtime.Spawn("test-agent", "planner", "Test Planner", []types.AgentCapability{types.CapabilityPlanner})
	if err != nil {
		t.Fatalf("Failed to spawn agent: %v", err)
	}

	if agent.ID != "test-agent" {
		t.Errorf("Expected agent ID 'test-agent', got '%s'", agent.ID)
	}

	if agent.Type != "planner" {
		t.Errorf("Expected agent type 'planner', got '%s'", agent.Type)
	}
}

func TestRuntime_Spawn_Duplicate(t *testing.T) {
	runtime := NewRuntime()

	_, err := runtime.Spawn("test-agent", "planner", "Test Planner", []types.AgentCapability{types.CapabilityPlanner})
	if err != nil {
		t.Fatalf("First spawn failed: %v", err)
	}

	_, err = runtime.Spawn("test-agent", "executor", "Test Executor", []types.AgentCapability{types.CapabilityExecutor})
	if err == nil {
		t.Error("Expected error for duplicate agent ID, got nil")
	}
}

func TestRuntime_Send(t *testing.T) {
	runtime := NewRuntime()

	agent, _ := runtime.Spawn("receiver", "planner", "Receiver", []types.AgentCapability{types.CapabilityPlanner})

	// Start listening
	done := make(chan bool)
	agent.Start(func(msg *types.Message) error {
		if msg.Type == "test" {
			done <- true
		}
		return nil
	})

	runtime.Send("receiver", "sender", "test", "hello")

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Message not received within timeout")
	}
}

func TestRuntime_ListAgents(t *testing.T) {
	runtime := NewRuntime()

	runtime.Spawn("agent-1", "planner", "Planner 1", []types.AgentCapability{types.CapabilityPlanner})
	runtime.Spawn("agent-2", "executor", "Executor 1", []types.AgentCapability{types.CapabilityExecutor})

	agents := runtime.ListAgents()
	if len(agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agents))
	}
}

func TestRuntime_Stop(t *testing.T) {
	runtime := NewRuntime()

	_, err := runtime.Spawn("test-agent", "planner", "Test", []types.AgentCapability{types.CapabilityPlanner})
	if err != nil {
		t.Fatalf("Failed to spawn agent: %v", err)
	}

	err = runtime.Stop("test-agent")
	if err != nil {
		t.Fatalf("Failed to stop agent: %v", err)
	}

	agents := runtime.ListAgents()
	if len(agents) != 0 {
		t.Errorf("Expected 0 agents after stop, got %d", len(agents))
	}
}

func TestRuntime_FindByCapability(t *testing.T) {
	runtime := NewRuntime()

	runtime.Spawn("planner-1", "planner", "Planner 1", []types.AgentCapability{types.CapabilityPlanner})
	runtime.Spawn("executor-1", "executor", "Executor 1", []types.AgentCapability{types.CapabilityExecutor})
	runtime.Spawn("both-1", "hybrid", "Both 1", []types.AgentCapability{types.CapabilityPlanner, types.CapabilityExecutor})

	planners := runtime.FindByCapability(types.CapabilityPlanner)
	if len(planners) != 2 {
		t.Errorf("Expected 2 planners, got %d", len(planners))
	}

	executors := runtime.FindByCapability(types.CapabilityExecutor)
	if len(executors) != 2 {
		t.Errorf("Expected 2 executors, got %d", len(executors))
	}
}
