package mall_order_outbox

const (
	StatusPending = iota
	StatusProcessing
	StatusSent
	StatusDead
)

const DefaultMaxAttempts = 3

func CanTransition(from, to int) bool {
	switch from {
	case StatusPending:
		return to == StatusProcessing
	case StatusProcessing:
		return to == StatusProcessing || to == StatusPending || to == StatusSent || to == StatusDead
	case StatusSent, StatusDead:
		return false
	default:
		return false
	}
}
