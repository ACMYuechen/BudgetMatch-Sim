package agent

// RetrievalSource is assigned by trusted providers, never parsed from model text.
type RetrievalSource uint8

const (
	RetrievalUnknown RetrievalSource = iota
	RetrievalMallKeyword
	RetrievalMallVector
	RetrievalDemo
)

type VerificationState uint8

const (
	VerificationUnverified VerificationState = iota
	VerificationChecked
	VerificationDemo
)

// CandidateEvidence is request-local provenance. SnapshotAtUnixMs is the
// observed start of the source read (0 = unknown/legacy), not a row update time.
// VerifiedAtUnixMs is a past check, never a promise or stock reservation.
type CandidateEvidence struct {
	Source            RetrievalSource
	ProductID         string
	Relevance         float64
	HasRelevance      bool
	SnapshotAtUnixMs  int64
	RetrievedAtUnixMs int64
	State             VerificationState
	VerifiedAtUnixMs  int64
}

// SelectionScope preserves the last select_bundle's allowed IDs and tightened
// limits. Repricing must not silently widen the model's selection scope.
type SelectionScope struct {
	CandidateIDs []string
	Limits       Constraints
}
