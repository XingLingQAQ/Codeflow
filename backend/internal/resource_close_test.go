package internal_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scannedResourceRoots are the trees the lifecycle contract covers, relative to
// this package directory (backend/internal). cmd/ is included because the server
// entrypoint opens several databases of its own; leaving it out meant the
// contract silently exempted the one package that wires them all together.
var scannedResourceRoots = []string{".", "../cmd"}

// TestDatabaseResourcesUseDeferredCloseOrRollback scans every non-test Go file
// under backend/internal and backend/cmd and requires that database resources
// acquired inside a function — *sql.Tx from Begin/BeginTx, *sql.Rows from
// Query/QueryContext and *sql.Stmt from Prepare/PrepareContext — are released on
// every return path.
//
// Per the §26.17 ruling the contract accepts all of these correct patterns
// (the form is relaxed, the substance is not):
//
//  1. deferred release: `defer tx.Rollback()`, `defer rows.Close()`, or a
//     deferred closure such as `defer func() { _ = tx.Rollback() }()`;
//  2. explicit error-path release: `_ = tx.Rollback()` / `rows.Close()` inside
//     the error branch that precedes the return it protects;
//  3. ownership transfer: the resource is passed to a helper as a call
//     argument (e.g. collectDecayItems(rows, ...)) that takes over disposal.
//
// A return that sits after the acquisition (and outside the
// acquisition-error guard) is reported unless a release of the same variable
// is guaranteed to execute first: a deferred release earlier in an enclosing
// block, or an explicit release/transfer in the return's own block chain.
// For transactions, Commit also counts as a release. The check is
// flow-insensitive within straight-line block structure; a release hidden in
// a sibling branch does not count.
func TestDatabaseResourcesUseDeferredCloseOrRollback(t *testing.T) {
	var violations []string

	scanned := 0
	for _, root := range scannedResourceRoots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			fileViolations, err := auditGoResourceLifecycle(filepath.ToSlash(path), content)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			violations = append(violations, fileViolations...)
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s failed: %v", root, err)
		}
	}
	// 空扫描等于契约失效：根目录写错时必须红，而不是静默通过。
	if scanned == 0 {
		t.Fatalf("resource lifecycle scan matched no Go files under %v", scannedResourceRoots)
	}

	if len(violations) > 0 {
		t.Fatalf("database resources must be released on every return path (defer or explicit error-path release, §26.17):\n%s", strings.Join(violations, "\n"))
	}
}

// TestResourceLifecycleAuditFixtures pins the audit rules themselves: the
// accepted patterns from the §26.17 ruling must stay clean, and real leaks
// (a transaction or row set with no release on some return path) must still
// be reported.
func TestResourceLifecycleAuditFixtures(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantClean bool
	}{
		{"deferred rollback", fixtureDeferredRollback, true},
		{"deferred closure rollback", fixtureDeferredClosureRollback, true},
		{"explicit error-path rollback", fixtureExplicitRollback, true},
		{"explicit error-path close", fixtureExplicitClose, true},
		{"ownership transfer to helper", fixtureOwnershipTransfer, true},
		{"transaction without any rollback", fixtureTxLeak, false},
		{"rollback missing on one error path", fixturePartialCoverage, false},
		{"rows never closed", fixtureRowsLeak, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := auditGoResourceLifecycle("fixture.go", []byte(tc.src))
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if tc.wantClean && len(violations) > 0 {
				t.Fatalf("expected clean fixture, got violations:\n%s", strings.Join(violations, "\n"))
			}
			if !tc.wantClean && len(violations) == 0 {
				t.Fatal("expected the leak to be reported, got no violations")
			}
		})
	}
}

const fixtureDeferredRollback = `package p

import "database/sql"

func writeDeferred(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE t SET v = 1"); err != nil {
		return err
	}
	return tx.Commit()
}
`

const fixtureDeferredClosureRollback = `package p

import "database/sql"

func writeClosure(db *sql.DB) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.Exec("UPDATE t SET v = 1"); err != nil {
		return err
	}
	return tx.Commit()
}
`

const fixtureExplicitRollback = `package p

import "database/sql"

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE t SET v = 1"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}
`

const fixtureExplicitClose = `package p

import "database/sql"

func load(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT v FROM t")
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, nil
}
`

const fixtureOwnershipTransfer = `package p

import "database/sql"

func collect(rows *sql.Rows) error {
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

func loadAll(db *sql.DB) error {
	rows, err := db.Query("SELECT v FROM t")
	if err != nil {
		return err
	}
	return collect(rows)
}
`

const fixtureTxLeak = `package p

import "database/sql"

func write(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE t SET v = 1"); err != nil {
		return err
	}
	return tx.Commit()
}
`

