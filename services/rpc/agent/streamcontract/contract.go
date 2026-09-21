// Package streamcontract defines the additive v1 recommendation stream limits.
// These are local resource bounds, not provider billing or heap-size estimates.
package streamcontract

import "time"

const (
	Version     = 1
	MaxDuration = 30 * time.Second

	Accepted      = "request.accepted"
	Final         = "recommendation.final"
	Error         = "error"
	Done          = "done"
	AnswerDelta   = "answer.delta"
	ToolStarted   = "tool.started"
	ToolCompleted = "tool.completed"

	MaxProgressEvents  = 256
	MaxProgressBytes   = 64 << 10
	MaxDeltaBytes      = 2 << 10
	MaxAnswerBytes     = 16 << 10
	MaxModelChunks     = 2048
	MaxModelBytes      = 256 << 10
	MaxModelCalls      = 9        // <= 8 orchestration calls plus one tool-free explanation
	MaxEstimatedTokens = 64 << 10 // whole-attempt estimated input + reserved output, not billed usage
	MaxToolCalls       = 32

	MaxEventBytes = 256 << 10 // each gateway RPC message / complete SSE frame, including final
	MaxHTTPBytes  = 1 << 20   // total serialized SSE bytes per attempt
)
