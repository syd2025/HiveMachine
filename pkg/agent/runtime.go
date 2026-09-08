package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/hivemachine/pkg/types"
)

// Runtime manages agent lifecycle and messaging
type Runtime struct {
	agents   map[string]*Agent
	inbox    chan *types.Message
	mu       sync.RWMutex
	registry map[string]types.AgentCapability
}

// NewRuntime creates a new agent runtime
func NewRuntime() *Runtime {
	return &Runtime{
		agents:   make(map[string]*Agent),
		inbox:    make(chan *types.Message, 100),
		registry: make(map[string]types.AgentCapability),
	}
}

// Agent represents an active agent in the system
type Agent struct {
	ID           string
	Type         string
	Name         string
	Capabilities []types.AgentCapability
	Inbox        chan *types.Message
	Outbox       chan *types.Message
	running      bool
	stopCh       chan struct{}
}

// Spawn creates and starts a new agent
func (r *Runtime) Spawn(id, agentType, name string, capabilities []types.AgentCapability) (*Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[id]; exists {
		return nil, fmt.Errorf("agent %s already exists", id)
	}

	agent := &Agent{
		ID:           id,
		Type:         agentType,
		Name:         name,
		Capabilities: capabilities,
		Inbox:        make(chan *types.Message, 50),
		Outbox:       make(chan *types.Message, 50),
		running:      true,
		stopCh:       make(chan struct{}),
	}

	r.agents[id] = agent
	r.registry[id] = capabilities[0]

	// Start message routing
	go r.routeMessages(agent)

	return agent, nil
}

// routeMessages routes messages from agent outbox to recipient inboxes
func (r *Runtime) routeMessages(agent *Agent) {
	for {
		select {
		case msg := <-agent.Outbox:
			r.mu.RLock()
			recipient, ok := r.agents[msg.To]
			r.mu.RUnlock()
			if ok {
				select {
				case recipient.Inbox <- msg:
				default:
					// Recipient inbox full, drop message
				}
			}
		case <-agent.stopCh:
			return
		}
	}
}

// Send delivers a message to an agent
func (r *Runtime) Send(to, from, msgType string, payload any) {
	msg := &types.Message{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        to,
		Type:      msgType,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	r.mu.RLock()
	agent, ok := r.agents[to]
	r.mu.RUnlock()

	if ok {
		select {
		case agent.Inbox <- msg:
		default:
			// Inbox full
		}
	}
}

// ListAgents returns all registered agents
func (r *Runtime) ListAgents() []*types.AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agents := make([]*types.AgentInfo, 0, len(r.agents))
	for _, a := range r.agents {
		agents = append(agents, &types.AgentInfo{
			ID:          a.ID,
			Type:        a.Type,
			Name:        a.Name,
			Capabilities: a.Capabilities,
		})
	}
	return agents
}

// Stop gracefully stops an agent
func (r *Runtime) Stop(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	agent, ok := r.agents[id]
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}

	agent.running = false
	close(agent.stopCh)
	delete(r.agents, id)
	delete(r.registry, id)

	return nil
}

// GetAgent returns an agent by ID
func (r *Runtime) GetAgent(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[id]
	return agent, ok
}

// FindByCapability finds agents with a specific capability
func (r *Runtime) FindByCapability(cap types.AgentCapability) []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Agent
	for _, a := range r.agents {
		for _, c := range a.Capabilities {
			if c == cap {
				result = append(result, a)
				break
			}
		}
	}
	return result
}

// StartAgent starts the agent's message processing loop
func (a *Agent) Start(handler func(*types.Message) error) {
	go func() {
		for a.running {
			select {
			case msg := <-a.Inbox:
				if err := handler(msg); err != nil {
					// Log error but continue
				}
			case <-a.stopCh:
				return
			}
		}
	}()
}

// Stop stops the agent
func (a *Agent) Stop() {
	a.running = false
	close(a.stopCh)
}
