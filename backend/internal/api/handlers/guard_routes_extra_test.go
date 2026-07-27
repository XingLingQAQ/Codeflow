package handlers

// HTTP handler-level coverage for guard policy routes: a dry-run POST /check on
// a stacked-naming path returns a blocking violation without touching disk, and
// GET /rules enumerates known rule ids with severities (including duplicate_symbol).
// A fresh *guard.Engine is swapped in so the concrete dry-run path is exercised.

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/guard"
)

func rexGuardRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev := guard.GetService()
	guard.SetService(guard.NewEngine(nil, nil))
	t.Cleanup(func() { guard.SetService(prev) })

	r := gin.New()
	r.POST("/api/v1/guard/check", GuardCheck)
	r.GET("/api/v1/guard/rules", GuardRules)
	return r
}

func TestGuardCheckStackedNamingRoutesExtra(t *testing.T) {
	r := rexGuardRouter(t)

	// "service_v2.go" trips the stacked-naming rule (error severity). Evaluate is
	// a pure dry-run: no file is created and no path is written.
	body := rexMustJSON(t, map[string]string{
		"path":         "service_v2.go",
		"content_text": "package x\n",
	})
	w := rexRequest(t, r, http.MethodPost, "/api/v1/guard/check", body, nil)
	var dec struct {
		Allowed    bool `json:"allowed"`
		Violations []struct {
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
		} `json:"violations"`
	}
	rexData(t, w, http.StatusOK, true, &dec)
	if dec.Allowed {
		t.Fatalf("expected allowed=false for stacked naming, body=%s", w.Body.String())
	}
	found := false
	for _, v := range dec.Violations {
		if v.Rule == string(guard.RuleStackedNaming) {
			found = true
			if v.Severity != string(guard.SeverityError) {
				t.Fatalf("stacked_naming severity=%q want=error", v.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("stacked_naming violation not present: %+v", dec.Violations)
	}

	// A clean path passes the same dry-run.
	body = rexMustJSON(t, map[string]string{"path": "service.go", "content_text": "package x\n"})
	w = rexRequest(t, r, http.MethodPost, "/api/v1/guard/check", body, nil)
	dec.Allowed = false
	dec.Violations = nil
	rexData(t, w, http.StatusOK, true, &dec)
	if !dec.Allowed {
		t.Fatalf("clean path expected allowed=true, body=%s", w.Body.String())
	}
}

func TestGuardRulesIncludeDuplicateSymbolRoutesExtra(t *testing.T) {
	r := rexGuardRouter(t)

	w := rexRequest(t, r, http.MethodGet, "/api/v1/guard/rules", nil, nil)
	var out struct {
		Items []struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
		} `json:"items"`
		Total           int      `json:"total"`
		DeniedPathGlobs []string `json:"denied_path_globs"`
		MaxFileBytes    int      `json:"max_file_bytes"`
	}
	rexData(t, w, http.StatusOK, true, &out)
	if out.Total == 0 || len(out.Items) == 0 {
		t.Fatalf("expected non-empty rule list, body=%s", w.Body.String())
	}
	var dupSeverity string
	seen := false
	for _, it := range out.Items {
		if it.ID == string(guard.RuleDuplicateSymbol) {
			seen = true
			dupSeverity = it.Severity
		}
	}
	if !seen {
		t.Fatalf("duplicate_symbol rule missing from %+v", out.Items)
	}
	if dupSeverity != string(guard.SeverityError) {
		t.Fatalf("duplicate_symbol severity=%q want=error", dupSeverity)
	}
	if out.MaxFileBytes <= 0 {
		t.Fatalf("expected max_file_bytes to be reported, got %d", out.MaxFileBytes)
	}
}
