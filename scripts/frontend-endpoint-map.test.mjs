#!/usr/bin/env node
// Tests for scripts/frontend-endpoint-map.mjs (node:test, no new dependencies).
//
// The fixture under scripts/fixtures/frontend-endpoint-map/ is a miniature
// repository: backend/docs/openapi.yaml, backend/internal/api/router.go, the
// generated openapi-types.ts and a workbench app with an old client layer
// (apps/workbench/services), a bridge layer (apps/workbench/src/services-bridge),
// callers in pages/features/stores and a test file. The tests below pin the
// four review questions from the dispatch:
//   1. the status vocabulary (no `migrated`; deletion-after text derived from
//      `statusOf` itself, mechanically consistent with every row);
//   2. every import/call shape that keeps a client alive (named, aliased,
//      namespace, re-export chain, default-object gatherer, dynamic import,
//      function-as-value, unattributable shapes);
//   3. fail-closed when the TypeScript compiler is missing (non-zero exit, no
//      outputs, no regex fallback);
//   4. audit semantics (unused_route vs backend_unregistered, command_id only
//      on registered writes, auth/envelope evidence).
// Fixture copies used by CLI tests live under a temp dir; the real fixture
// directory is never written to (asserted below).
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import {
  DEFAULT_PATHS,
  STATUS_ORDER,
  afterStateClause,
  analyze,
  deleteImpact,
  loadTypescript,
  main,
  setTypescriptResolver,
  statusOf,
  transportOfRecord,
  typescriptOverride,
} from './frontend-endpoint-map.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..');
const scriptPath = path.join(repoRoot, 'scripts', 'frontend-endpoint-map.mjs');
const fixtureDir = path.join(repoRoot, 'scripts', 'fixtures', 'frontend-endpoint-map');

// ---------------------------------------------------------------------------
// fixture analysis (read-only; analyze() never writes)
// ---------------------------------------------------------------------------

const analysis = analyze({ root: fixtureDir, paths: DEFAULT_PATHS });
const ledger = analysis.ledger;
const md = analysis.markdown;

const byKey = new Map(ledger.entries.map((entry) => [entry.key, entry]));
const entryOf = (key) => {
  const entry = byKey.get(key);
  assert.ok(entry, `fixture ledger is missing the endpoint ${key}`);
  return entry;
};
const deletionOf = (key) => {
  const text = entryOf(key).deletion_condition;
  assert.ok(typeof text === 'string' && text.length > 0, `${key} has an empty deletion_condition`);
  return text;
};

// A minimal entry shaped like a `buildEndpointMap` row, for direct `statusOf`
// predicate checks that no fixture layout can produce (e.g. a plain route with
// nothing else on it).
const bareEntry = (overrides) => ({
  path: '/api/v1/x',
  openapi: 'missing',
  backend_route: null,
  old_clients: [],
  bridge: [],
  callers: [],
  test_callers: [],
  ...overrides,
});

// ---------------------------------------------------------------------------
// 1. status vocabulary and one row per status
// ---------------------------------------------------------------------------

test('every status in the vocabulary appears in the fixture ledger', () => {
  const seen = new Set(ledger.entries.map((entry) => entry.status));
  for (const status of STATUS_ORDER) {
    assert.ok(seen.has(status), `no fixture endpoint reads ${status}`);
  }
  assert.equal(seen.size, STATUS_ORDER.length, `unexpected statuses: ${[...seen].filter((s) => !STATUS_ORDER.includes(s))}`);
});

test('the old `migrated` name is gone from the vocabulary and the report', () => {
  assert.ok(!STATUS_ORDER.includes('migrated'));
  const oldVocabulary = ['migrated', 'unused_route'];
  for (const status of oldVocabulary) {
    assert.ok(!ledger.entries.some((entry) => entry.status === status), `${status} is not a valid status anymore`);
  }
  for (const entry of ledger.entries) {
    if (!entry.deletion_condition) continue;
    assert.ok(!/status[^.]*\bmigrated\b/.test(entry.deletion_condition), `${entry.key} still says "migrated"`);
  }
});

test('no status is named other than by the 10-value vocabulary', () => {
  const named = new Set();
  for (const entry of ledger.entries) {
    const matches = entry.deletion_condition.match(/after the edit the row reads ([a-z_]+)/g) ?? [];
    for (const match of matches) named.add(match.replace('after the edit the row reads ', ''));
  }
  for (const status of named) {
    assert.ok(STATUS_ORDER.includes(status), `deletion text names an unknown status: ${status}`);
    assert.notEqual(status, 'migrated');
  }
});

// ---------------------------------------------------------------------------
// 2. deletion-after state is mechanically consistent for every row (ruling 5)
// ---------------------------------------------------------------------------

test('every fixture row: deletion text names exactly deleteImpact(entry), or no after-state', () => {
  for (const entry of ledger.entries) {
    const derived = deleteImpact(entry);
    const text = entry.deletion_condition ?? '';
    const talksAfter = /after the edit the row reads|after the edit the row leaves/.test(text);
    if (derived === null) {
      assert.ok(!talksAfter, `${entry.key}: deleteImpact is null but the text promises an after-state: ${text}`);
      continue;
    }
    assert.ok(talksAfter, `${entry.key}: deleteImpact is ${derived} but the text has no after-state clause`);
    if (derived === 'unknown') {
      assert.match(text, /after the edit the row leaves this table/);
    } else {
      assert.match(text, new RegExp(`after the edit the row reads ${derived}\\b`));
    }
  }
});

