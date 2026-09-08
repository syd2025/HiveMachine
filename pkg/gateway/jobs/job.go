package jobs

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// JobState represents the lifecycle state of a job.
type JobState string

const (
	JobStatePending   JobState = "pending"
	JobStateRunning   JobState = "running"
	JobStateSucceeded JobState = "succeeded"
	JobStateFailed    JobState = "failed"
	JobStateCancelled JobState = "cancelled"
)

// Valid returns true for terminal states.
func (s JobState) Valid() bool {
	return s == JobStateSucceeded || s == JobStateFailed || s == JobStateCancelled
}

var ErrInvalidTransition = errors.New("jobs: invalid state transition")

// StepEvent describes a single step update within a job.
type StepEvent struct {
	Step    string          `json:"step"`
	Status  string          `json:"status"`
	Message string          `json:"message,omitempty"`
	Output  json.RawMessage `json:"output,omitempty"`
	At      int64           `json:"at"`
}

// Job represents a durable async inference job.
type Job struct {
	ID        string      `json:"id"`
	APIKey    string      `json:"api_key"`
	Model     string      `json:"model"`
	Workflow  []Step      `json:"workflow"`
	State     JobState    `json:"state"`
	Steps     []StepEvent `json:"steps"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
	CreatedAt int64       `json:"created_at"`
	UpdatedAt int64       `json:"updated_at"`
}

// Step defines a single step in a job workflow.
type Step struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
}

// transitionTo updates the job state.
func (j *Job) transitionTo(next JobState) error {
	switch j.State {
	case JobStatePending:
		if next != JobStateRunning && next != JobStateCancelled {
			return ErrInvalidTransition
		}
	case JobStateRunning:
		if next != JobStateSucceeded && next != JobStateFailed && next != JobStateCancelled {
			return ErrInvalidTransition
		}
	case JobStateSucceeded, JobStateFailed, JobStateCancelled:
		return ErrInvalidTransition
	}
	j.State = next
	j.UpdatedAt = time.Now().Unix()
	return nil
}

// TransitionTo is the public state transition method.
func (j *Job) TransitionTo(next JobState) error {
	return j.transitionTo(next)
}

// Start marks a pending job as running.
func (j *Job) Start() error {
	return j.transitionTo(JobStateRunning)
}

// Succeed marks a running job as succeeded with an optional result.
func (j *Job) Succeed(result json.RawMessage) error {
	if err := j.transitionTo(JobStateSucceeded); err != nil {
		return err
	}
	j.Result = result
	return nil
}

// Fail marks a running job as failed with an error message.
func (j *Job) Fail(errMsg string) error {
	if err := j.transitionTo(JobStateFailed); err != nil {
		return err
	}
	j.Error = errMsg
	return nil
}

// Cancel marks a pending or running job as cancelled.
func (j *Job) Cancel() error {
	return j.transitionTo(JobStateCancelled)
}

// AddStepEvent appends a step event to the job's step log.
func (j *Job) AddStepEvent(event StepEvent) {
	if j.Steps == nil {
		j.Steps = []StepEvent{}
	}
	event.At = time.Now().Unix()
	j.Steps = append(j.Steps, event)
	j.UpdatedAt = event.At
}

// Store is the persistence layer for jobs.
type Store interface {
	Save(j *Job) error
	ByID(id string) (*Job, error)
	ByAPIKey(apiKey string) ([]*Job, error)
	List(n int) ([]*Job, error)
}

// InMemoryStore holds jobs in process memory.
// Thread-safe via RWMutex.
type InMemoryStore struct {
	mu      sync.RWMutex
	byID     map[string]*Job
	byAPIKey map[string][]*Job
}

// NewInMemoryStore creates an empty in-memory job store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		byID:     make(map[string]*Job),
		byAPIKey: make(map[string][]*Job),
	}
}

// Save implements Store.
func (s *InMemoryStore) Save(j *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[j.ID] = j
	s.byAPIKey[j.APIKey] = append([]*Job{j}, s.byAPIKey[j.APIKey]...)
	return nil
}

// ByID implements Store.
func (s *InMemoryStore) ByID(id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if j, ok := s.byID[id]; ok {
		return j, nil
	}
	return nil, nil
}

// ByAPIKey implements Store.
func (s *InMemoryStore) ByAPIKey(apiKey string) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if jobs, ok := s.byAPIKey[apiKey]; ok {
		return jobs, nil
	}
	return []*Job{}, nil
}

// List implements Store.
func (s *InMemoryStore) List(n int) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]*Job, 0, len(s.byID))
	for _, j := range s.byID {
		jobs = append(jobs, j)
	}
	// Sort newest first by CreatedAt.
	for i := range jobs {
		for j := i + 1; j < len(jobs); j++ {
			if jobs[j].CreatedAt > jobs[i].CreatedAt {
				jobs[i], jobs[j] = jobs[j], jobs[i]
			}
		}
	}
	if n > 0 && len(jobs) > n {
		jobs = jobs[:n]
	}
	return jobs, nil
}
