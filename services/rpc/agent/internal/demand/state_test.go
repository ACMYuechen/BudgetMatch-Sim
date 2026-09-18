package demand

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"github.com/stretchr/testify/require"
)

func initialState(t *testing.T) State {
	t.Helper()
	s, err := NewState(agent.Constraints{BudgetCents: 1000, MaxItems: 3})
	require.NoError(t, err)
	return s
}

func patchFrom(t *testing.T, raw string) Patch {
	t.Helper()
	p, err := DecodePatch(strings.NewReader(raw))
	require.NoError(t, err)
	return p
}

func TestDemandPatchReplaceAddRemoveAndCanonicalState(t *testing.T) {
	s := initialState(t)
	sequence := []struct {
		patch       string
		required    []Category
		preferences []Preference
	}{
		{`{"schema_version":1,"required":{"operation":"replace","values":["mouse","keyboard"]},"preferences":{"operation":"add","values":["quiet","portable"]}}`, []Category{Keyboard, Mouse}, []Preference{"portable", "quiet"}},
		{`{"schema_version":1,"preferences":{"operation":"remove","values":["quiet"]}}`, []Category{Keyboard, Mouse}, []Preference{"portable"}},
		{`{"schema_version":1,"required":{"operation":"remove","values":["mouse"]},"excluded":{"operation":"add","values":["mouse"]}}`, []Category{Keyboard}, []Preference{"portable"}},
		{`{"schema_version":1,"preferences":{"operation":"replace","values":[]}}`, []Category{Keyboard}, []Preference{}},
		{`{"schema_version":1,"required":{"operation":"add","values":["keyboard"]},"preferences":{"operation":"remove","values":["quiet"]}}`, []Category{Keyboard}, []Preference{}},
		{`{"schema_version":1}`, []Category{Keyboard}, []Preference{}},
	}
	for _, tc := range sequence {
		p := patchFrom(t, tc.patch)
		before, err := json.Marshal(s)
		require.NoError(t, err)
		out, err := Apply(s, p)
		require.NoError(t, err)
		require.Equal(t, "ready", out.Status)
		require.Empty(t, out.Conflicts)
		require.Equal(t, tc.required, out.State.Required)
		require.Equal(t, tc.preferences, out.State.Preferences)
		require.NoError(t, out.State.Validate())
		after, err := json.Marshal(s)
		require.NoError(t, err)
		require.Equal(t, before, after, "Apply mutated its input")
		again, err := Apply(out.State, p)
		require.NoError(t, err)
		require.Equal(t, out, again, "set operations must be idempotent")
		s = out.State
	}
	require.Equal(t, []Category{Mouse}, s.Excluded)
	cleared, err := Apply(s, patchFrom(t, `{"schema_version":1,"excluded":{"operation":"replace","values":[]},"budget_cents":500,"max_items":1}`))
	require.NoError(t, err)
	require.Empty(t, cleared.State.Excluded)
	require.EqualValues(t, 500, cleared.State.BudgetCents)
	require.EqualValues(t, 1, cleared.State.MaxItems)
}

func TestDemandConflictsKeepTheWholeActiveState(t *testing.T) {
	base := initialState(t)
	base.Required = []Category{Keyboard}
	base.Preferences = []Preference{"quiet"}
	for _, tc := range []struct{ patch, code string }{
		{`"excluded":{"operation":"add","values":["keyboard"]}`, "required_and_excluded"},
		{`"optional":{"operation":"add","values":["keyboard"]}`, "required_and_optional"},
		{`"optional":{"operation":"add","values":["mouse"]},"excluded":{"operation":"add","values":["mouse"]}`, "optional_and_excluded"},
		{`"max_items":1,"required":{"operation":"add","values":["mouse"]}`, "required_categories_exceed_item_limit"},
	} {
		p := patchFrom(t, `{"schema_version":1,"budget_cents":500,"preferences":{"operation":"replace","values":[]},`+tc.patch+`}`)
		out, err := Apply(base, p)
		require.NoError(t, err)
		require.Equal(t, "needs_clarification", out.Status)
		require.Equal(t, base, out.State, "budget/preferences must not be partly committed")
		require.Len(t, out.Conflicts, 1)
		require.Equal(t, tc.code, out.Conflicts[0].Code)
	}
}

func TestDemandCopiesInputsAndSeparatesConversations(t *testing.T) {
	base := initialState(t)
	base.Required = []Category{Keyboard}
	p := patchFrom(t, `{"schema_version":1,"preferences":{"operation":"add","values":["quiet"]}}`)
	want, err := Apply(base, p)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				got, err := Apply(base, p)
				if err != nil || !reflect.DeepEqual(want, got) {
					t.Error("concurrent transition changed")
					return
				}
				got.State.Required[0] = Mouse
				got.State.Preferences[0] = "portable"
			}
		}()
	}
	wg.Wait()
	require.Equal(t, []Category{Keyboard}, base.Required)
	require.Equal(t, []Preference{"quiet"}, p.Preferences.Values)
	conflict := patchFrom(t, `{"schema_version":1,"excluded":{"operation":"add","values":["keyboard"]}}`)
	out, err := Apply(base, conflict)
	require.NoError(t, err)
	out.State.Required[0] = Mouse
	require.Equal(t, []Category{Keyboard}, base.Required)
}

