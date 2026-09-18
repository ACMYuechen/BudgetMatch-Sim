// Package beam provides a bounded, deterministic DEMO-SNAPSHOT search
// experiment. It is not wired to recommendation routes, tools or finalizers.
// A complete result only satisfies supplied constraints in the supplied demo
// snapshots; it neither checks live stock nor claims global optimality.
package beam

import (
	"errors"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

const (
	StrategyVersion  = "demand_beam_v1"
	SnapshotScope    = "synthetic_demo_snapshot_only"
	Complete         = "complete"
	NoFeasibleBundle = "no_feasible_bundle"
	MaxBeamWidth     = 128
	MaxExpansions    = 65536
	MaxTimeBudget    = time.Second
	MaxInputBytes    = 1 << 20
	MaxTagsPerSKU    = 32
	stopFinished     = "enumeration_finished"
	stopTime         = "time_budget"
	stopExpansions   = "expansion_limit"
)

var (
	ErrConfig = errors.New("invalid beam search bounds")
	ErrInput  = errors.New("invalid beam search input")
)

type Config struct {
	MaxCandidates int
	BeamWidth     int
	MaxExpansions int
	TimeBudget    time.Duration
}

func (c Config) valid() bool {
	return c.MaxCandidates > 0 && c.MaxCandidates <= agent.MaxCandidateIDs &&
		c.BeamWidth > 0 && c.BeamWidth <= MaxBeamWidth &&
		c.MaxExpansions > 0 && c.MaxExpansions <= MaxExpansions &&
		c.TimeBudget > 0 && c.TimeBudget <= MaxTimeBudget
}

// Selector is immutable and safe for concurrent calls. The private clock is
// injectable by package tests; production always uses a monotonic time.Now.
type Selector struct {
	config Config
	now    func() time.Time
}

// New fills zero values with conservative defaults, rejecting negative and
// oversized bounds. TimeBudget is cooperative, not a hard realtime guarantee.
func New(c Config) (*Selector, error) {
	if c.MaxCandidates == 0 {
		c.MaxCandidates = 64
	}
	if c.BeamWidth == 0 {
		c.BeamWidth = 32
	}
	if c.MaxExpansions == 0 {
		c.MaxExpansions = 8192
	}
	if c.TimeBudget == 0 {
		c.TimeBudget = 100 * time.Millisecond
	}
	if !c.valid() {
		return nil, ErrConfig
	}
	return &Selector{config: c, now: time.Now}, nil
}
