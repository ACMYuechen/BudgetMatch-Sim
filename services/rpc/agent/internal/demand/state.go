// Package demand defines the M4 domain foundation used by planning-only turns.
// Bundle selection does not consume it yet; ready means a valid intent only.
package demand

import (
	"errors"
	"io"
	"slices"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

const (
	SchemaVersion   = 1
	TaxonomyVersion = "demo_taxonomy_v1"
	MaxTerms        = 16
	MaxPatchBytes   = 8 << 10
)

var ErrInvalid = errors.New("invalid demand data")

// This flat demo taxonomy does not imply compatibility, attributes or a
// hierarchy. A required category means at least one distinct SKU in that class.
type Category string

const (
	Unknown     Category = "unknown"
	Keyboard    Category = "keyboard"
	Mouse       Category = "mouse"
	Lighting    Category = "lighting"
	Stationery  Category = "stationery"
	Monitor     Category = "monitor"
	Headphones  Category = "headphones"
	Tablet      Category = "tablet"
	Phone       Category = "phone"
	Computer    Category = "computer"
	Accessories Category = "accessories"
)

func knownCategory(c Category) bool {
	switch c {
	case Keyboard, Mouse, Lighting, Stationery, Monitor, Headphones, Tablet, Phone, Computer, Accessories:
		return true
	}
	return false
}

// Preferences are soft objectives, not inferred product facts. This step does
// not score them or claim that a product satisfies quiet/battery requirements.
type Preference string

func knownPreference(p Preference) bool {
	switch p {
	case "value", "portable", "battery_life", "lightweight", "performance", "durable", "quiet":
		return true
	}
	return false
}

type State struct {
	SchemaVersion int          `json:"schema_version"`
	BudgetCents   int64        `json:"budget_cents"`
	MaxItems      int32        `json:"max_items"`
	Required      []Category   `json:"required"`
	Optional      []Category   `json:"optional"`
	Excluded      []Category   `json:"excluded"`
	Preferences   []Preference `json:"preferences"`
}

type Operation string

const (
	Replace Operation = "replace"
	Add     Operation = "add"
	Remove  Operation = "remove"
)

type Change[T ~string] struct {
	Operation Operation `json:"operation"`
	Values    []T       `json:"values"`
}

// Omission inherits; replace with [] clears; add/remove are idempotent set
// operations. Explicit null and zero limits are not inheritance syntax.
type Patch struct {
	SchemaVersion int                 `json:"schema_version"`
	BudgetCents   *int64              `json:"budget_cents,omitempty"`
	MaxItems      *int32              `json:"max_items,omitempty"`
	Required      *Change[Category]   `json:"required,omitempty"`
	Optional      *Change[Category]   `json:"optional,omitempty"`
	Excluded      *Change[Category]   `json:"excluded,omitempty"`
	Preferences   *Change[Preference] `json:"preferences,omitempty"`
}

type Conflict struct {
	Code     string   `json:"code"`
	Category Category `json:"category,omitempty"`
}

type Transition struct {
	Status    string     `json:"status"` // ready / needs_clarification; NOT a recommendation result
	State     State      `json:"state"`  // Unchanged active state on conflict, never a partly applied patch.
	Conflicts []Conflict `json:"conflicts"`
}

func NewState(limits agent.Constraints) (State, error) {
	s := State{SchemaVersion: SchemaVersion, BudgetCents: limits.BudgetCents, MaxItems: limits.MaxItems}
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	return canonical(s), nil
}

func validValues[T ~string](values []T, known func(T) bool) bool {
	if len(values) > MaxTerms {
		return false
	}
	seen := make(map[T]bool, len(values))
	for _, v := range values {
		if !known(v) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

func (s State) validShape() bool {
	_, err := agent.NewConstraints(agent.Intent{BudgetCents: s.BudgetCents, MaxItems: s.MaxItems})
	return err == nil && s.SchemaVersion == SchemaVersion &&
		validValues(s.Required, knownCategory) && validValues(s.Optional, knownCategory) &&
		validValues(s.Excluded, knownCategory) && validValues(s.Preferences, knownPreference)
}

func (s State) Validate() error {
	if !s.validShape() || len(conflicts(canonical(s))) != 0 {
		return ErrInvalid
	}
	return nil
}

func validChange[T ~string](c *Change[T], known func(T) bool) bool {
	return c == nil || c.Values != nil && validValues(c.Values, known) &&
		(c.Operation == Replace || len(c.Values) > 0 && (c.Operation == Add || c.Operation == Remove))
}

func (p Patch) valid() bool {
	return p.SchemaVersion == SchemaVersion &&
		(p.BudgetCents == nil || *p.BudgetCents > 0 && *p.BudgetCents <= agent.MaxBudgetCents) &&
		(p.MaxItems == nil || *p.MaxItems > 0 && *p.MaxItems <= agent.MaxItems) &&
		validChange(p.Required, knownCategory) && validChange(p.Optional, knownCategory) &&
		validChange(p.Excluded, knownCategory) && validChange(p.Preferences, knownPreference)
}

// DecodePatch validates a proposal's syntax and domain values, not its origin
// or authority. Apply must still check it against the active state.
func DecodePatch(r io.Reader) (Patch, error) {
	var p Patch
	if _, err := decodeBounded(r, MaxPatchBytes, &p); err != nil || !p.valid() {
		return Patch{}, ErrInvalid
	}
	return p, nil
}

func Apply(base State, p Patch) (Transition, error) {
	if base.Validate() != nil || !p.valid() {
		return Transition{}, ErrInvalid
	}
	next := canonical(base)
	if p.BudgetCents != nil {
		next.BudgetCents = *p.BudgetCents
	}
	if p.MaxItems != nil {
		next.MaxItems = *p.MaxItems
	}
	next.Required = applyChange(next.Required, p.Required)
	next.Optional = applyChange(next.Optional, p.Optional)
	next.Excluded = applyChange(next.Excluded, p.Excluded)
	next.Preferences = applyChange(next.Preferences, p.Preferences)
	if !next.validShape() {
		return Transition{}, ErrInvalid
	}
	issues := conflicts(next)
	if len(issues) != 0 {
		return Transition{Status: "needs_clarification", State: canonical(base), Conflicts: issues}, nil
	}
	return Transition{Status: "ready", State: next, Conflicts: issues}, nil
}

func sortedCopy[T ~string](values []T) []T {
	copy := append([]T{}, values...)
	slices.Sort(copy)
	return copy
}

func canonical(s State) State {
	s.Required = sortedCopy(s.Required)
	s.Optional = sortedCopy(s.Optional)
	s.Excluded = sortedCopy(s.Excluded)
	s.Preferences = sortedCopy(s.Preferences)
	return s
}

func applyChange[T ~string](base []T, c *Change[T]) []T {
	if c == nil {
		return base
	}
	if c.Operation == Replace {
		return sortedCopy(c.Values)
	}
	set := make(map[T]bool, len(base)+len(c.Values))
	for _, value := range base {
		set[value] = true
	}
	for _, value := range c.Values {
		if c.Operation == Remove {
			delete(set, value)
		} else {
			set[value] = true
		}
	}
	out := make([]T, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func conflicts(s State) []Conflict {
	out := []Conflict{}
	for _, category := range s.Required {
		if slices.Contains(s.Excluded, category) {
			out = append(out, Conflict{Code: "required_and_excluded", Category: category})
		}
		if slices.Contains(s.Optional, category) {
			out = append(out, Conflict{Code: "required_and_optional", Category: category})
		}
	}
	for _, category := range s.Optional {
		if slices.Contains(s.Excluded, category) {
			out = append(out, Conflict{Code: "optional_and_excluded", Category: category})
		}
	}
	if len(s.Required) > int(s.MaxItems) {
		out = append(out, Conflict{Code: "required_categories_exceed_item_limit"})
	}
	return out
}