const fixturePartialCoverage = `package p

import "database/sql"

func writeTwoSteps(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE t SET v = 1"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec("UPDATE t SET v = 2"); err != nil {
		return err
	}
	return tx.Commit()
}
`

const fixtureRowsLeak = `package p

import "database/sql"

func readAll(db *sql.DB) error {
	rows, err := db.Query("SELECT v FROM t")
	if err != nil {
		return err
	}
	_ = rows
	return nil
}
`

// resourceKind enumerates the tracked database resource kinds.
type resourceKind int

const (
	resourceTx resourceKind = iota
	resourceRows
	resourceStmt
)

func (k resourceKind) String() string {
	switch k {
	case resourceTx:
		return "transaction"
	case resourceRows:
		return "rows"
	default:
		return "statement"
	}
}

// releaseMethods returns the call names that dispose of the resource.
// For transactions a Commit releases the transaction just as a Rollback does.
func (k resourceKind) releaseMethods() map[string]bool {
	if k == resourceTx {
		return map[string]bool{"Rollback": true, "Commit": true}
	}
	return map[string]bool{"Close": true}
}

// acquireMethods maps lower-cased acquiring call names to the resource kind.
// "begintx" additionally matches project helpers such as s.beginTx(ctx) that
// hand a live transaction to their caller. QueryRow is deliberately absent:
// *sql.Row has no Close.
var acquireMethods = map[string]resourceKind{
	"begin":          resourceTx,
	"begintx":        resourceTx,
	"query":          resourceRows,
	"querycontext":   resourceRows,
	"prepare":        resourceStmt,
	"preparecontext": resourceStmt,
}

// pathElem locates a statement inside its container (block or clause) by index.
type pathElem struct {
	container ast.Node
	idx       int
}

// acquisition is a resource binding plus the lexical span in which returns
// are subject to the release obligation.
type acquisition struct {
	name     string
	kind     resourceKind
	end      token.Pos // end of the acquiring statement
	scopeEnd token.Pos // end of the acquired variable's scope
	guardPos token.Pos // span of the acquisition-error guard, NoPos when absent
	guardEnd token.Pos
	line     int
}

// releaseEvent is a call that disposes of a variable (Rollback/Commit/Close)
// or transfers its ownership to a helper ("transfer").
type releaseEvent struct {
	name   string
	method string
	pos    token.Pos
	path   []pathElem
}

type returnSite struct {
	pos  token.Pos
	path []pathElem
}

type lifecycleAnalyzer struct {
	fset    *token.FileSet
	fnBody  *ast.BlockStmt
	acqs    []acquisition
	events  []releaseEvent
	returns []returnSite
}

// auditGoResourceLifecycle parses one Go source file and returns one
// violation string per uncovered return path of an acquired database resource.
func auditGoResourceLifecycle(path string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		a := &lifecycleAnalyzer{fset: fset, fnBody: fn.Body}
		a.walkBlock(fn.Body, nil)
		violations = append(violations, a.check(path, fn.Name.Name)...)
	}
	return violations, nil
}

func childPath(path []pathElem, container ast.Node, idx int) []pathElem {
	p := make([]pathElem, len(path)+1)
	copy(p, path)
	p[len(path)] = pathElem{container: container, idx: idx}
	return p
}

func (a *lifecycleAnalyzer) walkBlock(block *ast.BlockStmt, path []pathElem) {
	for i, stmt := range block.List {
		p := childPath(path, block, i)
		if as, ok := stmt.(*ast.AssignStmt); ok {
			a.maybeAcquire(as, p, block.End(), nil)
		}
		a.walkStmt(stmt, p)
	}
}

func (a *lifecycleAnalyzer) walkStmt(stmt ast.Stmt, path []pathElem) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		a.walkBlock(s, path)
	case *ast.IfStmt:
		if as, ok := s.Init.(*ast.AssignStmt); ok {
			a.maybeAcquire(as, path, s.End(), s)
		}
		a.inspectExpr(s.Init, path)
		a.inspectExpr(s.Cond, path)
		a.walkBlock(s.Body, path)
		if s.Else != nil {
			a.walkStmt(s.Else, path)
		}
	case *ast.ForStmt:
		if as, ok := s.Init.(*ast.AssignStmt); ok {
			a.maybeAcquire(as, path, s.End(), nil)
		}
		a.inspectExpr(s.Init, path)
		a.inspectExpr(s.Cond, path)
		a.walkBlock(s.Body, path)
	case *ast.RangeStmt:
		a.inspectExpr(s.X, path)
		a.walkBlock(s.Body, path)
	case *ast.SwitchStmt:
		a.inspectExpr(s.Init, path)
		a.inspectExpr(s.Tag, path)
		a.walkClauses(s.Body, path)
	case *ast.TypeSwitchStmt:
		a.inspectExpr(s.Init, path)
		a.inspectExpr(s.Assign, path)
		a.walkClauses(s.Body, path)
	case *ast.SelectStmt:
		a.walkClauses(s.Body, path)
	case *ast.LabeledStmt:
		a.walkStmt(s.Stmt, path)
	case *ast.DeferStmt:
		a.inspectDefer(s, path)
	case *ast.GoStmt:
		// goroutine bodies do not run within this function's return paths
	case *ast.ReturnStmt:
		a.inspectReturn(s, path)
		a.returns = append(a.returns, returnSite{pos: s.Pos(), path: path})
	default:
		a.inspectExpr(stmt, path)
	}
}