test('deleteImpact simulates the deletion the status documents, through statusOf itself', () => {
  // legacy_unused WITHOUT a bridge is exactly the case the old report got
  // wrong ("deleting it leaves bridge_only"): deleting the client leaves the
  // contract+route row, never a bridge.
  const unused = {
    ...bareEntry({ path: '/api/v1/a', openapi: 'stable', backend_route: 'handlers.A', old_clients: ['services/a.ts#f'] }),
    status: 'legacy_unused',
  };
  assert.equal(deleteImpact(unused), 'contract_only');
  // legacy_unused on a route-only path leaves route_only, not contract_only.
  const routeOnly = { ...unused, path: '/api/v1/b', openapi: 'missing', backend_route: 'handlers.B' };
  assert.equal(deleteImpact(routeOnly), 'route_only');
  // legacy_unused on a path nothing registers leaves no row at all.
  const orphan = { ...unused, path: '/api/v1/c', openapi: 'missing', backend_route: null };
  assert.equal(deleteImpact(orphan), 'unknown');
  // A `both` row keeps its bridge when the old client goes.
  const both = {
    ...bareEntry({ path: '/api/v1/d', openapi: 'stable', backend_route: 'handlers.D', old_clients: ['services/d.ts#f'], bridge: ['bridge/d.ts#f'] }),
    status: 'both',
  };
  assert.equal(deleteImpact(both), 'bridge_only');
  // legacy_only moves onto the bridge T12.03.b writes.
  const legacyOnly = {
    ...bareEntry({ path: '/api/v1/e', openapi: 'stable', backend_route: 'handlers.E', old_clients: ['services/e.ts#f'], callers: ['pages/E.tsx#module'] }),
    status: 'legacy_only',
  };
  assert.equal(deleteImpact(legacyOnly), 'bridge_only');
  // States with nothing frontend-side to delete report no after-state.
  for (const status of ['contract_only', 'route_only', 'backend_unregistered', 'orphan_client', 'unknown']) {
    assert.equal(deleteImpact({ ...bareEntry({ openapi: 'stable', backend_route: 'handlers.X' }), status }), null);
  }
});

test('afterStateClause is a standalone clause and spells out "leaves this table"', () => {
  const entry = entryOf('DELETE /api/v1/memory/atomic/{id}');
  assert.equal(afterStateClause(entry, 'contract_only'), 'after the edit the row reads contract_only (the contract declares it; the router registers it)');
  assert.equal(afterStateClause(entry, 'route_only'), 'after the edit the row reads route_only (the contract declares it; the router registers it)');
  assert.match(afterStateClause(entry, 'unknown'), /after the edit the row leaves this table/);
  assert.equal(afterStateClause(entry, null), '');
});

test('deletion text per status: the action matches the state', () => {
  // legacy_unused without a bridge must NOT be described as a migration onto a bridge.
  const unused = deletionOf('GET /api/v1/realtime/socket');
  assert.match(unused, /no production caller exists/);
  assert.match(unused, /not a migration/i);
  assert.doesNotMatch(unused, /rewrites the caller/);
  // legacy_unused kept alive by a test must warn about the test, and its text
  // must say only test files evaluate it.
  const testOnly = deletionOf('DELETE /api/v1/memory/atomic/{id}');
  assert.match(testOnly, /only test files evaluate/);
  assert.match(testOnly, /break that test/);
  // legacy_only names the bridge client T12.03.b must write first.
  const legacyOnly = deletionOf('GET /api/v1/projects');
  assert.match(legacyOnly, /writes the bridge client/);
  assert.match(legacyOnly, /rewrites the caller/);
  assert.match(legacyOnly, /after the edit the row reads bridge_only/);
  // both moves callers onto the existing bridge.
  const both = deletionOf('GET /api/v1/workspace/list');
  assert.match(both, /moves the caller\(s\)/);
  assert.match(both, /The bridge call must stay/);
  // bridge_only retires the bridge call site, deleting nothing legacy.
  const bridgeOnly = deletionOf('GET /api/v1/agents/{id}/logs/stream');
  assert.match(bridgeOnly, /only the bridge calls it/);
  assert.match(bridgeOnly, /after the edit the row reads contract_only/);
  // legacy_direct removes call sites, not a module.
  const direct = deletionOf('POST /api/v1/memory/atomic/search');
  assert.match(direct, /no client module holds this path/);
  assert.match(direct, /removing or rewriting those call sites/);
  // contract_only / route_only / backend_unregistered / orphan_client / unknown:
  // no frontend deletion, and the text says what actually repairs the row.
  assert.match(deletionOf('GET /api/v1/projects/{id}'), /remove the operation from openapi\.yaml/);
  assert.match(deletionOf('GET /api/v1/debug/state'), /register the contract operation/);
  assert.match(deletionOf('POST /api/v1/workspace/force-sync'), /registers no matching route/);
  assert.match(deletionOf('GET /api/v1/playground/sessions'), /remove the call site or add the route \+ operation/);
  assert.match(deletionOf('GET {url}'), /no endpoint could be attributed/);
});

