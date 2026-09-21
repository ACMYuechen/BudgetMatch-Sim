// Package indexcontract defines the resource limits shared by Mall snapshot
// producers and Agent consumers. Exceeding a limit fails the entire scan, never
// truncates it into an apparently complete catalog.
package indexcontract

import "time"

const (
	DefaultPageSize  = 100
	MaxPageSize      = 200
	MaxDuration      = 30 * time.Second
	MaxItems         = 50_000
	MaxPageBytes     = 2 << 20
	MaxSnapshotBytes = 32 << 20
)

// Budget counts protobuf payload bytes (not transport overhead or Go heap size).
// Both data and terminal frames must be counted.
type Budget struct {
	Items int
	Bytes int
}

func (b *Budget) Add(items, size int) bool {
	if items < 0 || size < 0 || size > MaxPageBytes || items > MaxItems-b.Items || size > MaxSnapshotBytes-b.Bytes {
		return false
	}
	b.Items += items
	b.Bytes += size
	return true
}
