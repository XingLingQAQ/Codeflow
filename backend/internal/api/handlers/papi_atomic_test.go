package handlers

// HTTP end-to-end coverage for the PAPI mutation pipeline (E-09) and the
// category-conflict rule (E-10), per §28 T13.03.c: durable faults and legacy
// rows are injected as real SQLite artifacts in the config database file, and
// assertions go through the actual PAPI handlers — create/update/delete/
// hotswap/Get/resolve/conflicts — including close/reopen reconciliation.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/config"
	"github.com/codeflow/backend/internal/dbx"
)

// setupPAPIHTTP installs a fresh SQLite-backed config service as the global
// service and returns a router wiring the real PAPI handlers.
func setupPAPIHTTP(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "papi_http.db")
	svc, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("create config service: %v", err)
	}
	prev := config.GetConfigService()
	config.SetConfigService(svc)
	t.Cleanup(func() {
		if cur, ok := config.GetConfigService().(*config.SQLiteConfigService); ok {
			_ = cur.Close()
		}
		config.SetConfigService(prev)
	})

	r := gin.New()
	r.GET("/api/v1/config/papi", GetPAPIVariables)
	r.GET("/api/v1/config/papi/conflicts", DetectPAPIConflicts)
	r.GET("/api/v1/config/papi/:name", GetPAPIVariable)
	r.POST("/api/v1/config/papi", CreatePAPIVariable)
	r.PUT("/api/v1/config/papi/:name", UpdatePAPIVariable)
	r.DELETE("/api/v1/config/papi/:name", DeletePAPIVariable)
	r.POST("/api/v1/config/papi/resolve", ResolvePAPIByCategory)
	r.POST("/api/v1/config/papi/hotswap", HotSwapPAPI)
	return r, dbPath
}

// reopenPAPIService closes the live service and reopens it from the same
// database file, so the served state is provably the persisted state.
func reopenPAPIService(t *testing.T, dbPath string) {
	t.Helper()
	if cur, ok := config.GetConfigService().(*config.SQLiteConfigService); ok {
		if err := cur.Close(); err != nil {
			t.Fatalf("close config service: %v", err)
		}
	}
	svc, err := config.NewSQLiteConfigService(dbPath)
	if err != nil {
		t.Fatalf("reopen config service: %v", err)
	}
	config.SetConfigService(svc)
}

// withPAPIRawDB closes the live service, runs fn against a raw connection to
// the same database file (trigger install/removal, legacy row seeding), then
// reopens the service so the load path consumes the durable artifact.
func withPAPIRawDB(t *testing.T, dbPath string, fn func(db *sql.DB)) {
	t.Helper()
	if cur, ok := config.GetConfigService().(*config.SQLiteConfigService); ok {
		if err := cur.Close(); err != nil {
			t.Fatalf("close config service: %v", err)
		}
	}
	raw, err := dbx.Open(dbPath)
	if err != nil {
		t.Fatalf("open raw config db: %v", err)
	}
	fn(raw)
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw config db: %v", err)
	}
	reopenPAPIService(t, dbPath)
}

func papiCall(t *testing.T, r http.Handler, method, target string, body interface{}, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = rexMustJSON(t, body)
	}
	w := rexRequest(t, r, method, target, raw, nil)
	if w.Code != wantStatus {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, target, w.Code, wantStatus, w.Body.String())
	}
	return w
}

