package agent

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// Backoff implements jittered exponential backoff for reconnection.
// Default parameters: Min=100ms, Max=60s, Factor=2.0, Jitter=±25%.
type Backoff struct {
	Min    time.Duration
	Max    time.Duration
	Factor float64
	Jitter float64 // fraction for ±jitter (0.25 = ±25%)

	attempt int
	mu      sync.Mutex
}

// DefaultBackoff returns a Backoff configured per project spec:
// Min=100ms, Max=60s, Factor=2.0, Jitter=±25%.
func DefaultBackoff() *Backoff {
	return &Backoff{
		Min:    100 * time.Millisecond,
		Max:    60 * time.Second,
		Factor: 2.0,
		Jitter: 0.25,
	}
}

// Duration returns the next backoff duration with jitter applied
// and increments the attempt counter.
func (b *Backoff) Duration() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Base exponential: min * factor^attempt
	d := float64(b.Min) * math.Pow(b.Factor, float64(b.attempt))

	// Cap at max before applying jitter
	if d > float64(b.Max) {
		d = float64(b.Max)
	}

	// Apply jitter: d ± (d * jitter), uniform distribution
	if b.Jitter > 0 {
		jitter := d * b.Jitter * (2*rand.Float64() - 1)
		d += jitter
	}

	// Clamp to [Min, Max]
	if d < float64(b.Min) {
		d = float64(b.Min)
	}
	if d > float64(b.Max) {
		d = float64(b.Max)
	}

	b.attempt++
	return time.Duration(d)
}

// Reset resets the attempt counter to 0. Called on successful connection.
func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempt = 0
}

// Attempt returns the current attempt number.
func (b *Backoff) Attempt() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempt
}
