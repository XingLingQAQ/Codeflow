// Package config - PAPI category normalization and candidate snapshot tests (T13.03.a)
package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCategory(t *testing.T) {
	t.Run("nil and empty input normalize to a non-nil empty set", func(t *testing.T) {
		got := NormalizeCategory(nil)
		assert.NotNil(t, got)
		assert.Empty(t, got)

		got = NormalizeCategory([]string{})
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("trims whitespace lowercases and drops empty labels", func(t *testing.T) {
		got := NormalizeCategory([]string{"  Backend ", "", "   ", "API"})
		assert.Equal(t, []string{"backend", "api"}, got)
	})

	t.Run("dedups after case folding preserving first occurrence order", func(t *testing.T) {
		got := NormalizeCategory([]string{"Backend", "api", "backend", " API ", "db"})
		assert.Equal(t, []string{"backend", "api", "db"}, got)
	})

	t.Run("output is independent of the input slice", func(t *testing.T) {
		input := []string{"Backend"}
		got := NormalizeCategory(input)
		input[0] = "mutated"
		assert.Equal(t, []string{"backend"}, got)
	})
}

func TestBuildCategoryIndexEmptySet(t *testing.T) {
	index, err := BuildCategoryIndex(nil)
	require.NoError(t, err)
	assert.NotNil(t, index)
	assert.Empty(t, index)

	index, err = BuildCategoryIndex(map[string]*PAPIVariable{})
	require.NoError(t, err)
	assert.NotNil(t, index)
	assert.Empty(t, index)

	// Variables without usable categories contribute nothing and never conflict.
	index, err = BuildCategoryIndex(map[string]*PAPIVariable{
		"A": {Name: "A", Model: "m"},
		"B": {Name: "B", Model: "m", Category: []string{"", "  "}},
		"C": nil,
	})
	require.NoError(t, err)
	assert.Empty(t, index)
}

func TestBuildCategoryIndexUniqueMapping(t *testing.T) {
	index, err := BuildCategoryIndex(map[string]*PAPIVariable{
		"BACKEND_EXPERT":  {Name: "BACKEND_EXPERT", Category: []string{"Backend", "backend", " api "}},
		"FRONTEND_EXPERT": {Name: "FRONTEND_EXPERT", Category: []string{"frontend"}},
	})
	require.NoError(t, err)
	// Duplicate categories within one variable normalize away instead of
	// self-conflicting.
	assert.Equal(t, map[string]string{
		"backend":  "BACKEND_EXPERT",
		"api":      "BACKEND_EXPERT",
		"frontend": "FRONTEND_EXPERT",
	}, index)
}

func TestBuildCategoryIndexCaseFoldConflictIsTypedAndDeterministic(t *testing.T) {
	variables := map[string]*PAPIVariable{
		"VAR_B": {Name: "VAR_B", Category: []string{"Backend"}},
		"VAR_A": {Name: "VAR_A", Category: []string{" backend "}},
	}
	var first string
	// Repeated runs must produce the identical typed conflict despite Go map
	// iteration order being deliberately random.
	for i := 0; i < 50; i++ {
		index, err := BuildCategoryIndex(variables)
		assert.Nil(t, index)
		require.Error(t, err)
		var conflict *CategoryConflictError
		require.True(t, errors.As(err, &conflict), "expected *CategoryConflictError, got %T (%v)", err, err)
		assert.Equal(t, "backend", conflict.Category)
		assert.Equal(t, []string{"VAR_A", "VAR_B"}, conflict.Variables)
		if first == "" {
			first = err.Error()
		} else {
			assert.Equal(t, first, err.Error())
		}
	}
}

func TestBuildCandidateMappingDoesNotModifyLiveMapping(t *testing.T) {
	mgr := NewPAPIManager()
	require.NoError(t, mgr.DefineVariable(&PAPIVariable{
		Name:     "BACKEND_EXPERT",
		Model:    "m1",
		Category: []string{"Backend"},
		MCPTools: []string{"tool1"},
	}))

	candidate, err := mgr.BuildCandidateMapping(func(m *PAPIMapping) error {
		m.Variables["FRONTEND_EXPERT"] = &PAPIVariable{
			Name:     "FRONTEND_EXPERT",
			Model:    "m2",
			Category: []string{" Frontend "},
		}
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, candidate)

	// The candidate carries the mutation with normalized categories.
	require.Len(t, candidate.Variables, 2)
	assert.Equal(t, []string{"frontend"}, candidate.Variables["FRONTEND_EXPERT"].Category)
	assert.Equal(t, []string{"backend"}, candidate.Variables["BACKEND_EXPERT"].Category)

	// Live state is untouched by candidate construction: no new variable and
	// no normalization leaked into the live mapping.
	live, err := mgr.GetVariable("BACKEND_EXPERT")
	require.NoError(t, err)
	assert.Equal(t, []string{"Backend"}, live.Category)
	_, err = mgr.GetVariable("FRONTEND_EXPERT")
	assert.Error(t, err)

	// Mutating the candidate afterwards cannot leak into live state either.
	candidate.Variables["BACKEND_EXPERT"].Category[0] = "mutated"
	candidate.Variables["BACKEND_EXPERT"].MCPTools[0] = "mutated"
	live, err = mgr.GetVariable("BACKEND_EXPERT")
	require.NoError(t, err)
	assert.Equal(t, []string{"Backend"}, live.Category)
	assert.Equal(t, []string{"tool1"}, live.MCPTools)
}

func TestBuildCandidateMappingRejectsConflictWithoutTouchingLive(t *testing.T) {
	mgr := NewPAPIManager()
	require.NoError(t, mgr.DefineVariable(&PAPIVariable{
		Name:     "A",
		Model:    "m",
		Category: []string{"backend"},
	}))

	candidate, err := mgr.BuildCandidateMapping(func(m *PAPIMapping) error {
		m.Variables["B"] = &PAPIVariable{Name: "B", Model: "m", Category: []string{"BACKEND"}}
		return nil
	})
	assert.Nil(t, candidate)
	var conflict *CategoryConflictError
	require.True(t, errors.As(err, &conflict), "expected *CategoryConflictError, got %T (%v)", err, err)
	assert.Equal(t, "backend", conflict.Category)
	assert.Equal(t, []string{"A", "B"}, conflict.Variables)

	// The rejected candidate was never published.
	_, getErr := mgr.GetVariable("B")
	assert.Error(t, getErr)
}

func TestBuildCandidateMappingMutateErrorAbortsCandidate(t *testing.T) {
	mgr := NewPAPIManager()
	boom := errors.New("boom")
	candidate, err := mgr.BuildCandidateMapping(func(m *PAPIMapping) error {
		m.Variables["B"] = &PAPIVariable{Name: "B", Model: "m"}
		return boom
	})
	assert.Nil(t, candidate)
	assert.ErrorIs(t, err, boom)
	_, getErr := mgr.GetVariable("B")
	assert.Error(t, getErr)
}

func TestBuildCandidateMappingEmptyManagerYieldsEmptyCandidate(t *testing.T) {
	mgr := NewPAPIManager()
	candidate, err := mgr.BuildCandidateMapping(nil)
	require.NoError(t, err)
	require.NotNil(t, candidate)
	assert.NotNil(t, candidate.Variables)
	assert.Empty(t, candidate.Variables)
}

// Getters must hand out deep copies: mutating a returned slice never changes
// the live snapshot (E-09).
func TestPAPIManagerGettersReturnDeepCopies(t *testing.T) {
	mgr := NewPAPIManager()
	require.NoError(t, mgr.DefineVariable(&PAPIVariable{
		Name:     "A",
		Model:    "m",
		Category: []string{"backend"},
		MCPTools: []string{"tool1"},
	}))

	mutate := func(v *PAPIVariable) {
		v.Category[0] = "mutated"
		v.MCPTools[0] = "mutated"
	}

	got, err := mgr.GetVariable("A")
	require.NoError(t, err)
	mutate(got)

	listed := mgr.ListVariables()
	require.Len(t, listed, 1)
	mutate(listed[0])

	mapping := mgr.GetMapping()
	mutate(mapping.Variables["A"])

	resolved, err := mgr.ResolveByCategory("backend")
	require.NoError(t, err)
	mutate(resolved)

	fresh, err := mgr.GetVariable("A")
	require.NoError(t, err)
	assert.Equal(t, []string{"backend"}, fresh.Category)
	assert.Equal(t, []string{"tool1"}, fresh.MCPTools)
}