// ---------------------------------------------------------------------------
// 3. statuses of the named fixture rows (each state pinned with evidence)
// ---------------------------------------------------------------------------

test('fixture row statuses: one decisive row per state', () => {
  const expected = {
    'GET /api/v1/workspace/list': 'both',
    'GET /api/v1/projects': 'legacy_only',
    'DELETE /api/v1/workspace/files': 'legacy_only',
    'POST /api/v1/workspace/write': 'legacy_only',
    'DELETE /api/v1/memory/atomic/{id}': 'legacy_unused',
    'GET /api/v1/realtime/socket': 'legacy_unused',
    'POST /api/v1/memory/atomic/search': 'legacy_direct',
    'GET /api/v1/agents/{id}/logs/stream': 'bridge_only',
    'GET /api/v1/workspace/files/{id}/download': 'bridge_only',
    'GET /api/v1/projects/{id}': 'contract_only',
    'GET /api/v1/unregistered/only-in-contract': 'contract_only',
    'POST /api/v1/projects': 'contract_only',
    'GET /api/v1/debug/state': 'route_only',
    'POST /api/v1/workspace/force-sync': 'backend_unregistered',
    'GET /api/v1/playground/sessions': 'orphan_client',
    'GET {url}': 'unknown',
  };
  for (const [key, status] of Object.entries(expected)) {
    assert.equal(entryOf(key).status, status, `${key} should read ${status}`);
  }
});

test('route_only is a route with no contract and a test-only caller; legacy_direct is a route with no contract and a direct caller', () => {
  const routeOnly = entryOf('GET /api/v1/debug/state');
  assert.equal(routeOnly.openapi, 'missing');
  assert.notEqual(routeOnly.backend_route, null);
  assert.deepEqual(routeOnly.callers, []);
  assert.deepEqual(routeOnly.test_callers, ['apps/workbench/src/features/__tests__/projects.test.ts#module']);

  const direct = entryOf('POST /api/v1/memory/atomic/search');
  assert.equal(direct.openapi, 'stable');
  assert.equal(direct.backend_route, null);
  assert.ok(direct.callers.length > 0, 'legacy_direct still has a production call site on record');
  assert.deepEqual(direct.old_clients, []);
  assert.match(deletionOf('POST /api/v1/memory/atomic/search'), /after the edit the row reads backend_unregistered/);
});

test('route_only / backend_unregistered / orphan_client are never merged', () => {
  assert.equal(statusOf(bareEntry({ backend_route: 'handlers.X' })), 'route_only');
  assert.equal(statusOf(bareEntry({ openapi: 'stable' })), 'backend_unregistered');
  assert.equal(statusOf(bareEntry({ callers: ['pages/X.tsx#module'] })), 'orphan_client');
  // A test-only call to an unknown path is still attributable (orphan), a
  // placeholder path with no call at all is not (unknown).
  assert.equal(statusOf(bareEntry({ test_callers: ['t.test.ts#module'] })), 'orphan_client');
  assert.equal(statusOf(bareEntry({ path: '{url}' })), 'unknown');
});

// ---------------------------------------------------------------------------
// 4. caller shapes (review question 2): each shape is a caller
// ---------------------------------------------------------------------------

const callersOf = (key) => entryOf(key).callers;

test('named import + call is a caller', () => {
  // MockedPage calls listWorkspaceFiles inside a JSX handler: a production caller.
  assert.ok(callersOf('GET /api/v1/workspace/list').some((caller) => caller.startsWith('apps/workbench/src/pages/MockedPage.tsx')));
  // The test file calls it too, but test callers must NOT feed the production
  // "somebody calls it" verdict: they land in test_callers instead.
  assert.ok(entryOf('GET /api/v1/workspace/list').test_callers.some((caller) => caller.startsWith('apps/workbench/src/features/__tests__/')));
  assert.ok(!callersOf('GET /api/v1/workspace/list').some((caller) => caller.includes('__tests__')));
  assert.ok(callersOf('DELETE /api/v1/workspace/files').some((caller) => caller.startsWith('apps/workbench/src/pages/ShellPage.tsx')));
});

test('aliased named import + call is a caller', () => {
  const callers = callersOf('GET /api/v1/agents/{id}/logs/raw');
  assert.ok(
    callers.some((caller) => caller === 'apps/workbench/src/pages/AgentLogStream.tsx#module'),
    `aliased import (readAgentLogChunks as readChunks) not recognised: ${callers}`,
  );
});

test('namespace import with a member value pass is a caller', () => {
  assert.ok(callersOf('DELETE /api/v1/workspace/files').some((caller) => caller.startsWith('apps/workbench/src/pages/ShellPage.tsx')));
});

