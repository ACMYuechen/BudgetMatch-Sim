package agent

import "context"

type Input struct {
	Query       string
	BudgetCents int64
	MaxItems    int32
	UserID      string
}

type Intent struct {
	BudgetCents int64
	MaxItems    int32
	Keywords    []string
	Preferences []string
}

type BundleItem struct {
	ID         string
	Name       string
	Category   string
	Source     string
	PriceCents int64
	Stock      int64
	Score      float64
	Reason     string
}

type ToolCall struct {
	Name    string
	Success bool
	Detail  string
}

type Result struct {
	Intent          Intent
	Items           []BundleItem
	TotalPriceCents int64
	Summary         string
	ToolsUsed       []ToolCall
}

type Agent interface {
	Name() string
	Run(ctx context.Context, input Input) (*Result, error)
}