func papiListHTTP(t *testing.T, r http.Handler) []config.PAPIVariable {
	t.Helper()
	w := papiCall(t, r, http.MethodGet, "/api/v1/config/papi", nil, http.StatusOK)
	var out struct {
		Variables []config.PAPIVariable `json:"variables"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v body=%s", err, w.Body.String())
	}
	return out.Variables
}

func papiGetHTTP(t *testing.T, r http.Handler, name string) config.PAPIVariable {
	t.Helper()
	w := papiCall(t, r, http.MethodGet, "/api/v1/config/papi/"+name, nil, http.StatusOK)
	var v config.PAPIVariable
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode variable %s: %v body=%s", name, err, w.Body.String())
	}
	return v
}

// TestPAPIPersistFailureKeepsVisibleSnapshot injects a real SQLite fault at
// the snapshot-replace INSERT through the HTTP handlers: create/update/
// hotswap/delete all fail 5xx, the visible variables and the persisted rows
// stay exactly as they were (verified across a close/reopen), and the service
// recovers once the fault is cleared (E-09).
func TestPAPIPersistFailureKeepsVisibleSnapshot(t *testing.T) {
	router, dbPath := setupPAPIHTTP(t)

	papiCall(t, router, http.MethodPost, "/api/v1/config/papi", config.PAPIVariable{
		Name: "BASE_VAR", Model: "m1", Category: []string{"backend"},
	}, http.StatusCreated)
	papiCall(t, router, http.MethodPost, "/api/v1/config/papi", config.PAPIVariable{
		Name: "KEEP_VAR", Model: "k1", Category: []string{"docs"},
	}, http.StatusCreated)

	withPAPIRawDB(t, dbPath, func(db *sql.DB) {
		if _, err := db.Exec(`
CREATE TRIGGER fail_papi_insert_http
BEFORE INSERT ON papi_variables
BEGIN SELECT RAISE(ABORT, 'injected papi write failure'); END;`); err != nil {
			t.Fatalf("install trigger: %v", err)
		}
	})

	assertOriginalVisible := func() {
		t.Helper()
		if got := papiGetHTTP(t, router, "BASE_VAR"); got.Model != "m1" {
			t.Fatalf("BASE_VAR model = %q, want m1 (failed writes must not leak)", got.Model)
		}
		if vars := papiListHTTP(t, router); len(vars) != 2 {
			t.Fatalf("visible variables = %d, want 2", len(vars))
		}
	}

	// Every mutation verb fails 5xx and changes nothing.
	papiCall(t, router, http.MethodPost, "/api/v1/config/papi", config.PAPIVariable{
		Name: "NEW_VAR", Model: "m9", Category: []string{"other"},
	}, http.StatusInternalServerError)
	papiCall(t, router, http.MethodGet, "/api/v1/config/papi/NEW_VAR", nil, http.StatusNotFound)

	papiCall(t, router, http.MethodPut, "/api/v1/config/papi/BASE_VAR", config.PAPIVariable{
		Name: "BASE_VAR", Model: "m2", Category: []string{"backend"},
	}, http.StatusInternalServerError)

	papiCall(t, router, http.MethodPost, "/api/v1/config/papi/hotswap", map[string]interface{}{
		"variable_name": "BASE_VAR",
		"new_variable":  map[string]interface{}{"model": "m9", "category": []string{"backend"}},
	}, http.StatusInternalServerError)

	papiCall(t, router, http.MethodDelete, "/api/v1/config/papi/BASE_VAR", nil, http.StatusInternalServerError)
	assertOriginalVisible()

	// The persisted file still holds exactly the original snapshot.
	reopenPAPIService(t, dbPath)
	assertOriginalVisible()

	// Clearing the fault lets the same write commit through the same path.
	withPAPIRawDB(t, dbPath, func(db *sql.DB) {
		if _, err := db.Exec("DROP TRIGGER IF EXISTS fail_papi_insert_http"); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	})
	papiCall(t, router, http.MethodPut, "/api/v1/config/papi/BASE_VAR", config.PAPIVariable{
		Name: "BASE_VAR", Model: "m2", Category: []string{"backend"},
	}, http.StatusOK)
	if got := papiGetHTTP(t, router, "BASE_VAR"); got.Model != "m2" {
		t.Fatalf("post-recovery model = %q, want m2", got.Model)
	}
	reopenPAPIService(t, dbPath)
	if got := papiGetHTTP(t, router, "BASE_VAR"); got.Model != "m2" {
		t.Fatalf("reopened model = %q, want m2", got.Model)
	}
}

// TestConcurrentPAPIWritesMatchReopenedState runs interleaved create and
// hot-swap traffic through the HTTP handlers concurrently; once all writers
// finish, the visible snapshot equals what a freshly reopened service serves
// from the same database file (E-09).
func TestConcurrentPAPIWritesMatchReopenedState(t *testing.T) {
	router, dbPath := setupPAPIHTTP(t)

	papiCall(t, router, http.MethodPost, "/api/v1/config/papi", config.PAPIVariable{
		Name: "BASE", Model: "m0", Category: []string{"base"},
	}, http.StatusCreated)

	const iterations = 20
	var wg sync.WaitGroup
	codes := make([]int, 2*iterations)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			w := rexRequest(t, router, http.MethodPost, "/api/v1/config/papi", rexMustJSON(t, config.PAPIVariable{
				Name:     fmt.Sprintf("G1_VAR_%02d", i),
				Model:    fmt.Sprintf("g1-model-%d", i),
				Category: []string{fmt.Sprintf("g1cat-%d", i)},
			}), nil)
			codes[i] = w.Code
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			w := rexRequest(t, router, http.MethodPost, "/api/v1/config/papi/hotswap", rexMustJSON(t, map[string]interface{}{
				"variable_name": "BASE",
				"new_variable": map[string]interface{}{
					"model":    fmt.Sprintf("base-model-%d", i),
					"category": []string{"base"},
				},
			}), nil)
			codes[iterations+i] = w.Code
		}
	}()
	wg.Wait()
	for i, code := range codes {
		want := http.StatusCreated
		if i >= iterations {
			want = http.StatusOK
		}
		if code != want {
			t.Fatalf("request %d: status=%d want=%d", i, code, want)
		}
	}

	snapshot := func() map[string]config.PAPIVariable {
		t.Helper()
		out := make(map[string]config.PAPIVariable)
		for _, v := range papiListHTTP(t, router) {
			out[v.Name] = v
		}
		return out
	}

	beforeClose := snapshot()
	if len(beforeClose) != iterations+1 {
		t.Fatalf("visible variables = %d, want %d", len(beforeClose), iterations+1)
	}

	reopenPAPIService(t, dbPath)
	afterReopen := snapshot()
	if len(afterReopen) != len(beforeClose) {
		t.Fatalf("reopened variables = %d, want %d", len(afterReopen), len(beforeClose))
	}
	names := make([]string, 0, len(beforeClose))
	for name := range beforeClose {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		before, after := beforeClose[name], afterReopen[name]
		if before.Model != after.Model {
			t.Fatalf("variable %s model: before-close %q, after-reopen %q", name, before.Model, after.Model)
		}
		if fmt.Sprint(before.Category) != fmt.Sprint(after.Category) {
			t.Fatalf("variable %s category: before-close %v, after-reopen %v", name, before.Category, after.Category)
		}
		if fmt.Sprint(before.MCPTools) != fmt.Sprint(after.MCPTools) {
			t.Fatalf("variable %s tools: before-close %v, after-reopen %v", name, before.MCPTools, after.MCPTools)
		}
	}
	// Whichever hot swap won the serialization is the one that persisted.
	if got := beforeClose["BASE"].Model; len(got) < len("base-model-") || got[:len("base-model-")] != "base-model-" {
		t.Fatalf("BASE model = %q, want a base-model-* winner", got)
	}
}

// TestPAPICaseFoldConflictIsDeterministic exercises the E-10 conflict rule
// through the HTTP handlers: trim/case/duplicate differences within one
// variable normalize to a single claim, a second claimant of the same
// normalized category is a deterministic 409 that writes nothing, and a
// legacy persisted conflict resolves as a deterministic 409 naming every
// claimant instead of a random pick — with the conflict and diagnostics
// visible through the conflicts endpoint.
func TestPAPICaseFoldConflictIsDeterministic(t *testing.T) {
	router, dbPath := setupPAPIHTTP(t)

	papiCall(t, router, http.MethodPost, "/api/v1/config/papi", config.PAPIVariable{
		Name: "AAA_FRONT", Model: "m1", Category: []string{"Backend"},
	}, http.StatusCreated)

	// A second claimant of the same normalized category is rejected with a
	// deterministic 409 — same status, same body — and stays invisible.
	var firstBody string
	for i := 0; i < 50; i++ {
		w := papiCall(t, router, http.MethodPost, "/api/v1/config/papi", config.PAPIVariable{
			Name: "ZZZ_BACK", Model: "m2", Category: []string{" backend "},
		}, http.StatusConflict)
		if i == 0 {
			var env rexEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode conflict envelope: %v body=%s", err, w.Body.String())
			}
			if env.Success {
				t.Fatalf("conflict envelope success=true body=%s", w.Body.String())
			}
			var data struct {
				Category  string   `json:"category"`
				Variables []string `json:"variables"`
			}
			if err := json.Unmarshal(env.Data, &data); err != nil {
				t.Fatalf("decode conflict data: %v body=%s", err, w.Body.String())
			}
			if data.Category != "backend" {
				t.Fatalf("conflict category = %q, want backend", data.Category)
			}
			if fmt.Sprint(data.Variables) != fmt.Sprint([]string{"AAA_FRONT", "ZZZ_BACK"}) {
				t.Fatalf("conflict variables = %v, want [AAA_FRONT ZZZ_BACK]", data.Variables)
			}
			firstBody = w.Body.String()
		} else if w.Body.String() != firstBody {
			t.Fatalf("iteration %d: conflict response not deterministic:\n%s\nvs\n%s", i, w.Body.String(), firstBody)
		}
	}
	papiCall(t, router, http.MethodGet, "/api/v1/config/papi/ZZZ_BACK", nil, http.StatusNotFound)
	if vars := papiListHTTP(t, router); len(vars) != 1 {
		t.Fatalf("variables after rejected writes = %d, want 1", len(vars))
	}

	// The sole claimant still resolves, case- and trim-insensitively.
	w := papiCall(t, router, http.MethodPost, "/api/v1/config/papi/resolve",
		map[string]string{"category": " BACKEND "}, http.StatusOK)
	var resolved config.PAPIVariable
	if err := json.Unmarshal(w.Body.Bytes(), &resolved); err != nil {
		t.Fatalf("decode resolved variable: %v body=%s", err, w.Body.String())
	}
	if resolved.Name != "AAA_FRONT" {
		t.Fatalf("resolved = %q, want AAA_FRONT", resolved.Name)
	}

	// Trim/case/duplicate labels within one variable persist normalized.
	papiCall(t, router, http.MethodPost, "/api/v1/config/papi", config.PAPIVariable{
		Name: "DUP_VAR", Model: "m3", Category: []string{" Docs ", "DOCS", "docs", ""},
	}, http.StatusCreated)
	if got := papiGetHTTP(t, router, "DUP_VAR"); fmt.Sprint(got.Category) != fmt.Sprint([]string{"docs"}) {
		t.Fatalf("normalized category = %v, want [docs]", got.Category)
	}

	// Legacy rows from before the rule existed seed a real persisted conflict.
	withPAPIRawDB(t, dbPath, func(db *sql.DB) {
		seed := func(v config.PAPIVariable) {
			payload, err := json.Marshal(&v)
			if err != nil {
				t.Fatalf("marshal seed: %v", err)
			}
			if _, err := db.Exec("INSERT INTO papi_variables (name, variable_json) VALUES (?, ?)", v.Name, string(payload)); err != nil {
				t.Fatalf("seed %s: %v", v.Name, err)
			}
		}
		seed(config.PAPIVariable{Name: "LEGACY_ONE", Model: "l1", Category: []string{"Backend"}})
		seed(config.PAPIVariable{Name: "LEGACY_TWO", Model: "l2", Category: []string{" backend "}})
	})

	// Resolving the conflicted category is a deterministic 409 naming every
	// claimant in sorted order — never a random choice.
	wantClaimants := []string{"AAA_FRONT", "LEGACY_ONE", "LEGACY_TWO"}
	firstBody = ""
	for i := 0; i < 50; i++ {
		w := papiCall(t, router, http.MethodPost, "/api/v1/config/papi/resolve",
			map[string]string{"category": "Backend"}, http.StatusConflict)
		if i == 0 {
			var env rexEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode resolve conflict: %v body=%s", err, w.Body.String())
			}
			var data struct {
				Category  string   `json:"category"`
				Variables []string `json:"variables"`
			}
			if err := json.Unmarshal(env.Data, &data); err != nil {
				t.Fatalf("decode resolve conflict data: %v body=%s", err, w.Body.String())
			}
			if data.Category != "backend" || fmt.Sprint(data.Variables) != fmt.Sprint(wantClaimants) {
				t.Fatalf("resolve conflict = %q %v, want backend %v", data.Category, data.Variables, wantClaimants)
			}
			firstBody = w.Body.String()
		} else if w.Body.String() != firstBody {
			t.Fatalf("iteration %d: resolve conflict not deterministic", i)
		}
	}

	// The conflicts endpoint reports the loaded legacy conflict and the
	// diagnostics; the legacy rows stay visible and are not deleted.
	w = papiCall(t, router, http.MethodGet, "/api/v1/config/papi/conflicts", nil, http.StatusOK)
	var conflictsResponse struct {
		Conflicts   []string `json:"conflicts"`
		Diagnostics []string `json:"diagnostics"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &conflictsResponse); err != nil {
		t.Fatalf("decode conflicts: %v body=%s", err, w.Body.String())
	}
	joined := fmt.Sprint(conflictsResponse.Conflicts)
	for _, want := range []string{"backend", "AAA_FRONT", "LEGACY_ONE", "LEGACY_TWO"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("conflicts %v missing %q", conflictsResponse.Conflicts, want)
		}
	}
	joinedDiag := fmt.Sprint(conflictsResponse.Diagnostics)
	for _, want := range []string{"backend", "LEGACY_ONE", "LEGACY_TWO"} {
		if !strings.Contains(joinedDiag, want) {
			t.Fatalf("diagnostics %v missing %q", conflictsResponse.Diagnostics, want)
		}
	}
	if vars := papiListHTTP(t, router); len(vars) != 4 {
		t.Fatalf("variables with legacy rows = %d, want 4 (nothing deleted)", len(vars))
	}

	// A non-conflicted legacy category keeps resolving next to the conflict.
	w = papiCall(t, router, http.MethodPost, "/api/v1/config/papi/resolve",
		map[string]string{"category": "docs"}, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &resolved); err != nil {
		t.Fatalf("decode docs resolve: %v body=%s", err, w.Body.String())
	}
	if resolved.Name != "DUP_VAR" {
		t.Fatalf("docs resolved = %q, want DUP_VAR", resolved.Name)
	}
}
