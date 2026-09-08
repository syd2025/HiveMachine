# Extended APIs Specification

## Overview

Implement extended OpenAI-compatible API endpoints for embeddings, image generation, audio processing, and ComfyUI workflows.

## Endpoint Matrix

| Endpoint | Method | Description | Status |
|----------|--------|-------------|--------|
| `/v1/embeddings` | POST | Text embeddings | To implement |
| `/v1/images/generations` | POST | Image generation | To implement |
| `/v1/images/edits` | POST | Image editing | To implement |
| `/v1/images/variations` | POST | Image variations | To implement |
| `/v1/audio/speech` | POST | Text-to-speech | To implement |
| `/v1/audio/transcriptions` | POST | Speech-to-text | To implement |
| `/v1/audio/generations` | POST | Audio generation | To implement |
| `/v1/music/generations` | POST | Music generation | To implement |
| `/v1/videos` | POST | Video generation | To implement |
| `/v1/workflows` | POST | ComfyUI workflows | To implement |
| `/v1/responses` | POST | Alternate response format | To implement |
| `/v1/jobs/<id>` | GET | Async job status | To implement |
| `/v1/jobs/<id>/result` | GET | Job result | To implement |
| `/v1/jobs/<id>/artifacts/<id>` | GET | Download artifact | To implement |

## Common Patterns

### Async Job Pattern

For long-running operations, use async job pattern:

```go
// Request with async preference
type AsyncRequest struct {
    Model           string   `json:"model"`
    Prefer          string   `json:"prefer"`  // "respond-async"
    IdempotencyKey  string   `json:"idempotency_key,omitempty"`
}

// Response for async request
type AsyncResponse struct {
    JobID    string `json:"x-mayhem-job-id"`
    Status   int    `json:"status"`  // 202 Accepted
}

// Job status response
type JobStatus struct {
    ID        string     `json:"id"`
    Status    JobState   `json:"status"`
    CreatedAt time.Time  `json:"created_at"`
    UpdatedAt time.Time  `json:"updated_at"`
    Result    *JobResult `json:"result,omitempty"`
    Error     *JobError  `json:"error,omitempty"`
}

// JobState
type JobState string

const (
    JobStatePending   JobState = "pending"
    JobStateRunning   JobState = "running"
    JobStateCompleted JobState = "completed"
    JobStateFailed    JobState = "failed"
    JobStateCancelled JobState = "cancelled"
)
```

### Usage Tracking

```go
// All extended endpoints track usage
type ExtendedUsage struct {
    InputTokens  int64  `json:"prompt_tokens,omitempty"`
    OutputTokens int64  `json:"completion_tokens,omitempty"`
    TotalTokens  int64  `json:"total_tokens,omitempty"`
    
    // Media-specific
    Images      int    `json:"images,omitempty"`
    AudioSeconds float64 `json:"audio_seconds,omitempty"`
    VideoSeconds float64 `json:"video_seconds,omitempty"`
    Steps       int    `json:"steps,omitempty"`
}
```

## Embeddings

### Request

```go
type EmbeddingRequest struct {
    Model string   `json:"model"`  // e.g., "BAAI/bge-m3", "Qwen/Qwen3-Embedding-0.6B"
    Input interface{} `json:"input"`  // string, []string, or []int
    EncodingFormat string `json:"encoding_format"`  // "float" or "base64"
    Dimensions    *int   `json:"dimensions,omitempty"`
    User          string `json:"user,omitempty"`
}
```

### Response

```go
type EmbeddingResponse struct {
    Object string           `json:"object"`
    Data   []EmbeddingItem  `json:"data"`
    Model  string           `json:"model"`
    Usage  ExtendedUsage    `json:"usage"`
}

type EmbeddingItem struct {
    Object    string    `json:"object"`
    Embedding []float64 `json:"embedding"`
    Index     int       `json:"index"`
}
```

## Image Generation

### Request

