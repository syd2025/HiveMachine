// Package packer implements multi-model memory packing for GPU providers.
package packer

import (
	"fmt"
	"sort"
	"sync"
)

// MemoryBudget represents available memory.
type MemoryBudget struct {
	TotalMB     int64
	UsedMB      int64
	ReserveMB   int64 // Reserved for OS, other processes
}

// Available returns available memory in MB.
func (b *MemoryBudget) Available() int64 {
	return b.TotalMB - b.UsedMB - b.ReserveMB
}

// CanFit checks if a footprint can fit in available memory.
func (b *MemoryBudget) CanFit(footprint *ModelFootprint) bool {
	return b.Available() >= footprint.MemoryMB
}

// ModelFootprint represents a model's memory requirements.
type ModelFootprint struct {
	ModelID     string
	MemoryMB    int64
	ContextSize int // tokens
	Quantization string // "fp16", "int8", "int4", etc.
}

// Placement represents a placed model.
type Placement struct {
	ModelID  string
	StartMB  int64
	EndMB    int64
	Priority int // Higher = placed first
}

// Packer implements first-fit decreasing bin packing.
type Packer struct {
	budget    *MemoryBudget
	placements []*Placement
	mu        sync.RWMutex
}

// NewPacker creates a new packer with the given budget.
func NewPacker(budget *MemoryBudget) *Packer {
	if budget == nil {
		budget = &MemoryBudget{ReserveMB: 2048} // 2GB reserve by default
	}
	return &Packer{
		budget:     budget,
		placements: make([]*Placement, 0),
	}
}

// SetBudget updates the memory budget.
func (p *Packer) SetBudget(budget *MemoryBudget) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.budget = budget
}

// CanFit checks if a model can fit.
func (p *Packer) CanFit(footprint *ModelFootprint) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.budget.CanFit(footprint)
}

// CurrentUsage returns current memory usage.
func (p *Packer) CurrentUsage() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.budget.UsedMB
}

// AvailableMemory returns available memory.
func (p *Packer) AvailableMemory() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.budget.Available()
}

// Placements returns current model placements.
func (p *Packer) Placements() []*Placement {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*Placement, len(p.placements))
	copy(result, p.placements)
	return result
}

// Place attempts to place a model in memory.
func (p *Packer) Place(footprint *ModelFootprint) (*Placement, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.budget.CanFit(footprint) {
		return nil, fmt.Errorf("model %s requires %dMB, only %dMB available",
			footprint.ModelID, footprint.MemoryMB, p.budget.Available())
	}

	// First-fit decreasing: place in first gap that fits
	sort.Slice(p.placements, func(i, j int) bool {
		return p.placements[i].StartMB < p.placements[j].StartMB
	})

	var startMB int64 = 0
	for _, pl := range p.placements {
		gap := pl.StartMB - startMB
		if gap >= footprint.MemoryMB {
			placement := &Placement{
				ModelID: footprint.ModelID,
				StartMB: startMB,
				EndMB:   startMB + footprint.MemoryMB,
				Priority: int(footprint.MemoryMB),
			}
			p.placements = append(p.placements, placement)
			p.budget.UsedMB += footprint.MemoryMB
			return placement, nil
		}
		startMB = pl.EndMB
	}

	// Place at the end
	if p.budget.Available() < footprint.MemoryMB {
		return nil, fmt.Errorf("cannot fit model %s", footprint.ModelID)
	}

	placement := &Placement{
		ModelID: footprint.ModelID,
		StartMB: startMB,
		EndMB:   startMB + footprint.MemoryMB,
		Priority: int(footprint.MemoryMB),
	}
	p.placements = append(p.placements, placement)
	p.budget.UsedMB += footprint.MemoryMB

	return placement, nil
}

// Remove removes a model from memory.
func (p *Packer) Remove(modelID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, pl := range p.placements {
		if pl.ModelID == modelID {
			p.budget.UsedMB -= (pl.EndMB - pl.StartMB)
			p.placements = append(p.placements[:i], p.placements[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("model %s not found in packer", modelID)
}

// Optimize rearranges placements to minimize fragmentation.
func (p *Packer) Optimize() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Simple optimization: sort by startMB and recalculate
	sort.Slice(p.placements, func(i, j int) bool {
		return p.placements[i].StartMB < p.placements[j].StartMB
	})

	var currentMB int64 = 0
	for _, pl := range p.placements {
		pl.StartMB = currentMB
		pl.EndMB = currentMB + (pl.EndMB - pl.StartMB)
		currentMB = pl.EndMB
	}

	return nil
}

// OptimalPlacement finds the best placement for a set of models.
func (p *Packer) OptimalPlacement(footprints []*ModelFootprint) ([]*Placement, error) {
	// Sort by memory size descending (FFD algorithm)
	sorted := make([]*ModelFootprint, len(footprints))
	copy(sorted, footprints)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MemoryMB > sorted[j].MemoryMB
	})

	// Create a copy of the packer state for trying
	testBudget := &MemoryBudget{
		TotalMB:   p.budget.TotalMB,
		UsedMB:    p.budget.UsedMB,
		ReserveMB: p.budget.ReserveMB,
	}

	var placements []*Placement
	for _, fp := range sorted {
		// Find first fit
		var placed bool
		for i, pl := range placements {
			gap := pl.StartMB - (func() int64 {
				if i == 0 {
					return 0
				}
				return placements[i-1].EndMB
			}())
			if i > 0 {
				gap = pl.StartMB - placements[i-1].EndMB
			}
			if gap >= fp.MemoryMB {
				placement := &Placement{
					ModelID:  fp.ModelID,
					StartMB:  placements[i-1].EndMB,
					EndMB:    placements[i-1].EndMB + fp.MemoryMB,
					Priority: int(fp.MemoryMB),
				}
				placements = append(placements, placement)
				testBudget.UsedMB += fp.MemoryMB
				placed = true
				break
			}
		}

		if !placed {
			// Try placing at end
			var endMB int64 = 0
			for _, pl := range placements {
				if pl.EndMB > endMB {
					endMB = pl.EndMB
				}
			}
			if testBudget.Available() >= fp.MemoryMB {
				placement := &Placement{
					ModelID:  fp.ModelID,
					StartMB:  endMB,
					EndMB:    endMB + fp.MemoryMB,
					Priority: int(fp.MemoryMB),
				}
				placements = append(placements, placement)
				testBudget.UsedMB += fp.MemoryMB
			} else {
				return nil, fmt.Errorf("cannot fit all models: %s requires %dMB",
					fp.ModelID, fp.MemoryMB)
			}
		}
	}

	return placements, nil
}

// Dump returns a string representation of current placements.
func (p *Packer) Dump() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := fmt.Sprintf("Memory: %d/%dMB (used/available: %dMB)\n",
		p.budget.UsedMB, p.budget.TotalMB, p.budget.Available())

	for _, pl := range p.placements {
		result += fmt.Sprintf("  %s: %d-%dMB\n", pl.ModelID, pl.StartMB, pl.EndMB)
	}

	return result
}