func TestDemandRejectsMalformedOrUnboundedProposals(t *testing.T) {
	for _, raw := range []string{
		`{}`, `null`, `[]`, `{"schema_version":2}`, `{"schema_version":1} {}`,
		`{"schema_version":1,"budget_cents":null}`, `{"schema_version":1,"budget_cents":0}`,
		`{"schema_version":1,"budget_cents":-1}`, `{"schema_version":1,"budget_cents":100000000001}`,
		`{"schema_version":1,"max_items":0}`, `{"schema_version":1,"max_items":11}`,
		`{"schema_version":1,"required":null}`, `{"schema_version":1,"required":{"operation":"add"}}`,
		`{"schema_version":1,"required":{"operation":"add","values":[]}}`,
		`{"schema_version":1,"required":{"operation":"merge","values":["keyboard"]}}`,
		`{"schema_version":1,"required":{"operation":"replace","values":["unknown"]}}`,
		`{"schema_version":1,"required":{"operation":"replace","values":["keyboard","keyboard"]}}`,
		`{"schema_version":1,"required":{"operation":"replace","values":["PRIVATE"]}}`,
		`{"schema_version":1,"preferences":{"operation":"replace","values":["PRIVATE"]}}`,
		`{"schema_version":1,"required":{"operation":"replace","values":["keyboard"],"min_count":2}}`,
		`{"schema_version":1,"required":{"operation":"replace","values":["keyboard"]},"category":"PRIVATE"}`,
		`{"schema_version":1,"max_items":1,"max_items":2}`,
		`{"schema_version":1,"max_items":1,"MAX_ITEMS":2}`,
		`{"schema_version":1,"max_items":1,"\u006dax_items":2}`,
		`{"schema_version":1,"required":{"operation":"add","operation":"remove","values":["mouse"]}}`,
		`{"schema_version":1,"Schema_version":1}`, string([]byte{'"', 255, '"'}),
		strings.Repeat("[", 10) + "0" + strings.Repeat("]", 10), strings.Repeat(" ", MaxPatchBytes+1),
	} {
		_, err := DecodePatch(strings.NewReader(raw))
		require.ErrorIs(t, err, ErrInvalid, "%s", raw)
		require.NotContains(t, err.Error(), "PRIVATE")
	}
	_, err := DecodePatch(privateErrorReader{})
	require.ErrorIs(t, err, ErrInvalid)
	require.NotContains(t, err.Error(), "PRIVATE")
	base := initialState(t)
	for _, mutate := range []func(*State){
		func(s *State) { s.SchemaVersion = 0 },
		func(s *State) { s.BudgetCents = 0 },
		func(s *State) { s.MaxItems = 0 },
		func(s *State) { s.Required = []Category{Unknown} },
		func(s *State) { s.Required = []Category{Keyboard, Keyboard} },
		func(s *State) { s.Preferences = make([]Preference, MaxTerms+1) },
		func(s *State) { s.Required = []Category{Keyboard}; s.Excluded = []Category{Keyboard} },
	} {
		bad := base
		mutate(&bad)
		_, err := Apply(bad, Patch{SchemaVersion: 1})
		require.ErrorIs(t, err, ErrInvalid)
	}
	// Typed construction must obey the same rules as decoded proposals.
	_, err = Apply(base, Patch{SchemaVersion: 1, Required: &Change[Category]{Operation: Add, Values: []Category{Unknown}}})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = NewState(agent.Constraints{})
	require.ErrorIs(t, err, ErrInvalid)
}

type privateErrorReader struct{}

func (privateErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("PRIVATE reader diagnostic")
}

func FuzzDemandPatchTransition(f *testing.F) {
	for _, seed := range []string{
		`{"schema_version":1}`, `{"schema_version":1,"required":{"operation":"add","values":["mouse"]}}`,
		`{"schema_version":1,"excluded":{"operation":"add","values":["keyboard"]}}`, `null`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		p, err := DecodePatch(strings.NewReader(raw))
		if err != nil {
			require.ErrorIs(t, err, ErrInvalid)
			return
		}
		base := initialState(t)
		base.Required = []Category{Keyboard}
		out, err := Apply(base, p)
		require.NoError(t, err)
		require.NoError(t, out.State.Validate())
		if out.Status == "needs_clarification" {
			require.Equal(t, base, out.State)
			require.NotEmpty(t, out.Conflicts)
		} else {
			require.Equal(t, "ready", out.Status)
			again, err := Apply(out.State, p)
			require.NoError(t, err)
			require.Equal(t, out, again)
		}
		require.Equal(t, []Category{Keyboard}, base.Required)
	})
}
