// Package streamcontract defines the v1 recommendation lifecycle stream limits.
// Model deltas/tool progress and HTTP SSE forwarding are separate milestones.
package streamcontract

import "time"

const (
	Version     = 1
	MaxDuration = 30 * time.Second

	Accepted = "request.accepted"
	Final    = "recommendation.final"
	Error    = "error"
	Done     = "done"
)
