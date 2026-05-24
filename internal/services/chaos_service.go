package services

import (
	"sync"
	"time"
)

// ChaosMode represents a currently active demo chaos scenario
type ChaosMode struct {
	Mode      string    `json:"mode"`
	ExpiresAt time.Time `json:"expiresAt"`
	ExpiresIn int       `json:"expiresIn,omitempty"` // seconds, only in activation response
}

// ChaosService manages demo chaos scenarios for dashboard demos
type ChaosService struct {
	mu      sync.RWMutex
	current *ChaosMode
}

// NewChaosService instantiates a ChaosService
func NewChaosService() *ChaosService {
	return &ChaosService{}
}

// Activate enables a chaos scenario for a given duration (default 60s)
func (s *ChaosService) Activate(scenario string, durationSec int) ChaosMode {
	if durationSec <= 0 {
		durationSec = 60
	}
	mode := ChaosMode{
		Mode:      scenario,
		ExpiresAt: time.Now().Add(time.Duration(durationSec) * time.Second),
		ExpiresIn: durationSec,
	}
	s.mu.Lock()
	s.current = &mode
	s.mu.Unlock()
	return mode
}

// Status returns the current chaos mode if still active, or a "none" state
func (s *ChaosService) Status() ChaosMode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.current == nil || time.Now().After(s.current.ExpiresAt) {
		return ChaosMode{Mode: "none", ExpiresAt: time.Time{}}
	}
	return *s.current
}

// IsActive returns true if a specific scenario is currently running
func (s *ChaosService) IsActive(scenario string) bool {
	status := s.Status()
	return status.Mode == scenario
}
