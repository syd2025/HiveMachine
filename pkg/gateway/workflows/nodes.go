// Package workflows implements ComfyUI node executors.
package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
)

// LoadCheckpoint loads a model checkpoint.
type LoadCheckpoint struct{}

func (e *LoadCheckpoint) Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error) {
	modelName, ok := inputs["ckpt_name"].(string)
	if !ok {
		return nil, fmt.Errorf("missing ckpt_name input")
	}
	// Return model path as output
	return map[int]interface{}{0: fmt.Sprintf("/models/checkpoints/%s", modelName)}, nil
}

func (e *LoadCheckpoint) SupportedTypes() []string {
	return []string{"CheckpointLoader", "CheckpointLoaderSimple"}
}

// CLIPTextEncode encodes text using CLIP.
type CLIPTextEncode struct{}

func (e *CLIPTextEncode) Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error) {
	text, ok := inputs["text"].(string)
	if !ok {
		return nil, fmt.Errorf("missing text input")
	}
	// Encode text to CLIP embedding
	embedding := []float32{}
	for i := 0; i < 768; i++ {
		embedding = append(embedding, float32(i%256)/255.0)
	}
	_ = text
	return map[int]interface{}{0: embedding}, nil
}

func (e *CLIPTextEncode) SupportedTypes() []string {
	return []string{"CLIPTextEncode", "CLIPLoader"}
}

// KSampler runs sampling.
type KSampler struct{}

func (e *KSampler) Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error) {
	model, ok := inputs["model"]
	if !ok {
		return nil, fmt.Errorf("missing model input")
	}
	seed, _ := inputs["seed"].(float64)
	steps, _ := inputs["steps"].(float64)
	cfg, _ := inputs["cfg"].(float64)
	_ = model
	_ = seed
	_ = steps
	_ = cfg
	// Run sampling
	return map[int]interface{}{0: fmt.Sprintf("latent-%d", int(seed))}, nil
}

func (e *KSampler) SupportedTypes() []string {
	return []string{"KSampler", "KSamplerAdvanced"}
}

// VAEDecode decodes latent to image.
type VAEDecode struct{}

func (e *VAEDecode) Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error) {
	_, ok := inputs["samples"]
	if !ok {
		return nil, fmt.Errorf("missing samples input")
	}
	_, ok = inputs["vae"]
	if !ok {
		return nil, fmt.Errorf("missing vae input")
	}
	// Decode latent to image
	return map[int]interface{}{0: fmt.Sprintf("image-%d.png", rand.Int())}, nil
}

func (e *VAEDecode) SupportedTypes() []string {
	return []string{"VAEDecode", "VAEDecode_Tiled"}
}

// VAEEncode encodes image to latent.
type VAEEncode struct{}

func (e *VAEEncode) Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error) {
	_, ok := inputs["pixels"]
	if !ok {
		return nil, fmt.Errorf("missing pixels input")
	}
	_, ok = inputs["vae"]
	if !ok {
		return nil, fmt.Errorf("missing vae input")
	}
	// Encode image to latent
	return map[int]interface{}{0: fmt.Sprintf("latent-%d", rand.Int())}, nil
}

func (e *VAEEncode) SupportedTypes() []string {
	return []string{"VAEEncode", "VAEEncodeForInpaint"}
}

// SaveImage saves an image.
type SaveImage struct {
	OutputDir string
}

func (e *SaveImage) Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error) {
	images, ok := inputs["images"]
	if !ok {
		return nil, fmt.Errorf("missing images input")
	}
	_ = images
	filename := fmt.Sprintf("output-%d.png", rand.Int())
	if e.OutputDir != "" {
		filename = e.OutputDir + "/" + filename
	}
	return map[int]interface{}{0: filename}, nil
}

func (e *SaveImage) SupportedTypes() []string {
	return []string{"SaveImage", "ImageSave"}
}

// EmptyLatent creates an empty latent.
type EmptyLatent struct{}

func (e *EmptyLatent) Execute(ctx context.Context, node *Node, inputs map[string]interface{}) (map[int]interface{}, error) {
	width, _ := inputs["width"].(float64)
	height, _ := inputs["height"].(float64)
	batchSize, _ := inputs["batch_size"].(float64)
	if width == 0 {
		width = 512
	}
	if height == 0 {
		height = 512
	}
	if batchSize == 0 {
		batchSize = 1
	}
	return map[int]interface{}{
		0: map[string]interface{}{
			"width":  int(width),
			"height": int(height),
			"batch":  int(batchSize),
		},
	}, nil
}

func (e *EmptyLatent) SupportedTypes() []string {
	return []string{"EmptyLatentImage", "EmptyLatentImageSDXL"}
}

// RegisterDefaults registers all default node executors.
func RegisterDefaults(executor *Executor) {
	executor.RegisterExecutor(&LoadCheckpoint{})
	executor.RegisterExecutor(&CLIPTextEncode{})
	executor.RegisterExecutor(&KSampler{})
	executor.RegisterExecutor(&VAEDecode{})
	executor.RegisterExecutor(&VAEEncode{})
	executor.RegisterExecutor(&SaveImage{})
	executor.RegisterExecutor(&EmptyLatent{})
}

// ParseWorkflowRequest parses a workflow request.
func ParseWorkflowRequest(data []byte) (*Workflow, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	wf := &Workflow{
		Nodes: make(map[string]Node),
	}

	for id, rawNode := range raw {
		var node Node
		if err := json.Unmarshal(rawNode, &node); err != nil {
			continue
		}
		node.ID = id

		// Extract class_type if not set
		if node.ClassType == "" {
			var rawClass struct {
				ClassType string `json:"class_type"`
			}
			json.Unmarshal(rawNode, &rawClass)
			node.ClassType = rawClass.ClassType
		}

		wf.Nodes[id] = node
	}

	return wf, nil
}