```go
type ImageGenerationRequest struct {
    Model      string           `json:"model"`  // e.g., "Krea/base-le1-2mp"
    Prompt     string           `json:"prompt"`
    N          int              `json:"n,omitempty"`  // Default 1
    Size       string           `json:"size,omitempty"`  // "1024x1024", "512x512"
    ResponseFormat string       `json:"response_format"`  // "url" or "b64_json"
    Style      string           `json:"style,omitempty"`  // "natural", "vivid"
    
    // Reference image support
    Image      string           `json:"image,omitempty"`  // base64 or URL
    Mask       string           `json:"mask,omitempty"`
    
    // Advanced controls
    Steps      int              `json:"steps,omitempty"`
    Seed       int64            `json:"seed,omitempty"`
    Strength   float64          `json:"strength,omitempty"`  // For editing
    
    // ControlNet / LoRA (future)
    ControlNet []ControlNetUnit `json:"controlnet,omitempty"`
}
```

### Response

```go
type ImageGenerationResponse struct {
    Created int64           `json:"created"`
    Data    []ImageResult   `json:"data"`
    Usage   ExtendedUsage   `json:"usage"`
}

type ImageResult struct {
    URL     string `json:"url,omitempty"`
    B64JSON string `json:"b64_json,omitempty"`
    RevisedPrompt string `json:"revised_prompt,omitempty"`
}
```

## Audio Processing

### Speech (TTS)

```go
type SpeechRequest struct {
    Model          string `json:"model"`  // e.g., "hexgrad/Kokoro-82M", "ResembleAI/chatterbox"
    Input          string `json:"input"`
    Voice          string `json:"voice"`  // "default" or voice ID
    ResponseFormat string `json:"response_format"`  // "mp3", "wav", "opus"
    Speed          float64 `json:"speed,omitempty"`  // 0.25 - 4.0
    
    // Cloning support (Chatterbox)
    ReferenceAudio *ReferenceAudio `json:"reference_audio,omitempty"`
}

type ReferenceAudio struct {
    Data        string `json:"data"`         // base64 WAV
    Encoding    string `json:"encoding"`     // "base64"
    ContentType string `json:"content_type"` // "audio/wav"
}
```

### Transcription (STT)

```go
type TranscriptionRequest struct {
    Model       string  `json:"model"`  // e.g., "nvidia/parakeet-tdt-0.6b-v3"
    File        string  `json:"file"`   // File to transcribe (multipart)
    Language    string  `json:"language,omitempty"`
    Prompt      string  `json:"prompt,omitempty"`  // Context for better accuracy
    ResponseFormat string `json:"response_format"`  // "json", "text", "srt", "verbose_json"
    Temperature float64 `json:"temperature,omitempty"`
}

type TranscriptionResponse struct {
    Text       string   `json:"text"`
    Language   string   `json:"language,omitempty"`
    Duration   float64  `json:"duration,omitempty"`
    Words      []Word   `json:"words,omitempty"`
}
```

## Video Generation

### Request

```go
type VideoGenerationRequest struct {
    Model     string   `json:"model"`  // e.g., "video.minimax_h3.t2v_i2v"
    Prompt    string   `json:"prompt"`
    
    // Input media (for I2V/R2V)
    Image     string   `json:"image,omitempty"`  // base64 or URL
    Video     string   `json:"video,omitempty"`  // base64 or URL (R2V)
    
    // Generation parameters
    Duration  int      `json:"duration,omitempty"`  // Seconds
    Resolution string  `json:"resolution,omitempty"`  // "896x512", etc.
    Steps     int      `json:"steps,omitempty"`
    
    // Audio (for video with audio)
    AudioRef  string   `json:"audio_ref,omitempty"`
}

type VideoGenerationResponse struct {
    JobID  string `json:"x-mayhem-job-id"`
    Status int    `json:"status"`  // 202
}
```

## ComfyUI Workflows

### Workflow Structure

```go
type WorkflowRequest struct {
    Model     string            `json:"model"`  // Workflow-enabled model ID
    Workflow  map[string]Node   `json:"workflow"`  // Node graph
    InputFiles []InputFile      `json:"input_files,omitempty"`  // Source media
    
    // Output control
    ResponseFormat string       `json:"response_format"`  // "artifact" or "url"
}

type Node struct {
    ClassType string                 `json:"class_type"`
    Inputs    map[string]interface{} `json:"inputs"`
}

type InputFile struct {
    Name      string `json:"name"`
    Data      string `json:"data"`        // base64
    Encoding  string `json:"encoding"`    // "base64"
    ContentType string `json:"content_type"`
}
```

### Supported Node Types

Based on signed parts index, only whitelisted nodes are allowed:

```go
// Whitelist from COMFY-CHEATSHEET.md
var AllowedNodeTypes = map[string]bool{
    // Image generation
    "KSampler":              true,
    "KSamplerAdvanced":      true,
    "CheckpointLoaderSimple": true,
    "CLIPTextEncode":        true,
    "VAEDecode":             true,
    "VAEEncode":             true,
    "VAELoader":             true,
    "EmptyLatentImage":      true,
    "SaveImage":             true,
    
    // Upscaling
    "UpscaleModelLoader":    true,
    "ImageUpscaleWithModel": true,
    
    // Video
    "VHS_VideoCombine":      true,
    "VHS_LoadVideo":         true,
    
    // Audio
    "AudioEncode":           true,
    "AudioDecode":           true,
    "SaveAudio":             true,
    
    // Control
    "ControlNetApply":       true,
    "ControlNetLoader":      true,
    "LoraLoader":            true,
}
```

### Workflow Policy Validation

```go
type WorkflowPolicy struct {
    AllowedNodes    []string           `json:"allowed_nodes"`
    RequiredParts   []string           `json:"required_parts"`
    OutputClass     string             `json:"outcome_class"`
    MaxSteps        int                `json:"max_steps"`
    MaxResolution   string             `json:"max_resolution"`
    TimeoutSeconds  int                `json:"timeout_seconds"`
}

// Validation
func ValidateWorkflow(policy *WorkflowPolicy, workflow map[string]Node) error {
    for nodeID, node := range workflow {
        if !policy.AllowedNodes[node.ClassType] {
            return fmt.Errorf("node %s uses disallowed type %s", nodeID, node.ClassType)
        }
    }
    // Check graph hash matches policy
    return nil
}
```

## API Handler Interface

```go
type ExtendedAPIHandler interface {
    // Embeddings
    HandleEmbeddings(c *gin.Context) error
    
    // Images
    HandleImageGenerations(c *gin.Context) error
    HandleImageEdits(c *gin.Context) error
    HandleImageVariations(c *gin.Context) error
    
    // Audio
    HandleSpeech(c *gin.Context) error
    HandleTranscriptions(c *gin.Context) error
    HandleAudioGenerations(c *gin.Context) error
    
    // Video
    HandleVideos(c *gin.Context) error
    
    // Workflows
    HandleWorkflows(c *gin.Context) error
    
    // Jobs
    HandleJobStatus(c *gin.Context) error
    HandleJobResult(c *gin.Context) error
    HandleJobArtifact(c *gin.Context) error
}
```

## Error Codes

| Error | Description |
|-------|-------------|
| `invalid_request.model_not_available` | Model not in catalog |
| `invalid_request.media_assembly_failed` | Failed to process media input |
| `invalid_request.workflow_invalid_node` | Workflow uses disallowed node |
| `invalid_request.workflow_exceeds_limits` | Workflow exceeds policy limits |
| `route_selection.required_modality_unavailable` | Required modality not available |
| `provider_admission.no_capacity` | No provider has capacity |

## Acceptance Criteria

- **A13.1**: `/v1/embeddings` returns valid float vectors
- **A14.1**: `/v1/images/generations` creates image
- **A14.2**: Image variations and edits work
- **A15.1**: `/v1/audio/transcriptions` converts speech to text
- **A15.2**: `/v1/audio/speech` generates audio from text
- **A16.1**: `/v1/workflows` executes ComfyUI graph
- **A16.2**: Workflow node whitelist is enforced
- **A16.3**: Workflow timeout is enforced
- **A16.4**: Job status polling works
- **A16.5**: Async jobs return 202 with job ID

## Backend Integration

Extended APIs route to specialized backends:

```
Extended API Request
        │
        ▼
┌───────────────────┐
│ ExtendedAPIHandler │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐     ┌───────────────────┐
│ Model Routing     │────►│ Provider Selection │
└───────────────────┘     └─────────┬─────────┘
                                    │
            ┌───────────────────────┼───────────────────────┐
            │                       │                       │
            ▼                       ▼                       ▼
    ┌───────────────┐     ┌───────────────┐     ┌───────────────┐
    │ Image Engine  │     │ Audio Engine  │     │ Video Engine  │
    │ (ComfyUI/vLLM)│     │ (Whisper/Piper)     │ (ComfyUI)     │
    └───────────────┘     └───────────────┘     └───────────────┘
```