func (a *lifecycleAnalyzer) walkClauses(body *ast.BlockStmt, path []pathElem) {
	for _, clause := range body.List {
		var stmts []ast.Stmt
		switch c := clause.(type) {
		case *ast.CaseClause:
			for _, expr := range c.List {
				a.inspectExpr(expr, path)
			}
			stmts = c.Body
		case *ast.CommClause:
			a.inspectExpr(c.Comm, path)
			stmts = c.Body
		}
		for j, stmt := range stmts {
			p := childPath(path, clause, j)
			if as, ok := stmt.(*ast.AssignStmt); ok {
				a.maybeAcquire(as, p, clause.End(), nil)
			}
			a.walkStmt(stmt, p)
		}
	}
}

// inspectExpr records release/transfer events inside an expression or simple
// statement. Function literals are pruned: their bodies run at call time, not
// inline, so their calls cannot be attributed to this position. Deferred
// closures are the exception and are handled by inspectDefer.
func (a *lifecycleAnalyzer) inspectExpr(node ast.Node, path []pathElem) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			a.recordCall(v, path, v.Pos())
		}
		return true
	})
}

// inspectDefer records releases inside a deferred call, descending into
// deferred closures (defer func() { _ = tx.Rollback() }()). Events are
// attributed to the defer statement's position: every return after it is
// covered.
func (a *lifecycleAnalyzer) inspectDefer(s *ast.DeferStmt, path []pathElem) {
	ast.Inspect(s.Call, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			a.recordCall(call, path, s.Pos())
		}
		return true
	})
}

// inspectReturn records releases embedded in the return expression itself
// (return tx.Commit(), return collect(rows)) at the return's own position.
func (a *lifecycleAnalyzer) inspectReturn(s *ast.ReturnStmt, path []pathElem) {
	for _, result := range s.Results {
		ast.Inspect(result, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok {
				a.recordCall(call, path, s.Pos())
			}
			return true
		})
	}
}

func (a *lifecycleAnalyzer) recordCall(call *ast.CallExpr, path []pathElem, pos token.Pos) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			switch sel.Sel.Name {
			case "Rollback", "Commit", "Close":
				a.events = append(a.events, releaseEvent{name: id.Name, method: sel.Sel.Name, pos: pos, path: path})
			}
		}
	}
	for _, arg := range call.Args {
		if id, ok := arg.(*ast.Ident); ok {
			a.events = append(a.events, releaseEvent{name: id.Name, method: "transfer", pos: pos, path: path})
		}
	}
}

// maybeAcquire records resource bindings from assignment RHS calls such as
// db.Begin(), db.BeginTx(), s.beginTx(ctx), db.Query(...) or tx.Prepare(...).
// Query/Prepare bindings are only tracked when the variable is named like a
// resource (suffix rows/stmt, matching the contract's original naming
// assumption) so domain methods like s.storage.Query(...) stay out of scope.
// selfGuard is the if/for statement when the acquisition happens in its init;
// the guard region is then that statement's body.
func (a *lifecycleAnalyzer) maybeAcquire(as *ast.AssignStmt, path []pathElem, scopeEnd token.Pos, selfGuard *ast.IfStmt) {
	for i, rhs := range as.Rhs {
		call, ok := rhs.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		kind, tracked := acquireMethods[strings.ToLower(sel.Sel.Name)]
		if !tracked || i >= len(as.Lhs) {
			continue
		}
		id, ok := as.Lhs[i].(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if kind != resourceTx && !resourceLikeName(id.Name) {
			continue
		}
		end := scopeEnd
		if as.Tok == token.ASSIGN {
			// Reassignment: the variable was declared in a wider scope.
			end = a.fnBody.End()
		}
		acq := acquisition{
			name:     id.Name,
			kind:     kind,
			end:      as.End(),
			scopeEnd: end,
			line:     a.fset.Position(as.Pos()).Line,
		}
		if selfGuard != nil {
			acq.guardPos, acq.guardEnd = guardSpanOf(selfGuard, as)
		} else {
			acq.guardPos, acq.guardEnd = a.errorGuardSpan(as, path)
		}
		a.acqs = append(a.acqs, acq)
	}
}

func resourceLikeName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "rows") || strings.HasSuffix(lower, "stmt")
}