test('re-export chain through a barrel is a caller', () => {
  // projectStore pulls listProjects through `./barrel` (a star re-export) with
  // a dynamic import; sessionStore pulls deleteWorkspaceFiles through the
  // services barrel with `import * as ws`.
  assert.ok(callersOf('GET /api/v1/projects').includes('apps/workbench/src/stores/projectStore.ts#module'));
  assert.ok(callersOf('DELETE /api/v1/workspace/files').includes('apps/workbench/src/stores/sessionStore.ts#module'));
  // The export member list the barrel hop carried is a Client#name key: the
  // owner recorded for projectStore's use is services/projects.ts#listProjects,
  // not stores/barrel.ts#listProjects (mutation 2's failure mode).
  const evidence = entryOf('GET /api/v1/projects').import_evidence.find(
    (row) => row.importer === 'apps/workbench/src/stores/projectStore.ts',
  );
  assert.equal(evidence.client, 'apps/workbench/services/projects.ts#listProjects');
});

test('default-object gatherer (`export default { f }` then `api.f`) is a caller', () => {
  // ProjectsPage passes api.listProjects as a queryFn and a JSX prop, and
  // calls api.deleteProject in a handler. Both members are declared by
  // services/projects.ts, so the page must appear as a caller of both rows.
  const projects = callersOf('GET /api/v1/projects');
  assert.ok(projects.includes('apps/workbench/src/pages/ProjectsPage.tsx#module'), `default-object member call lost: ${projects}`);
  assert.ok(callersOf('DELETE /api/v1/projects/{id}').includes('apps/workbench/src/pages/ProjectsPage.tsx#module'));
});

test('function-as-value (`useQuery({ queryFn: f })`, `onClick={f}`) is a caller', () => {
  // api.listProjects is never called in ProjectsPage; it is only passed as a
  // value. A value-held function is still a caller.
  const evidence = entryOf('GET /api/v1/projects').import_evidence ?? [];
  const page = evidence.find((row) => row.importer === 'apps/workbench/src/pages/ProjectsPage.tsx');
  assert.ok(page, 'ProjectsPage value-pass evidence missing');
  assert.match(page.context, /JsxExpression|PropertyAssignment|PropertyAccessExpression|CallExpression/);
  // WorkspacePage holds writeWorkspaceFile as a value and never calls it.
  assert.ok(callersOf('POST /api/v1/workspace/write').includes('apps/workbench/src/pages/WorkspacePage.tsx#module'));
});

test('dynamic import with a member call is a caller', () => {
  assert.ok(callersOf('GET /api/v1/projects').includes('apps/workbench/src/stores/projectStore.ts#module'));
});

test('unattributable shapes are recorded as unresolved, never as "no caller"', () => {
  // ShortcutBar destructures a namespace import; the walker cannot tie the
  // local to a member, so every export of services/workspace.ts must be
  // recorded as possibly imported.
  const unresolved = ledger.unresolved_imports.filter((row) => row.file === 'apps/workbench/src/features/ShortcutBar.tsx');
  assert.equal(unresolved.length, 1);
  assert.match(unresolved[0].reason, /escapes as a value|destructur/);
  assert.ok(callersOf('DELETE /api/v1/workspace/files').includes('apps/workbench/src/features/ShortcutBar.tsx#module'));
  // A whole-object escape (`api[name]`, `return api`) cannot be proven either.
  const store = ledger.unresolved_imports.filter((row) => row.file === 'apps/workbench/src/stores/projectStore.ts');
  assert.ok(store.length >= 1, 'projectStore namespace escape must stay on record');
});

test('a namespace binding read as a call head is not treated as an escape', () => {
  // `ws.connect(...)` visits `ws` in a `.head` position - that is member-read
  // evidence the walker does have, and it must not mark every member
  // unreached (or the report drowns in bogus unresolved rows).
  const session = ledger.unresolved_imports.find((row) => row.file === 'apps/workbench/src/stores/sessionStore.ts');
  assert.ok(session, 'sessionStore namespace import should still be reported for its unreached names');
  assert.doesNotMatch(session.reason, /escapes as a value/);
});

