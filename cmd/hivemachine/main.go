package main

import (
	"fmt"
	"log"
	"os"

	"github.com/hivemachine/pkg/agent"
	"github.com/hivemachine/pkg/task"
	"github.com/hivemachine/pkg/types"
	"github.com/hivemachine/pkg/workflow"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: hivemachine <workflow.yaml>")
		os.Exit(1)
	}

	workflowPath := os.Args[1]

	// Initialize runtime
	runtime := agent.NewRuntime()
	distributor := task.NewDistributor()
	workflowLoader := workflow.NewLoader()

	// Load workflow
	wf, err := workflowLoader.Load(workflowPath)
	if err != nil {
		log.Fatalf("Failed to load workflow: %v", err)
	}

	if err := workflowLoader.Validate(wf); err != nil {
		log.Fatalf("Invalid workflow: %v", err)
	}

	fmt.Printf("Loaded workflow: %s\n", wf.Name)

	// Spawn agents for the workflow
	agentTypes := make(map[string]*agent.Agent)
	for _, binding := range wf.Agents {
		capabilities := make([]types.AgentCapability, len(binding.Capabilities))
		for i, c := range binding.Capabilities {
			capabilities[i] = types.AgentCapability(c)
		}

		a, err := runtime.Spawn(binding.ID, binding.Type, binding.ID, capabilities)
		if err != nil {
			log.Fatalf("Failed to spawn agent %s: %v", binding.ID, err)
		}

		// Start agent with simple handler
		a.Start(func(msg *types.Message) error {
			fmt.Printf("[%s] Received: %s from %s\n", a.ID, msg.Type, msg.From)
			return nil
		})

		agentTypes[binding.ID] = a
		fmt.Printf("Spawned agent: %s (%s)\n", binding.ID, binding.Type)
	}

	// Execute workflow steps
	for _, step := range wf.Steps {
		fmt.Printf("\nExecuting step: %s (%s)\n", step.Name, step.Type)

		// Create task for this step
		taskItem := &types.Task{
			ID:      step.ID,
			Type:    step.Type,
			Payload: step.Input,
		}

		if err := distributor.Submit(taskItem); err != nil {
			log.Printf("Warning: %v", err)
		}

		// Send task to appropriate agent
		if agt, ok := agentTypes[step.Agent]; ok {
			runtime.Send(agt.ID, "orchestrator", "task", taskItem)
		}
	}

	fmt.Println("\nWorkflow completed!")
}
