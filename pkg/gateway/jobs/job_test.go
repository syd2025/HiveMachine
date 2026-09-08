package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// mockAPIKey returns "test-key" for any request.
func mockAPIKey(*gin.Context) string { return "test-key" }

// ─── Job state machine ─────────────────────────────────────────────────────────

func TestJobState_Valid(t *testing.T) {
	terminal := []JobState{JobStateSucceeded, JobStateFailed, JobStateCancelled}
	nonTerminal := []JobState{JobStatePending, JobStateRunning}

	for _, s := range terminal {
		if !s.Valid() {
			t.Errorf("%s: expected Valid()==true", s)
		}
	}
	for _, s := range nonTerminal {
		if s.Valid() {
			t.Errorf("%s: expected Valid()==false", s)
		}
	}
}

func TestJob_Start(t *testing.T) {
	j := &Job{State: JobStatePending}
	if err := j.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if j.State != JobStateRunning {
		t.Errorf("State: got %s, want running", j.State)
	}
}

func TestJob_Start_InvalidTransition(t *testing.T) {
	j := &Job{State: JobStateRunning}
	if err := j.Start(); err == nil {
		t.Errorf("Start from running: expected error")
	}
}

func TestJob_Succeed(t *testing.T) {
	j := &Job{State: JobStateRunning}
	result := json.RawMessage(`{"answer":"42"}`)
	if err := j.Succeed(result); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if j.State != JobStateSucceeded {
		t.Errorf("State: got %s", j.State)
	}
	if string(j.Result) != `{"answer":"42"}` {
		t.Errorf("Result: got %s", string(j.Result))
	}
}

func TestJob_Fail(t *testing.T) {
	j := &Job{State: JobStateRunning}
	if err := j.Fail("oops"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if j.State != JobStateFailed {
		t.Errorf("State: got %s", j.State)
	}
	if j.Error != "oops" {
		t.Errorf("Error: got %q", j.Error)
	}
}

func TestJob_Cancel_FromPending(t *testing.T) {
	j := &Job{State: JobStatePending}
	if err := j.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if j.State != JobStateCancelled {
		t.Errorf("State: got %s", j.State)
	}
}

func TestJob_Cancel_FromRunning(t *testing.T) {
	j := &Job{State: JobStateRunning}
	if err := j.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if j.State != JobStateCancelled {
		t.Errorf("State: got %s", j.State)
	}
}

func TestJob_Cancel_FromTerminal(t *testing.T) {
	j := &Job{State: JobStateSucceeded}
	if err := j.Cancel(); err == nil {
		t.Errorf("Cancel from succeeded: expected error")
	}
}

func TestJob_InvalidTransitions(t *testing.T) {
	cases := [][2]JobState{
		{JobStatePending, JobStateSucceeded},
		{JobStatePending, JobStateFailed},
		{JobStateSucceeded, JobStateRunning},
		{JobStateFailed, JobStateRunning},
		{JobStateCancelled, JobStateRunning},
	}
	for _, c := range cases {
		j := &Job{State: c[0]}
		if err := j.transitionTo(c[1]); err == nil {
			t.Errorf("transition %s→%s: expected error", c[0], c[1])
		}
	}
}

func TestJob_AddStepEvent(t *testing.T) {
	j := &Job{State: JobStateRunning}
	time.Sleep(10 * time.Millisecond)

	j.AddStepEvent(StepEvent{Step: "step1", Status: "started"})
	if len(j.Steps) != 1 {
		t.Fatalf("Steps length: got %d, want 1", len(j.Steps))
	}
	if j.Steps[0].Step != "step1" {
		t.Errorf("Step: got %q", j.Steps[0].Step)
	}
	if j.Steps[0].At == 0 {
		t.Errorf("Step.At not set")
	}
}

func TestJob_TransitionTo(t *testing.T) {
	j := &Job{State: JobStatePending}
	if err := j.TransitionTo(JobStateRunning); err != nil {
		t.Fatalf("TransitionTo: %v", err)
	}
	if j.State != JobStateRunning {
		t.Errorf("State: got %s", j.State)
	}
}

// ─── InMemoryStore ─────────────────────────────────────────────────────────────

func TestInMemoryStore_SaveAndByID(t *testing.T) {
	store := NewInMemoryStore()
	j := &Job{ID: "job-1", APIKey: "key", State: JobStatePending}
	if err := store.Save(j); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fetched, err := store.ByID("job-1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if fetched == nil {
		t.Fatal("ByID returned nil")
	}
	if fetched.ID != "job-1" {
		t.Errorf("ID: got %s", fetched.ID)
	}
}

func TestInMemoryStore_ByID_NotFound(t *testing.T) {
	store := NewInMemoryStore()
	j, err := store.ByID("nonexistent")
	if err != nil {
		t.Fatalf("ByID: unexpected error: %v", err)
	}
	if j != nil {
		t.Errorf("ByID(nonexistent): got %v, want nil", j)
	}
}

func TestInMemoryStore_ByAPIKey(t *testing.T) {
	store := NewInMemoryStore()
	for _, j := range []*Job{
		{ID: "a1", APIKey: "key-a", State: JobStatePending},
		{ID: "a2", APIKey: "key-a", State: JobStatePending},
		{ID: "b1", APIKey: "key-b", State: JobStatePending},
	} {
		store.Save(j)
	}

	byA, err := store.ByAPIKey("key-a")
	if err != nil {
		t.Fatalf("ByAPIKey: %v", err)
	}
	if len(byA) != 2 {
		t.Errorf("ByAPIKey(key-a): got %d, want 2", len(byA))
	}

	byB, _ := store.ByAPIKey("key-b")
	if len(byB) != 1 {
		t.Errorf("ByAPIKey(key-b): got %d, want 1", len(byB))
	}

	none, _ := store.ByAPIKey("key-c")
	if len(none) != 0 {
		t.Errorf("ByAPIKey(unknown): got %d, want 0", len(none))
	}
}

func TestInMemoryStore_List(t *testing.T) {
	store := NewInMemoryStore()
	for i := 1; i <= 5; i++ {
		store.Save(&Job{ID: string(rune('0' + i)), State: JobStatePending})
	}

	list, err := store.List(3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("List(3): got %d, want 3", len(list))
	}

	list10, _ := store.List(10)
	if len(list10) != 5 {
		t.Errorf("List(10): got %d, want 5", len(list10))
	}
}

func TestInMemoryStore_List_Order(t *testing.T) {
	store := NewInMemoryStore()
	// Assign descending CreatedAt so order is deterministic.
	for i := 5; i >= 1; i-- {
		store.Save(&Job{
			ID:        string(rune('0' + i)),
			State:    JobStatePending,
			CreatedAt: int64(i),
			UpdatedAt: int64(i),
		})
	}

	list, _ := store.List(3)
	if len(list) != 3 {
		t.Fatalf("got %d", len(list))
	}
	// r5 has CreatedAt=5 → most recent → first.
	if list[0].ID != "5" {
		t.Errorf("List(3)[0]: got %s, want 5 (most recent)", list[0].ID)
	}
}

func TestInMemoryStore_List_Empty(t *testing.T) {
	store := NewInMemoryStore()
	list, err := store.List(10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List(empty): got %d, want 0", len(list))
	}
}

func TestInMemoryStore_Concurrent(t *testing.T) {
	store := NewInMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := strings.Repeat(string(rune('a'+idx)), 4)
			j := &Job{ID: id, APIKey: "key", State: JobStatePending}
			store.Save(j)
			store.ByID(id)
			store.ByAPIKey("key")
			store.List(5)
		}(i)
	}
	wg.Wait()
}