test('placeholder-only table members are inlined, never guessed into orphan_client', () => {
  // Defect (A): `API_ENDPOINTS.projects` inside a template must inline to
  // `/api/v1/projects`; a placeholder row like `{API_ENDPOINTS.projects}`
  // would make a live client look like a call to an unregistered route.
  assert.ok(byKey.has('GET /api/v1/projects'), 'API_ENDPOINTS.projects did not inline');
  for (const entry of ledger.entries) {
    assert.doesNotMatch(entry.key, /\{API_ENDPOINTS\./, `placeholder leaked into the ledger: ${entry.key}`);
  }
  assert.ok(!ledger.unknown_entries.some((row) => /API_ENDPOINTS/.test(row.expression)), 'table member reported as unresolvable');
});

// ---------------------------------------------------------------------------
// 5. non-JSON entries (dispatch: cannot migrate as ordinary JSON)
// ---------------------------------------------------------------------------

test('all six non-JSON transports are listed in the throwaway section', () => {
  const transports = new Set(ledger.non_json_entries.map((entry) => entry.transport));
  for (const transport of ['sse', 'websocket', 'blob', 'download', 'multipart', 'stream']) {
    assert.ok(transports.has(transport), `non-JSON section is missing ${transport}`);
  }
  assert.ok(ledger.non_json_entries.every((entry) => entry.transport !== 'json' && entry.transport !== 'unknown'));
  assert.match(md, /## Non-JSON entries \(cannot migrate as ordinary JSON\)/);
  assert.match(md, /\| `GET \/api\/v1\/agents\/\{id\}\/logs\/stream` \| sse \|/);
  assert.match(md, /\| `GET \/api\/v1\/realtime\/socket` \| websocket \|/);
  assert.match(md, /\| `POST \/api\/v1\/workspace\/upload` \| multipart \|/);
  assert.match(md, /\| `GET \/api\/v1\/agents\/\{id\}\/logs\/raw` \| stream \|/);
  assert.match(md, /\| `GET \/api\/v1\/exports\/\{id\}` \| download \|/);
  assert.match(md, /\| `GET \/api\/v1\/workspace\/files\/\{id\}\/download` \| blob \|/);
});

test('transportOfRecord: every transport marker maps to its transport value', () => {
  const ws = transportOfRecord({ kind: 'websocket', resolvedPath: '/ws', rawUrl: 'ws://127.0.0.1/ws' }, null);
  assert.equal(ws.transport, 'websocket');
  assert.equal(transportOfRecord({ kind: 'event-source', resolvedPath: '/x' }, null).transport, 'sse');
  assert.equal(transportOfRecord({ kind: 'fetch', resolvedPath: '/x', fetchInfo: { multipart: true } }, null).transport, 'multipart');
  assert.equal(transportOfRecord({ kind: 'fetch', resolvedPath: '/x', fetchInfo: { download: true, blob: true } }, null).transport, 'download');
  assert.equal(transportOfRecord({ kind: 'fetch', resolvedPath: '/x', fetchInfo: { blob: true } }, null).transport, 'blob');
  assert.equal(transportOfRecord({ kind: 'fetch', resolvedPath: '/x', fetchInfo: { stream: true } }, null).transport, 'stream');
  assert.equal(transportOfRecord({ kind: 'fetch', resolvedPath: '/x', fetchInfo: {} }, null).transport, 'json');
  // Content types decide when nobody calls the endpoint.
  assert.equal(transportOfRecord({ kind: 'fetch', resolvedPath: '/x', fetchInfo: {} }, 'sse').transport, 'sse');
});

// ---------------------------------------------------------------------------
// 6. unknown URL expressions are reported, never guessed
// ---------------------------------------------------------------------------

test('unresolvable URL expressions land in unknown_entries, not as guessed paths', () => {
  const expressions = ledger.unknown_entries.map((row) => row.expression);
  assert.ok(expressions.includes('this.url'), `websocket connect URL missing: ${expressions.join(' | ')}`);
  assert.ok(expressions.some((expr) => expr.includes('${host}')), 'Playground.composed URL missing');
  for (const row of ledger.unknown_entries) {
    assert.ok(row.file && row.function, 'unknown entries carry their call site');
  }
  // The unresolvable call sites must not spawn endpoint rows with fake paths.
  assert.ok(!ledger.entries.some((entry) => entry.key.includes('{host}')));
  assert.match(md, /## Unresolvable URL expressions \(not guessed\)/);
  assert.match(md, /never guessed into a route|not guessed/);
});

// ---------------------------------------------------------------------------
// 7. schema coverage (nullable/union/required/enum/pagination)
// ---------------------------------------------------------------------------

test('schema coverage reports all five areas and the pagination sites', () => {
  const areas = ledger.schema_coverage.measures.map((measure) => measure.area);
  assert.deepEqual([...areas].sort(), ['enum', 'nullable', 'pagination', 'required', 'union']);
  const pagination = ledger.schema_coverage.measures.find((measure) => measure.area === 'pagination');
  assert.equal(pagination.openapi, 2);
  assert.equal(pagination.generated, 2);
  assert.deepEqual(ledger.schema_coverage.pagination.openapi_sites, ['GET /api/v1/workspace/list?cursor', 'GET /api/v1/workspace/list?limit']);
  assert.deepEqual(ledger.schema_coverage.pagination.generated_property_names, ['cursor', 'limit']);
  const nullable = ledger.schema_coverage.measures.find((measure) => measure.area === 'nullable');
  assert.ok(nullable.openapi > 0 && nullable.generated > 0);
  const union = ledger.schema_coverage.measures.find((measure) => measure.area === 'union');
  assert.ok(union.openapi > 0 && union.generated > 0);
  const required = ledger.schema_coverage.measures.find((measure) => measure.area === 'required');
  assert.ok(required.openapi > 0 && required.generated > 0);
  const enumArea = ledger.schema_coverage.measures.find((measure) => measure.area === 'enum');
  assert.ok(enumArea.openapi > 0 && enumArea.generated > 0);
  // Where the fixture contract and generated types diverge, the divergence is
  // reported as a fact rather than silently equalised.
  assert.ok(ledger.schema_coverage.mismatches.some((mismatch) => mismatch.area === 'enum'));
  assert.match(md, /## Schema coverage \(T12\.03\.a\)/);
});

// ---------------------------------------------------------------------------
// 8. DEV mock detection
// ---------------------------------------------------------------------------

test('DEV mock entry points are detected and non-dev fallbacks flagged', () => {
  const mocks = ledger.mock_entry_points;
  assert.equal(mocks.dev_flag, 'import.meta.env.DEV');
  const guarded = mocks.injection_sites.find((site) => site.site === 'apps/workbench/src/pages/WorkspacePage.tsx');
  assert.ok(guarded, 'DEV-guarded mock import missing');
  assert.equal(guarded.guarded, true);
  assert.deepEqual(guarded.modules, ['../mocks/workspace']);
  const unguarded = mocks.injection_sites.find((site) => site.site === 'apps/workbench/src/pages/MockedPage.tsx');
  assert.ok(unguarded, 'non-DEV-guarded mock import missing');
  assert.equal(unguarded.guarded, false);
  assert.ok(mocks.non_dev_fallbacks.some((line) => line.includes('MockedPage.tsx')));
  assert.deepEqual(mocks.store_files, ['apps/workbench/src/mocks/workspace.ts']);
});

// ---------------------------------------------------------------------------
// 9. audit semantics (review question 4)
// ---------------------------------------------------------------------------

test('command_id: `no` only on registered state-changing routes', () => {
  for (const entry of ledger.entries) {
    if (entry.checks.command_id === 'no') {
      assert.notEqual(entry.backend_route, null, `${entry.key}: command_id no on an unregistered route`);
      assert.ok(['POST', 'PUT', 'PATCH', 'DELETE'].includes(entry.method), `${entry.key}: command_id no on a read`);
    }
    if (!['POST', 'PUT', 'PATCH', 'DELETE'].includes(entry.method)) assert.equal(entry.checks.command_id, 'n/a');
  }
  // A registered write with no Idempotency-Key at the call site reads `no`;
  // an unregistered write reads `unknown`, not `no`.
  assert.equal(entryOf('POST /api/v1/workspace/write').checks.command_id, 'no');
  assert.equal(entryOf('POST /api/v1/workspace/force-sync').checks.command_id, 'unknown');
  // A registered write whose call site sends Idempotency-Key reads yes; an
  // unregistered write reads unknown even when the call site sends one, since
  // no idempotency contract exists to honour.
  assert.equal(entryOf('POST /api/v1/workspace/upload').checks.command_id, 'yes');
  assert.equal(entryOf('POST /api/v1/memory/atomic/search').checks.command_id, 'unknown');
});

test('auth, envelope and retry are evidence-backed; unknown is never a guess', () => {
  assert.equal(entryOf('GET /api/v1/workspace/list').checks.auth, 'bearer');
  assert.equal(entryOf('GET /api/v1/workspace/list').checks.envelope, 'legacy');
  assert.equal(entryOf('GET /api/v1/realtime/socket').checks.auth, 'none');
  assert.equal(entryOf('GET /api/v1/realtime/socket').checks.envelope, 'raw');
  assert.equal(entryOf('GET /api/v1/realtime/socket').checks.retry, 'retry');
  assert.equal(entryOf('GET /api/v1/agents/{id}/logs/stream').checks.auth, 'unknown');
  // A contract-only row with no call site has no evidence to read: unknown,
  // not "bearer because the endpoint looks like the others".
  assert.equal(entryOf('GET /api/v1/projects/{id}').checks.auth, 'unknown');
  // Every entry carries check_notes with the evidence the reading came from.
  for (const entry of ledger.entries.filter((e) => e.checks.transport !== 'json')) {
    assert.ok(Array.isArray(entry.check_notes), `${entry.key}: no check_notes`);
  }
  assert.equal(entryOf('POST /api/v1/memory/atomic/search').checks.envelope, 'legacy');
  assert.equal(entryOf('POST /api/v1/memory/atomic/search').checks.command_id, 'unknown');
});

test('the report documents what it can and cannot see', () => {
  assert.match(md, /## What this table can and cannot see/);
  assert.match(md, /Recognised import shapes/);
  assert.match(md, /unresolved_imports/);
  // The limitations section must name the unattributable shapes and the
  // coarseness of the destructure scan, rather than implying completeness.
  assert.match(md, /destructur|spread/i);
});

// ---------------------------------------------------------------------------
// 10. --check mode and determinism (CLI, on a temp copy)
// ---------------------------------------------------------------------------

function makeTempRoot(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'fe-map-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  fs.cpSync(fixtureDir, dir, { recursive: true });
  return dir;
}

function runCli(root, args = [], env = {}) {
  return spawnSync(process.execPath, [scriptPath, '--root', root, ...args], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: { ...process.env, ...env },
  });
}

test('determinism: two runs over the same input are byte-identical', (t) => {
  const rootA = makeTempRoot(t);
  assert.equal(runCli(rootA).status, 0);
  const first = fs.readFileSync(path.join(rootA, 'apps', 'workbench', 'generated', 'endpoint-map.json'), 'utf8');
  const firstMd = fs.readFileSync(path.join(rootA, 'apps', 'workbench', 'generated', 'endpoint-map.md'), 'utf8');
  assert.equal(runCli(rootA).status, 0);
  const second = fs.readFileSync(path.join(rootA, 'apps', 'workbench', 'generated', 'endpoint-map.json'), 'utf8');
  const secondMd = fs.readFileSync(path.join(rootA, 'apps', 'workbench', 'generated', 'endpoint-map.md'), 'utf8');
  assert.equal(first, second);
  assert.equal(firstMd, secondMd);
  assert.ok(!first.includes('\r') && !firstMd.includes('\r'), 'generated files must be LF-only');
});

test('--check: exit 0 when up to date, non-zero after a caller is deleted from the fixture copy', (t) => {
  const root = makeTempRoot(t);
  assert.equal(runCli(root).status, 0);
  const check = runCli(root, ['--check']);
  assert.equal(check.status, 0, check.stderr);
  assert.match(check.stdout, /up to date/);

  // Delete a production caller of listWorkspaceFiles: the map must go stale
  // and --check must fail (this is the deletion-detection loop T12.03.b runs).
  const page = path.join(root, 'apps', 'workbench', 'src', 'pages', 'MockedPage.tsx');
  const source = fs.readFileSync(page, 'utf8');
  fs.writeFileSync(page, source.replace('onClick={() => void listWorkspaceFiles(3)}', 'onClick={() => undefined}'), 'utf8');
  const stale = runCli(root, ['--check']);
  assert.notEqual(stale.status, 0, '--check must fail after a caller changes');
  assert.match(stale.stderr, /out of date/);
});

test('--check: a CRLF checkout with identical content still passes', (t) => {
  const root = makeTempRoot(t);
  assert.equal(runCli(root).status, 0);
  const generated = path.join(root, 'apps', 'workbench', 'generated');
  for (const file of ['endpoint-map.json', 'endpoint-map.md']) {
    const abs = path.join(generated, file);
    const text = fs.readFileSync(abs, 'utf8');
    fs.writeFileSync(abs, text.replace(/\n/g, '\r\n'), 'utf8');
  }
  const check = runCli(root, ['--check']);
  assert.equal(check.status, 0, check.stderr);
});

test('the CLI writes only its two generated files inside the root', (t) => {
  const root = makeTempRoot(t);
  const before = walk(root);
  assert.equal(runCli(root).status, 0);
  const after = walk(root);
  const created = [...after.keys()].filter((rel) => !before.has(rel));
  assert.deepEqual(created.sort(), ['apps/workbench/generated/endpoint-map.json', 'apps/workbench/generated/endpoint-map.md']);
  for (const rel of before.keys()) {
    assert.equal(after.get(rel), before.get(rel), `${rel} was modified by a generation run`);
  }
});

test('analyze() on the real fixture directory writes nothing and leaves it unchanged', () => {
  const before = walk(fixtureDir);
  analyze({ root: fixtureDir, paths: DEFAULT_PATHS });
  const after = walk(fixtureDir);
  assert.deepEqual([...after.keys()].sort(), [...before.keys()].sort());
  for (const [rel, hash] of before) assert.equal(after.get(rel), hash, `${rel} changed during analysis`);
});

// A recursive listing of file sizes + mtimes + contents hash, for the
// write-scope assertions.
function walk(dir) {
  const out = new Map();
  const visit = (current) => {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const abs = path.join(current, entry.name);
      if (entry.isDirectory()) visit(abs);
      else {
        const rel = path.relative(dir, abs).split(path.sep).join('/');
        const content = fs.readFileSync(abs, 'utf8');
        out.set(rel, `${content.length}:${simpleHash(content)}`);
      }
    }
  };
  visit(dir);
  return out;
}

function simpleHash(text) {
  let hash = 0;
  for (let i = 0; i < text.length; i += 1) {
    hash = (hash * 31 + text.charCodeAt(i)) | 0;
  }
  return hash;
}

// ---------------------------------------------------------------------------
// 11. fail closed when the TypeScript compiler is missing (review question 3)
// ---------------------------------------------------------------------------

test('missing TypeScript: CLI exits non-zero, explains itself, writes nothing', (t) => {
  const root = makeTempRoot(t);
  const generated = path.join(root, 'apps', 'workbench', 'generated');
  // Remove any stale outputs so "wrote nothing" is provable.
  fs.rmSync(path.join(generated, 'endpoint-map.json'), { force: true });
  fs.rmSync(path.join(generated, 'endpoint-map.md'), { force: true });
  const result = runCli(root, [], { CODEFLOW_NO_TYPESCRIPT: '1' });
  assert.notEqual(result.status, 0, 'a missing compiler must not produce a ledger');
  assert.match(result.stderr, /TypeScript compiler not found/);
  assert.match(result.stderr, /no output was written/);
  assert.ok(!fs.existsSync(path.join(generated, 'endpoint-map.json')));
  assert.ok(!fs.existsSync(path.join(generated, 'endpoint-map.md')));
});

test('loadTypescript throws when the compiler cannot be resolved (resolver seam)', () => {
  const previous = loadTypescript();
  assert.ok(previous, 'the real compiler should resolve in this environment');
  try {
    setTypescriptResolver(() => null);
    assert.throws(() => loadTypescript(), /TypeScript compiler not found/);
  } finally {
    setTypescriptResolver(null);
  }
  // Restored.
  assert.equal(loadTypescript(), previous);
});

test('analyze() is fail-closed too: a null override throws before any output', () => {
  const previous = typescriptOverride.value;
  try {
    typescriptOverride.value = null; // not used; the resolver seam provides the null
    setTypescriptResolver(() => null);
    assert.throws(() => analyze({ root: fixtureDir, paths: DEFAULT_PATHS }), /TypeScript compiler not found/);
  } finally {
    setTypescriptResolver(null);
    typescriptOverride.value = previous;
  }
  // And the override seam can substitute a compiler directly.
  try {
    typescriptOverride.value = previous;
    assert.ok(analyze({ root: fixtureDir, paths: DEFAULT_PATHS }).ledger.entries.length > 0);
  } finally {
    typescriptOverride.value = null;
  }
});

// ---------------------------------------------------------------------------
// 12. main() seam over a temp copy (writes go to the temp dir, --check passes)
// ---------------------------------------------------------------------------

test('main() exit codes: generate writes inside the root, then --check passes', (t) => {
  const root = makeTempRoot(t);
  const lines = [];
  const code = main({ root, log: (line) => lines.push(line), error: (line) => lines.push(`ERR ${line}`) });
  assert.equal(code, 0, lines.join('\n'));
  assert.ok(fs.existsSync(path.join(root, 'apps', 'workbench', 'generated', 'endpoint-map.json')));

  const checkLines = [];
  const checkCode = main({ root, check: true, log: (line) => checkLines.push(line), error: (line) => checkLines.push(`ERR ${line}`) });
  assert.equal(checkCode, 0, checkLines.join('\n'));
  assert.match(checkLines.join('\n'), /up to date/);
});

// ---------------------------------------------------------------------------
// 11. main-agent acceptance review (T12.03.a): three guards no test above
//     reached - each mutation survived the suite and each test below fails
//     under it
// ---------------------------------------------------------------------------

test('a production caller inside the services layer still keeps an old client alive', () => {
  // One old client calling another is a production caller: reading the callee
  // as legacy_unused tells T12.03.b to delete a module the caller still needs.
  const known = { openapi: 'stable', backend_route: '/api/v1/x' };
  const client = ['apps/workbench/services/b.ts#fetchX'];
  assert.equal(statusOf(bareEntry({ ...known, old_clients: client, callers: ['apps/workbench/services/a.ts#loadAll'] })), 'legacy_only');
  assert.equal(statusOf(bareEntry({ ...known, old_clients: client, test_callers: ['apps/workbench/services/__tests__/b.test.ts#module'] })), 'legacy_unused');
  assert.equal(statusOf(bareEntry({ ...known, old_clients: client })), 'legacy_unused');
});

test('a placeholder-only path is unknown even with a client and callers on it', () => {
  // The path was never attributed, so it is not a route the backend could miss:
  // orphan_client would send T12.03.b after a call site that names no endpoint.
  for (const unattributed of ['{url}', '{API_ENDPOINTS.projects}/{id}']) {
    const entry = bareEntry({
      path: unattributed,
      old_clients: ['apps/workbench/services/http.ts#request'],
      callers: ['apps/workbench/src/pages/P.tsx#P'],
    });
    assert.equal(statusOf(entry), 'unknown', unattributed);
  }
});

test('a client reached through a shape the walker cannot attribute never reads caller-free', (t) => {
  // The real map leans on this guard: without it 26 legacy_only rows of the
  // workbench read legacy_unused. Here a page reaches deleteAtomicMemory only by
  // destructuring a gatherer object, so no name can be tied to the use.
  assert.equal(entryOf('DELETE /api/v1/memory/atomic/{id}').status, 'legacy_unused');
  const root = makeTempRoot(t);
  fs.appendFileSync(path.join(root, 'apps/workbench/services/atomic_memory.ts'),
    '\n// A gatherer object: callers reach the client through it.\nexport const atomicApi = { deleteAtomicMemory };\n');
  fs.writeFileSync(path.join(root, 'apps/workbench/src/pages/AtomicCleanup.tsx'), [
    "import { atomicApi } from '../../services/atomic_memory';",
    '',
    'export async function purge(id: string) {',
    '  const { deleteAtomicMemory: remove } = atomicApi;',
    '  return remove(id);',
    '}',
    '',
  ].join('\n'));

  const copied = analyze({ root, paths: DEFAULT_PATHS }).ledger;
  const row = copied.entries.find((entry) => entry.key === 'DELETE /api/v1/memory/atomic/{id}');
  assert.ok(row, 'the copied fixture lost the atomic delete row');
  assert.equal(row.status, 'legacy_only');
  assert.ok(row.callers.includes('apps/workbench/src/pages/AtomicCleanup.tsx#module'), JSON.stringify(row.callers));
  assert.ok(
    copied.unresolved_imports.some((entry) => entry.file === 'apps/workbench/src/pages/AtomicCleanup.tsx' && /cannot attribute/.test(entry.reason)),
    'the unattributable shape is not on record in unresolved_imports',
  );
});

test('the ledger names its inputs with forward slashes on every platform', () => {
  // A path.join() value leaks the host separator into the committed ledger, and
  // a Windows-generated map then fails --check on Linux CI.
  for (const [field, value] of Object.entries(ledger.generated_from)) {
    assert.doesNotMatch(String(value), /\\/, `generated_from.${field} = ${value}`);
  }
  // An escaped backslash in the JSON text is what a Windows path leaves behind.
  assert.doesNotMatch(JSON.stringify(ledger), /\\\\/, 'a backslash path is left somewhere in the ledger');
});
