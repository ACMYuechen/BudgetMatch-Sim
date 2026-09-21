// Package candidatecontract bounds online, user-authorized candidate checks.
package candidatecontract

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxCandidates    = 32
	MaxIDBytes       = 64
	MaxResponseBytes = 64 << 10
	MaxDuration      = 2 * time.Second
)

func ValidID(id string) bool {
	return id != "" && len(id) <= MaxIDBytes && utf8.ValidString(id) &&
		strings.TrimSpace(id) == id && !strings.ContainsRune(id, '\x00')
}

// ValidIDs deliberately rejects duplicates: every requested ID needs one answer.
func ValidIDs(ids []string) bool {
	if len(ids) == 0 || len(ids) > MaxCandidates {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !ValidID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