// ─── Handler HTTP endpoints ────────────────────────────────────────────────────

func testContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}

func TestHandler_Submit_Success(t *testing.T) {
	h := NewHandler(NewInMemoryStore(), nil, mockAPIKey)

	body := `{"model":"gpt-4o","steps":[{"name":"step1","prompt":"Hello"}]}`
	c, w := testContext()
	c.Request, _ = http.NewRequest("POST", "/v1/workflows", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Submit(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("Submit: status=%d, want 202", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["job_id"] == nil || resp["job_id"] == "" {
		t.Errorf("Submit: missing job_id")
	}
	if resp["state"] != "pending" {
		t.Errorf("state: got %v", resp["state"])
	}
}

func TestHandler_Submit_EmptySteps(t *testing.T) {
	h := NewHandler(NewInMemoryStore(), nil, mockAPIKey)

	body := `{"model":"gpt-4o","steps":[]}`
	c, w := testContext()
	c.Request, _ = http.NewRequest("POST", "/v1/workflows", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Submit(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Submit(empty steps): got %d, want 400", w.Code)
	}
}

func TestHandler_Submit_InvalidJSON(t *testing.T) {
	h := NewHandler(NewInMemoryStore(), nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("POST", "/v1/workflows", strings.NewReader("{invalid}"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Submit(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Submit(invalid JSON): got %d, want 400", w.Code)
	}
}

func TestHandler_ListJobs(t *testing.T) {
	store := NewInMemoryStore()
	for _, id := range []string{"j1", "j2"} {
		store.Save(&Job{ID: id, APIKey: "test-key", State: JobStateSucceeded})
	}
	h := NewHandler(store, nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs", nil)

	h.ListJobs(c)

	if w.Code != http.StatusOK {
		t.Errorf("ListJobs: got %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	jobs := resp["jobs"].([]interface{})
	if len(jobs) != 2 {
		t.Errorf("ListJobs: got %d, want 2", len(jobs))
	}
}

func TestHandler_ListJobs_WithAPIKey(t *testing.T) {
	store := NewInMemoryStore()
	store.Save(&Job{ID: "j1", APIKey: "key-a", State: JobStateSucceeded})
	store.Save(&Job{ID: "j2", APIKey: "key-b", State: JobStateSucceeded})
	h := NewHandler(store, nil, func(*gin.Context) string { return "key-a" })

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs?api_key=key-a", nil)

	h.ListJobs(c)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	jobs := resp["jobs"].([]interface{})
	if len(jobs) != 1 {
		t.Errorf("ListJobs(api_key=key-a): got %d, want 1", len(jobs))
	}
}

func TestHandler_ListJobs_Limit(t *testing.T) {
	store := NewInMemoryStore()
	for i := 0; i < 5; i++ {
		store.Save(&Job{ID: string(rune('a' + i)), APIKey: "key", State: JobStateSucceeded, CreatedAt: int64(i)})
	}
	h := NewHandler(store, nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs?limit=2", nil)

	h.ListJobs(c)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	got := len(resp["jobs"].([]interface{}))
	if got != 2 {
		t.Errorf("ListJobs(limit=2): got %d, want 2", got)
	}
}

func TestHandler_GetJob_Found(t *testing.T) {
	store := NewInMemoryStore()
	store.Save(&Job{ID: "job-123", APIKey: "test-key", State: JobStateRunning})
	h := NewHandler(store, nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs/job-123", nil)
	c.Params = []gin.Param{{Key: "id", Value: "job-123"}}

	h.GetJob(c)

	if w.Code != http.StatusOK {
		t.Errorf("GetJob: got %d, want 200", w.Code)
	}
}

func TestHandler_GetJob_NotFound(t *testing.T) {
	h := NewHandler(NewInMemoryStore(), nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs/nope", nil)
	c.Params = []gin.Param{{Key: "id", Value: "nope"}}

	h.GetJob(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetJob(not found): got %d, want 404", w.Code)
	}
}

func TestHandler_GetJob_WrongAPIKey(t *testing.T) {
	store := NewInMemoryStore()
	store.Save(&Job{ID: "job-x", APIKey: "key-a", State: JobStateRunning})
	h := NewHandler(store, nil, func(*gin.Context) string { return "key-b" })

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs/job-x?api_key=key-b", nil)
	c.Params = []gin.Param{{Key: "id", Value: "job-x"}}

	h.GetJob(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetJob(wrong key): got %d, want 404", w.Code)
	}
}

func TestHandler_CancelJob(t *testing.T) {
	store := NewInMemoryStore()
	store.Save(&Job{ID: "cancel-me", APIKey: "test-key", State: JobStatePending})
	h := NewHandler(store, nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("DELETE", "/v1/jobs/cancel-me", nil)
	c.Params = []gin.Param{{Key: "id", Value: "cancel-me"}}

	h.CancelJob(c)

	if w.Code != http.StatusOK {
		t.Errorf("CancelJob: got %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["state"] != "cancelled" {
		t.Errorf("state: got %v", resp["state"])
	}
}

func TestHandler_CancelJob_TerminalState(t *testing.T) {
	store := NewInMemoryStore()
	store.Save(&Job{ID: "done", APIKey: "test-key", State: JobStateSucceeded})
	h := NewHandler(store, nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("DELETE", "/v1/jobs/done", nil)
	c.Params = []gin.Param{{Key: "id", Value: "done"}}

	h.CancelJob(c)

	if w.Code != http.StatusConflict {
		t.Errorf("CancelJob(terminal): got %d, want 409", w.Code)
	}
}

func TestHandler_Events_NotFound(t *testing.T) {
	h := NewHandler(NewInMemoryStore(), nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs/nope/events", nil)
	c.Params = []gin.Param{{Key: "id", Value: "nope"}}

	h.Events(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("Events(not found): got %d, want 404", w.Code)
	}
}

func TestHandler_Events_CompletedJob(t *testing.T) {
	store := NewInMemoryStore()
	store.Save(&Job{
		ID:     "done-job",
		APIKey: "test-key",
		State:  JobStateSucceeded,
		Steps:  []StepEvent{{Step: "s1", Status: "completed"}},
	})
	h := NewHandler(store, nil, mockAPIKey)

	c, w := testContext()
	c.Request, _ = http.NewRequest("GET", "/v1/jobs/done-job/events", nil)
	c.Params = []gin.Param{{Key: "id", Value: "done-job"}}

	h.Events(c)

	body := w.Body.String()
	if !strings.Contains(body, "succeeded") && !strings.Contains(body, `"done"`) {
		t.Errorf("Events(terminal): body=%q, want done/succeeded", body)
	}
}