// errorGuardSpan finds the if-statement that immediately follows the
// acquisition and checks its error — the idiom `tx, err := db.Begin();
// if err != nil { return ... }`. Returns inside that guard run on the path
// where the acquisition failed, so they carry no release obligation. When the
// acquisition is the last statement of its block, the search ascends the
// block chain (branch-style acquisition followed by a shared error check).
func (a *lifecycleAnalyzer) errorGuardSpan(as *ast.AssignStmt, path []pathElem) (token.Pos, token.Pos) {
	names := lhsIdents(as, "")
	for d := len(path) - 1; d >= 0; d-- {
		list := clauseStatements(path[d].container)
		if list == nil {
			return token.NoPos, token.NoPos
		}
		if path[d].idx+1 >= len(list) {
			continue
		}
		ifs, ok := list[path[d].idx+1].(*ast.IfStmt)
		if !ok || !condUsesAnyIdent(ifs.Cond, names) {
			return token.NoPos, token.NoPos
		}
		return ifs.Body.Pos(), ifs.Body.End()
	}
	return token.NoPos, token.NoPos
}

// guardSpanOf covers the if-init form `if rows, err := db.Query(...);
// err != nil { return ... }`: the if body itself is the error branch.
func guardSpanOf(ifs *ast.IfStmt, as *ast.AssignStmt) (token.Pos, token.Pos) {
	if condUsesAnyIdent(ifs.Cond, lhsIdents(as, "")) {
		return ifs.Body.Pos(), ifs.Body.End()
	}
	return token.NoPos, token.NoPos
}

func lhsIdents(as *ast.AssignStmt, skip string) map[string]bool {
	names := make(map[string]bool)
	for _, lhs := range as.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" && id.Name != skip {
			names[id.Name] = true
		}
	}
	if len(names) == 0 {
		names["err"] = true
	}
	return names
}

func condUsesAnyIdent(cond ast.Expr, names map[string]bool) bool {
	used := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && names[id.Name] {
			used = true
			return false
		}
		return !used
	})
	return used
}

func clauseStatements(node ast.Node) []ast.Stmt {
	switch c := node.(type) {
	case *ast.BlockStmt:
		return c.List
	case *ast.CaseClause:
		return c.Body
	case *ast.CommClause:
		return c.Body
	}
	return nil
}

func (a *lifecycleAnalyzer) check(path, fnName string) []string {
	var violations []string
	for _, acq := range a.acqs {
		for _, ret := range a.returns {
			if ret.pos <= acq.end || ret.pos >= acq.scopeEnd {
				continue
			}
			if acq.guardPos != token.NoPos && ret.pos >= acq.guardPos && ret.pos < acq.guardEnd {
				continue
			}
			if a.covered(acq, ret) {
				continue
			}
			pos := a.fset.Position(ret.pos)
			violations = append(violations, fmt.Sprintf("%s:%d %s: %s %q acquired at line %d is not released before this return",
				path, pos.Line, fnName, acq.kind, acq.name, acq.line))
		}
	}
	return violations
}

func (a *lifecycleAnalyzer) covered(acq acquisition, ret returnSite) bool {
	methods := acq.kind.releaseMethods()
	for _, ev := range a.events {
		if ev.name != acq.name {
			continue
		}
		if ev.method != "transfer" && !methods[ev.method] {
			continue
		}
		if dominates(ev, ret) {
			return true
		}
	}
	return false
}

// dominates reports whether the release event is guaranteed to execute before
// the return: the event's own block must be on the return's block chain
// (same block or an ancestor), positioned before the statement that contains
// the return. A release inside a sibling branch does not count.
func dominates(ev releaseEvent, ret returnSite) bool {
	d := len(ev.path) - 1
	if len(ret.path) < d+1 {
		return false
	}
	for l := 0; l < d; l++ {
		if ev.path[l].container != ret.path[l].container || ev.path[l].idx != ret.path[l].idx {
			return false
		}
	}
	if ev.path[d].container != ret.path[d].container {
		return false
	}
	if ev.path[d].idx < ret.path[d].idx {
		return true
	}
	return ev.path[d].idx == ret.path[d].idx && ev.pos <= ret.pos
}
