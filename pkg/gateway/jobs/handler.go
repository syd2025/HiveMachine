package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	pb "github.com/hivemachine/internal/grpc/pb"
)

// ProxyClient is the subset of the gateway's inference proxy needed for job execution.
type ProxyClient interface {
	ChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error)
}

// Handler serves the /v1/workflows and /v1/jobs endpoints.
type Handler struct {
	store    Store
	proxy    ProxyClient
	apiKeyFn func(*gin.Context) string
}

// NewHandler creates a new jobs handler.
func NewHandler(store Store, proxy ProxyClient, apiKeyFn func(*gin.Context) string) *Handler {
	return &Handler{store: store, proxy: proxy, apiKeyFn: apiKeyFn}
}

// WorkflowRequest is the POST /v1/workflows body.
type WorkflowRequest struct {
	Model    string          `json:"model"`
	Steps    []Step         `json:"steps"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// WorkflowResponse is the POST /v1/workflows response.
type WorkflowResponse struct {
	JobID     string `json:"job_id"`
	State    string `json:"state"`
	CreatedAt int64  `json:"created_at"`
}

// Submit handles POST /v1/workflows — creates and starts executing a new job.
func (h *Handler) Submit(c *gin.Context) {
	var req WorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if len(req.Steps) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "steps required"})
		return
	}
	model := req.Model
	if model == "" {
		model = "mayhem/default"
	}

	jobID := uuid.New().String()
	apiKey := h.apiKeyFn(c)
	now := time.Now().Unix()

	job := &Job{
		ID:        jobID,
		APIKey:    apiKey,
		Model:     model,
		Workflow:  req.Steps,
		State:     JobStatePending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.store.Save(job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save job"})
		return
	}

	// Execute workflow asynchronously.
	go h.executeJob(job.ID, job.Workflow, job.Model)

	c.JSON(http.StatusAccepted, WorkflowResponse{
		JobID:     jobID,
		State:    string(job.State),
		CreatedAt: now,
	})
}

// executeJob runs the job steps and updates job state.
func (h *Handler) executeJob(jobID string, workflow []Step, model string) {
	job, err := h.store.ByID(jobID)
	if err != nil || job == nil {
		return
	}
	if err := job.Start(); err != nil {
		h.store.Save(job)
		return
	}

	for _, step := range workflow {
		job.AddStepEvent(StepEvent{Step: step.Name, Status: "started"})
		h.store.Save(job)

		prompt := step.Prompt
		stepModel := model
		if step.Model != "" {
			stepModel = step.Model
		}

		var result json.RawMessage
		if h.proxy != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			resp, err := h.proxy.ChatCompletions(ctx, &pb.ChatCompletionRequest{
				Model: stepModel,
				Messages: []*pb.ChatMessage{{Role: "user", Content: prompt}},
			})
			if err != nil {
				job.AddStepEvent(StepEvent{
					Step:    step.Name,
					Status:  "failed",
					Message: err.Error(),
				})
				job.Fail(err.Error())
				h.store.Save(job)
				return
			}
			if len(resp.Choices) > 0 {
				result = json.RawMessage(fmt.Sprintf(`{"text":%q}`, resp.Choices[0].Message.GetContent()))
			}
			job.AddStepEvent(StepEvent{
				Step:   step.Name,
				Status: "completed",
				Output: result,
			})
		} else {
			// No proxy — simulate step completion.
			job.AddStepEvent(StepEvent{
				Step:   step.Name,
				Status: "completed",
				Output: json.RawMessage(fmt.Sprintf(`{"text":"simulated output for step %q"}`, step.Name)),
			})
		}
		h.store.Save(job)
	}

	resultJSON, _ := json.Marshal(map[string]interface{}{"steps_completed": len(workflow)})
	job.Succeed(resultJSON)
	h.store.Save(job)
}

// ListJobs handles GET /v1/jobs.
func (h *Handler) ListJobs(c *gin.Context) {
	apiKey := c.Query("api_key")
	limit := 20
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	var jobs []*Job
	var err error
	if apiKey != "" {
		jobs, err = h.store.ByAPIKey(apiKey)
	} else {
		jobs, err = h.store.List(limit)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(jobs) > limit {
		jobs = jobs[:limit]
	}

	out := make([]JobResponse, len(jobs))
	for i, j := range jobs {
		out[i] = jobToResponse(j)
	}
	c.JSON(http.StatusOK, gin.H{"jobs": out})
}

// GetJob handles GET /v1/jobs/:id.
func (h *Handler) GetJob(c *gin.Context) {
	id := c.Param("id")
	job, err := h.store.ByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	if apiKey := c.Query("api_key"); apiKey != "" && job.APIKey != apiKey {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, jobToResponse(job))
}

// JobResponse is the public JSON view of a job.
type JobResponse struct {
	ID        string          `json:"id"`
	Model     string          `json:"model"`
	State     JobState        `json:"state"`
	Steps     []StepEvent     `json:"steps,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

func jobToResponse(j *Job) JobResponse {
	return JobResponse{
		ID:        j.ID,
		Model:     j.Model,
		State:     j.State,
		Steps:     j.Steps,
		Result:    j.Result,
		Error:     j.Error,
		CreatedAt: j.CreatedAt,
		UpdatedAt: j.UpdatedAt,
	}
}

// Events handles GET /v1/jobs/:id/events — SSE stream of job step events.
func (h *Handler) Events(c *gin.Context) {
	id := c.Param("id")

	job, err := h.store.ByID(id)
	if err != nil || job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	if apiKey := c.Query("api_key"); apiKey != "" && job.APIKey != apiKey {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Send existing step events immediately.
	for _, ev := range job.Steps {
		sendSSE(c, "step", ev)
	}

	if job.State.Valid() {
		sendSSE(c, "done", job.State)
		c.Writer.Flush()
		return
	}

	// Poll for new events — 30s window.
	deadline := time.Now().Add(30 * time.Second)
	lastSeen := len(job.Steps)

	for time.Now().Before(deadline) {
		job, err = h.store.ByID(id)
		if err != nil || job == nil {
			sendSSE(c, "error", "job not found")
			break
		}
		for i := lastSeen; i < len(job.Steps); i++ {
			sendSSE(c, "step", job.Steps[i])
		}
		lastSeen = len(job.Steps)

		if job.State.Valid() {
			sendSSE(c, "done", job.State)
			break
		}

		c.Writer.Flush()
		time.Sleep(500 * time.Millisecond)
	}
}

func sendSSE(c *gin.Context, event string, data interface{}) {
	payload, _ := json.Marshal(data)
	c.Writer.Write([]byte(fmt.Sprintf("event: %s\n", event)))
	c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", payload)))
}

// CancelJob handles DELETE /v1/jobs/:id.
func (h *Handler) CancelJob(c *gin.Context) {
	id := c.Param("id")
	job, err := h.store.ByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	if apiKey := c.Query("api_key"); apiKey != "" && job.APIKey != apiKey {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	if err := job.Cancel(); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	h.store.Save(job)
	c.JSON(http.StatusOK, jobToResponse(job))
}

// Compile-time interface guard.
var _ io.Writer = (io.Writer)(nil)
