#!/usr/bin/env node
/**
 * T12.03.a - endpoint -> old client -> callers -> new bridge ledger.
 *
 * Plan section 28 T12.03.a: build the hand-off table T12.03.b migrates one
 * module at a time from, and that T12.03.c recycles by listing the endpoint
 * set before and after. This is a *machine* table: derived from the
 * repository, deterministic (sorted arrays, fixed key order, trailing
 * newline) and re-checkable (`--check` compares against the committed files).
 *
 * Inputs (all read-only):
 *   - backend/internal/api/router.go            route registrations (group prefixes)
 *   - backend/docs/openapi.yaml                 operation set + experimental flags
 *   - apps/workbench/generated/openapi-types.ts operation ids (OpenApiOperationMap)
 *   - apps/workbench/api.ts                     API_ENDPOINTS/WS_ENDPOINTS transports
 *   - apps/workbench/services/**                the old client layer
 *   - apps/workbench/src/services-bridge/**     the new bridge
 *   - every other .ts/.tsx under apps/workbench call sites (+ test callers split out)
 *
 * Outputs:
 *   - apps/workbench/generated/endpoint-map.json
 *   - apps/workbench/generated/endpoint-map.md   (rendered from the json)
 *
 * Usage: node scripts/frontend-endpoint-map.mjs [--check]
 *        [--root <dir>] [--openapi <file>] [--router <file>]
 *        [--generated-types <file>] [--out-json <file>] [--out-md <file>]
 *
 * Parsing: the TypeScript compiler API (an existing repo dependency, resolved
 * the same way generate-openapi-types.mjs resolves yaml). It is required: when
 * the module cannot be resolved the script exits non-zero with an explanation
 * instead of falling back to regex heuristics, because a second, coarser
 * extractor would make the same checkout produce two different ledgers
 * depending on the machine. There is exactly one extractor (`typescript`).
 *
 * Per-endpoint checks (the list T12.03.a names):
 *   auth            bearer (api.ts request() or authHeadersFor(url)) | cookie
 *                   (page-origin fetch with no credential path) | query_token |
 *                   none (transports with no HTTP credential, e.g. WS upgrade) |
 *                   unknown
 *   envelope        v3 ({error,meta}) | legacy ({success,data}) | raw (body read
 *                   without a wrapper) | unknown
 *   command_id      yes | no | n/a | unknown - only asked of the METHODS THAT
 *                   CHANGE STATE (POST/PUT/PATCH/DELETE); only "no" when the
 *                   backend actually registers the route, so reads and
 *                   contract-first stubs are not reported as missing an
 *                   Idempotency-Key
 *   retry           none | retry | fallback_mock | fallback_static | unknown
 *   project_scope   project | global | unknown (path-based; a project id used
 *                   only as a query filter or body field is global)
 *   transport       json | sse | websocket | blob | download | multipart |
 *                   stream | unknown
 *   status          see STATUS_RULES below - `migrated` is NOT one of them.
 *                   `legacy_only` -> the old client still has a caller;
 *                   `legacy_unused` -> it does not (dead code to delete, with
 *                   no bridge to fall back to); `legacy_direct` -> the path is
 *                   called through the shared transport with no client module.
 */

/**
 * Final status vocabulary (see `statusOf` for the exact predicates). Every
 * state names what a deletion does to the row, because T12.03.b deletes old
 * clients one row at a time. The "after" half is machine-derived by
 * `deleteImpact` (which calls `statusOf` itself), so the text in
 * `deletion_condition` cannot drift away from the predicate:
 *
 *   both             old client + bridge call it -> delete the old client
 *                    (moving its callers onto the bridge first when there are
 *                    any) -> afterwards `bridge_only`
 *   legacy_only      old client only, and production code still calls that
 *                    client (another old client counts too) -> T12.03.b writes
 *                    a bridge client, moves the callers onto it, deletes the
 *                    old client -> afterwards `bridge_only`
 *   legacy_unused    old client only, no production caller anywhere (test
 *                    callers do not count) -> dead code;
 *                    deleting the function leaves `contract_only`,
 *                    `route_only` or `backend_unregistered` (whichever the
 *                    backend side still declares), or the row leaves the table
 *                    when nothing on it is left
 *   legacy_direct    the path is called through the shared transport (or a
 *                    direct fetch) with no client module at all -> there is no
 *                    old client to delete; removing the call sites leaves the
 *                    state the backend side still declares; T12.03.b must not
 *                    look for a module here
 *   bridge_only      only the bridge calls it -> no legacy client to delete; if
 *                    the bridge call site is retired the row falls back to what
 *                    the backend side still declares, and it leaves the table
 *                    once the contract operation and/or the route is removed
 *   contract_only    the contract declares it and the router registers it, no
 *                    frontend caller at all -> row disappears when the route
 *                    (and the contract operation) are removed
 *   route_only       router.go registers the path, the contract declares no
 *                    operation and no frontend caller exists -> row disappears
 *                    when the route is removed (or the contract operation is
 *                    added); a frontend change cannot clear it
 *   backend_unregistered  the contract declares it but the backend registers no
 *                    such route (contract-first stub or drifted route) -> row
 *                    disappears when the contract operation is removed; this is
 *                    the state that makes "in the contract, not in the router"
 *                    visible, distinct from contract_only
 *   orphan_client    frontend code calls a path neither contract nor router
 *                    knows -> row disappears when the call site is removed or
 *                    the backend registers the route
 *   unknown          no endpoint could be attributed to this call site (a bare
 *                    helper template, a URL built at runtime) -> not a route;
 *                    the row leaves this table when the call site disappears
 */
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import { repoRoot } from './_shared/runtime.mjs';

const repoRequire = createRequire(path.join(repoRoot, 'package.json'));

export const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete'];
export const PAGINATION_PARAMS = ['limit', 'cursor', 'after', 'offset'];
export const DEV_FLAG = 'import.meta.env.DEV';

/** Default input/output locations, relative to the repository root. */
export const DEFAULT_PATHS = {
  openapi: path.join('backend', 'docs', 'openapi.yaml'),
  router: path.join('backend', 'internal', 'api', 'router.go'),
  generatedTypes: path.join('apps', 'workbench', 'generated', 'openapi-types.ts'),
  outJson: path.join('apps', 'workbench', 'generated', 'endpoint-map.json'),
  outMd: path.join('apps', 'workbench', 'generated', 'endpoint-map.md'),
  scanDir: path.join('apps', 'workbench'),
  oldClientDirs: [path.join('apps', 'workbench', 'services')],
  bridgeDirs: [path.join('apps', 'workbench', 'src', 'services-bridge')],
  sharedTransportFiles: [path.join('apps', 'workbench', 'api.ts')],
};

// ---------------------------------------------------------------------------
// resolution helpers (existing repo dependencies, same lookup as the yaml one)
// ---------------------------------------------------------------------------

/**
 * `node_modules/.pnpm` directories to search, nearest first. A linked git
 * worktree has no node_modules of its own, so the ancestor checkboxes let the
 * script use the main checkout's store without creating links or directories
 * inside the worktree (and it adds no dependency: yaml and typescript are
 * already repo dependencies, resolved exactly like generate-openapi-types.mjs).
 */
function pnpmRoots() {
  const roots = [];
  let current = repoRoot;
  for (let i = 0; i < 6; i += 1) {
    const candidate = path.join(current, 'node_modules', '.pnpm');
    if (fs.existsSync(candidate)) roots.push(candidate);
    const parent = path.dirname(current);
    if (parent === current) break;
    current = parent;
  }
  return roots;
}

function pnpmCandidates(packageName, marker) {
  const found = [];
  for (const pnpmDir of pnpmRoots()) {
    for (const entry of fs.readdirSync(pnpmDir, { withFileTypes: true })) {
      if (!entry.isDirectory() || !entry.name.startsWith(`${packageName}@`)) continue;
      const dir = path.join(pnpmDir, entry.name, 'node_modules', packageName);
      if (marker && !fs.existsSync(path.join(dir, marker))) continue;
      if (!marker && !fs.existsSync(dir)) continue;
      found.push(dir);
    }
  }
  return found.sort((a, b) => b.localeCompare(a, undefined, { numeric: true }));
}

/** Test seam: when set, `loadTypescript()` returns this instead of probing. */
export const typescriptOverride = { value: null };

/** Test seam: `null` probes the real resolution, a function maps it. */
export let typescriptResolver = null;

export function setTypescriptResolver(fn) {
  typescriptResolver = fn;
}

/**
 * Load the TypeScript compiler API.
 *
 * Fail closed: the ledger must be parsed with the TypeScript compiler, so a
 * missing compiler is a hard error. There is deliberately no regex fallback -
 * two extractors would mean two different ledgers for one checkout.
 */
export function loadTypescript() {
  if (typescriptOverride.value !== null) return typescriptOverride.value;
  const found = (typescriptResolver ?? probeTypescript)();
  if (!found) {
    throw new Error(
      'TypeScript compiler not found. scripts/frontend-endpoint-map.mjs needs the `typescript` ' +
        'package (a repo dependency, resolved from the checkout or an ancestor node_modules/.pnpm ' +
        'store) and refuses to fall back to regex heuristics, because a coarser extractor would ' +
        'produce a different ledger for the same checkout. Install dependencies (pnpm install) ' +
        'and re-run; no output was written.',
    );
  }
  return found;
}

function probeTypescript() {
  // Test seam: CODEFLOW_NO_TYPESCRIPT=1 simulates a checkout without the
  // compiler so the fail-closed exit path can be exercised without deleting
  // dependencies (scripts/frontend-endpoint-map.test.mjs).
  if (process.env.CODEFLOW_NO_TYPESCRIPT === '1') return null;
  try {
    return repoRequire('typescript');
  } catch {
    /* pnpm layout: typescript is not hoisted to the worktree root */
  }
  for (const dir of pnpmCandidates('typescript', path.join('lib', 'typescript.js'))) {
    try {
      return repoRequire(dir);
    } catch {
      /* try the next copy */
    }
  }
  return null;
}

/** Load the yaml parser the way generate-openapi-types.mjs does. */
export function loadYaml() {
  try {
    return repoRequire('yaml');
  } catch {
    /* fall through to the pnpm store */
  }
  const candidates = pnpmCandidates('yaml');
  if (candidates.length === 0) throw new Error('yaml package not found; cannot parse openapi.yaml');
  return repoRequire(candidates[0]);
}

// ---------------------------------------------------------------------------
// deterministic output helpers
// ---------------------------------------------------------------------------

export function toPosix(value) {
  return String(value).split(path.sep).join('/');
}

export function sortStrings(values) {
  return [...new Set(values.filter((v) => v !== null && v !== undefined))].sort((a, b) =>
    a < b ? -1 : a > b ? 1 : 0,
  );
}

export function isTestPath(file) {
  return /\.(test|spec)\.[cm]?tsx?$/.test(file) || /(^|\/)(__tests__|__mocks__)(\/|$)/.test(file);
}

/** JSON.stringify with every object key sorted, so two runs are byte-equal. */
export function stableStringify(value) {
  const walk = (node) => {
    if (Array.isArray(node)) return node.map(walk);
    if (node && typeof node === 'object') {
      const out = {};
      for (const key of Object.keys(node).sort()) out[key] = walk(node[key]);
      return out;
    }
    return node;
  };
  return `${JSON.stringify(walk(value), null, 2)}\n`;
}

/** Drop `undefined` members so the json stays byte-comparable across runs. */
export function pruneUndefined(value) {
  if (Array.isArray(value)) return value.map(pruneUndefined);
  if (value && typeof value === 'object') {
    const out = {};
    for (const [key, item] of Object.entries(value)) {
      if (item === undefined) continue;
      out[key] = pruneUndefined(item);
    }
    return out;
  }
  return value;
}

export function uniqBy(items, key) {
  const seen = new Map();
  for (const item of items) {
    const id = item[key];
    if (!seen.has(id)) seen.set(id, item);
  }
  return [...seen.values()].sort((a, b) => (a[key] < b[key] ? -1 : a[key] > b[key] ? 1 : 0));
}

export function walkFiles(dir, extensions) {
  const out = [];
  if (!fs.existsSync(dir)) return out;
  const stack = [dir];
  while (stack.length > 0) {
    const current = stack.pop();
    const entries = fs.readdirSync(current, { withFileTypes: true }).sort((a, b) =>
      a.name < b.name ? -1 : a.name > b.name ? 1 : 0,
    );
    for (const entry of entries) {
      if (entry.name === 'node_modules' || entry.name === 'dist') continue;
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) stack.push(full);
      else if (extensions.some((ext) => entry.name.endsWith(ext))) out.push(full);
    }
  }
  return sortStrings(out);
}

// ---------------------------------------------------------------------------
// path / route helpers
// ---------------------------------------------------------------------------

/** `/api/v1/plans/{id}` -> /^\/api\/v1\/plans\/[^/]+\/?$/ */
export function templateToRegex(template) {
  const escaped = template
    .split('/')
    .map((segment) =>
      /^\{[^}]+\}$/.test(segment) ? '[^/]+' : segment.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'),
    )
    .join('/');
  return new RegExp(`^${escaped}/?$`);
}

/** Compare paths by shape: parameters collapse to `{}`. */
export function shapeOf(value) {
  return String(value).replace(/\{[^}]*\}/g, '{}');
}

/**
 * Go route params (`:id`) and wildcards (`*filepath`) are OpenAPI templates
 * (`{id}` / `{filepath}`); a trailing slash is not part of the shape.
 */
export function normalizeColonParams(routePath) {
  return String(routePath)
    .replace(/:([A-Za-z_]\w*)/g, '{$1}')
    .replace(/\*([A-Za-z_]\w*)/g, '{$1}')
    .replace(/\/+$/, '') || '/';
}

/** Collapse repeated slashes; an absolute URL keeps its scheme after `//`. */
export function normalizePath(value) {
  const text = String(value);
  const scheme = text.match(/^([a-z][a-z0-9+.-]*:\/\/)([\s\S]*)$/i);
  if (scheme) return `${scheme[1]}${normalizePath(scheme[2])}`;
  const collapsed = text.replace(/\/{2,}/g, '/').replace(/\/+$/, '');
  return collapsed === '' ? '/' : collapsed;
}

const PROJECT_MARKERS = new Set(['projects', 'project', 'pid', 'projectid']);

/**
 * Path-based project scope, evaluated on concrete path segments only: a
 * template placeholder always counts, a literal never does.
 */
export function projectScopeOf(pathTemplate) {
  if (!pathTemplate) return 'unknown';
  const segments = pathTemplate.split('/').filter(Boolean);
  for (let i = 0; i < segments.length; i += 1) {
    const lower = segments[i].toLowerCase();
    if (PROJECT_MARKERS.has(lower) || lower.startsWith('project_')) {
      const next = segments[i + 1];
      if (next === undefined) return 'project';
      if (/^\{[^}]+\}$/.test(next)) return 'project';
      // A literal after the marker is not a project id (e.g. /projects/marketplace).
    }
  }
  return 'global';
}

/**
 * Strip a URL down to its path template: a truthiness test
 * (`if (url) get(`...${url}`)`) or a defaulted parameter (`(url = '')`) must
 * not leak the guard text into the expression, or the ledger would read an
 * unresolvable URL where the URL is plainly there. Returns the input when the
 * expression still contains operators that carry meaning (`??`, `||`, `!`), so
 * `a ?? b` is *not* silently truncated.
 */
export function urlExpressionOf(raw) {
  let text = String(raw ?? '').trim().replace(/;$/, '').trim();
  if (text === '') return text;
  // `(url = '')` -> `url`: a defaulted parameter initializer, not a comparison.
  const defaulted = text.match(/^(\s*)([A-Za-z_$][\w$]*)(\s*=\s*)(['"`][^'"`]*['"`])\s*$/);
  if (defaulted) return defaulted[2];
  // `if (url)` / `url && ...` -> the URL itself is the tested value.
  text = text.replace(/^\s*if\s*\(\s*([^()]*?)\s*\)\s*$/, '$1').trim();
  if (/^(?:[A-Za-z_$][\w$]*|[A-Za-z_$][\w$]*\.[\w$]+)$/.test(text)) return text;
  return text;
}

export function ruleOf(pathTemplate) {
  const segments = String(pathTemplate ?? '').split('/').filter(Boolean);
  if (segments[0] === 'api' && segments[1]) return `/${segments.slice(0, 2).join('/')}${segments[2] ? `/${segments[2]}` : ''}`;
  return segments.length > 0 ? `/${segments[0]}` : '/';
}

// ---------------------------------------------------------------------------
// router.go: route registrations with resolved group prefixes
// ---------------------------------------------------------------------------

// Receivers are often dotted (`s.router`, `server.router`); only the *tail*
// identifies the group, so the alias is captured without its receiver.
const ROUTER_GROUP_RE = /^([A-Za-z_]\w*)\s*:?=\s*([A-Za-z_][\w.]*)\.Group\(\s*(?:"([^"]*)"|`([^`]*)`)\s*\)/;
const ROUTER_ROUTE_RE =
  /^([A-Za-z_][\w.]*)\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|ANY)\(\s*(?:"([^"]*)"|`([^`]*)`)\s*,\s*([^)]+)\)/;

/** The variable a receiver expression ends in: `s.router` -> `router`. */
function receiverTail(receiverText) {
  return String(receiverText).trim().split('.').pop();
}

/**
 * Parse router.go route registrations.
 *
 * Group aliases resolve through a variable table rooted at the server router,
 * so the group prefix is part of every path: `flows := v1.Group("/flows")`
 * plus `flows.POST("/:id/loop")` yields `POST /api/v1/flows/:id/loop`.
 * The root is `s.router` (the server's field), registered as `router`.
 */
export function parseRouterRoutes(source) {
  const routes = [];
  const groups = new Map([['router', '']]);
  for (const rawLine of source.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (line.startsWith('//') || line.startsWith('*')) continue;
    const group = line.match(ROUTER_GROUP_RE);
    if (group) {
      const alias = group[1];
      const parent = receiverTail(group[2]);
      const prefix = group[3] ?? group[4] ?? '';
      if (groups.has(parent)) groups.set(alias, `${groups.get(parent)}${prefix}`);
      continue;
    }
    const route = line.match(ROUTER_ROUTE_RE);
    if (!route) continue;
    const alias = receiverTail(route[1]);
    const method = route[2];
    const subPath = route[3] ?? route[4] ?? '';
    const handlerRaw = route[5];
    if (!groups.has(alias)) continue;
    const prefix = groups.get(alias);
    const fullPath = normalizePath(`${prefix}${subPath}` === '' ? '/' : `${prefix}${subPath}`);
    // The handler is the last argument: middleware may sit in front of it
    // (`router.GET("/ready", auth, handlers.ReadinessCheck)`).
    const args = splitArguments(handlerRaw);
    routes.push({
      method,
      path: normalizeColonParams(fullPath),
      handler: (args[args.length - 1] ?? handlerRaw).trim(),
      raw: `${prefix}${subPath}`,
    });
  }
  return routes.sort((a, b) =>
    `${a.method} ${a.path}` < `${b.method} ${b.path}`
      ? -1
      : `${a.method} ${a.path}` > `${b.method} ${b.path}`
        ? 1
        : 0,
  );
}

// ---------------------------------------------------------------------------
// OpenAPI contract
// ---------------------------------------------------------------------------

function contentTypesOf(operation) {
  const types = new Set();
  const collect = (content) => {
    if (content && typeof content === 'object') for (const type of Object.keys(content)) types.add(type);
  };
  if (operation.requestBody && typeof operation.requestBody === 'object') collect(operation.requestBody.content);
  for (const response of Object.values(operation.responses ?? {})) {
    if (response && typeof response === 'object') collect(response.content);
  }
  return sortStrings([...types]);
}

export function parseOpenApiDocument(doc) {
  const operations = new Map(); // "METHOD /path" -> operation
  for (const [apiPath, item] of Object.entries(doc.paths ?? {})) {
    if (!item || typeof item !== 'object') continue;
    const pathExperimental = item['x-codeflow-experimental'] === true;
    for (const [key, value] of Object.entries(item)) {
      if (!HTTP_METHODS.includes(key) || !value || typeof value !== 'object') continue;
      const parameters = Array.isArray(value.parameters) ? value.parameters : [];
      const contentTypes = contentTypesOf(value);
      operations.set(`${key.toUpperCase()} ${apiPath}`, {
        method: key.toUpperCase(),
        path: apiPath,
        operation_id: typeof value.operationId === 'string' ? value.operationId : null,
        summary: typeof value.summary === 'string' ? value.summary : null,
        experimental: value['x-codeflow-experimental'] === true || pathExperimental,
        security: Array.isArray(value.security) ? value.security : null,
        content_types: contentTypes,
        response_statuses: sortStrings(Object.keys(value.responses ?? {})),
        header_params: parameters.filter((p) => p && p.in === 'header').map((p) => String(p.name)),
        query_params: parameters.filter((p) => p && p.in === 'query').map((p) => String(p.name)),
      });
    }
  }
  return { operations };
}

/** Map a contract's declared content types onto the transport vocabulary. */
export function contractTransportOf(contentTypes) {
  for (const type of contentTypes) {
    if (type === 'text/event-stream') return 'sse';
    if (type === 'multipart/form-data') return 'multipart';
    if (type === 'application/octet-stream' || type === 'application/zip' || type.startsWith('application/vnd.')) {
      return 'download';
    }
  }
  return null;
}

/**
 * The generated types file keys every operation by method+path; reading it
 * back ties each endpoint to the generator's own contract. The generator
 * emits `'key': { method: 'GET'; path: '/x' };` (the `};` closes the object
 * literal and the entry in one token), so the match is `' }\s*;`, not `'; }`.
 */
export function parseGeneratedOperationMap(source) {
  const byOp = new Map();
  const block = source.match(/export interface OpenApiOperationMap \{([\s\S]*?)\n\}/);
  if (!block) return byOp;
  const entryRe = /'([^']+)':\s*\{\s*method:\s*'([A-Z]+)';\s*path:\s*'([^']+)'\s*\};/g;
  let match;
  while ((match = entryRe.exec(block[1])) !== null) {
    byOp.set(`${match[2]} ${match[3]}`, match[1]);
  }
  return byOp;
}

/**
 * Wire the generated operation map onto the entries: `generated_operation`
 * names the key the generator emits for this endpoint, so a ledger row and
 * `openapi-types.ts` can be cross-checked without re-parsing either. A
 * contract operation the generator skipped (or a frontend-only path) simply
 * carries no key - that is a fact, not an unknown.
 */
export function attachGeneratedOperations(entries, generatedOps) {
  for (const entry of entries) {
    if (entry.openapi === 'missing' || entry.method === undefined) continue;
    const key = generatedOps.get(`${entry.method} ${entry.path}`);
    if (key) entry.generated_operation = key;
  }
}

/**
 * The WS capability table in the generated types: transports the frontend may
 * use per endpoint class. Kept for the report so the .json explains why a
 * websocket entry can never be migrated as ordinary JSON.
 */
export function parseGeneratedWsTransports(source) {
  const names = [];
  const block = source.match(/export interface OpenApiWsTransports \{([\s\S]*?)\n\}/);
  if (block) {
    const re = /^\s{2}([A-Za-z_$][\w$]*)\??:/gm;
    let match;
    while ((match = re.exec(block[1])) !== null) names.push(match[1]);
  }
  return sortStrings(names);
}

// ---------------------------------------------------------------------------
// schema coverage (T12.03.a: nullable/union/required/enum/pagination)
// ---------------------------------------------------------------------------

export function countOpenApiSchemaFeatures(doc) {
  const counts = {
    nullable: 0,
    one_of: 0,
    any_of: 0,
    all_of: 0,
    required_properties: 0,
    enum_sites: 0,
    enum_members: 0,
    pagination_params: 0,
  };
  const paginationSites = [];
  const walk = (node) => {
    if (Array.isArray(node)) {
      for (const item of node) walk(item);
      return;
    }
    if (!node || typeof node !== 'object') return;
    if (node.nullable === true) counts.nullable += 1;
    if (Array.isArray(node.oneOf)) counts.one_of += 1;
    if (Array.isArray(node.anyOf)) counts.any_of += 1;
    if (Array.isArray(node.allOf)) counts.all_of += 1;
    if (Array.isArray(node.required)) counts.required_properties += node.required.length;
    if (Array.isArray(node.enum)) {
      counts.enum_sites += 1;
      counts.enum_members += node.enum.length;
    }
    for (const value of Object.values(node)) walk(value);
  };
  walk(doc.components?.schemas ?? {});
  walk(doc.paths ?? {});

  for (const [apiPath, item] of Object.entries(doc.paths ?? {})) {
    if (!item || typeof item !== 'object') continue;
    for (const [key, value] of Object.entries(item)) {
      if (!HTTP_METHODS.includes(key) || !value || typeof value !== 'object') continue;
      for (const parameter of Array.isArray(value.parameters) ? value.parameters : []) {
        if (parameter && PAGINATION_PARAMS.includes(String(parameter.name))) {
          counts.pagination_params += 1;
          paginationSites.push(`${key.toUpperCase()} ${apiPath}?${parameter.name}`);
        }
      }
    }
  }
  return { counts, paginationSites: sortStrings(paginationSites) };
}

/** What the generated types actually express, read with the TypeScript AST. */
export function countGeneratedTypeFeatures(ts, source, filePath = 'openapi-types.ts') {
  const file = ts.createSourceFile(filePath, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const counts = {
    nullable: 0,
    union_types: 0,
    required_properties: 0,
    optional_properties: 0,
    enum_literal_members: 0,
    pagination_properties: 0,
  };
  const paginationNames = new Set();
  const booleanLike = new Set(['boolean', 'true', 'false']);

  // `null` parses as a LiteralType wrapping NullKeyword, not as a bare
  // NullKeyword, so both the keyword and the literal form count.
  const isNullMember = (member) =>
    member.kind === ts.SyntaxKind.NullKeyword ||
    (ts.isLiteralTypeNode(member) && member.literal.kind === ts.SyntaxKind.NullKeyword);

  const visitType = (node) => {
    if (!node) return;
    if (ts.isUnionTypeNode(node)) {
      const members = node.types;
      const literalMembers = members.filter(
        (m) =>
          ts.isLiteralTypeNode(m) &&
          m.literal.kind !== ts.SyntaxKind.NullKeyword &&
          !booleanLike.has(m.literal.getText().replace(/['"]/g, '')),
      );
      const sawNull = members.some(isNullMember);
      if (sawNull) counts.nullable += 1;
      const withoutNull = members.filter((m) => !isNullMember(m));
      const allLiterals = withoutNull.every((m) => ts.isLiteralTypeNode(m));
      if (allLiterals && literalMembers.length > 0) counts.enum_literal_members += literalMembers.length;
      else if (!allLiterals && withoutNull.length > 1) counts.union_types += 1;
      for (const member of members) visitType(member);
      return;
    }
    if (ts.isTypeLiteralNode(node) || ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) {
      if (ts.isTypeAliasDeclaration(node)) {
        visitType(node.type);
        return;
      }
      for (const member of node.members ?? []) {
        if (!ts.isPropertySignature(member) || !member.name) continue;
        const name = member.name.getText().replace(/['"]/g, '');
        if (member.questionToken) counts.optional_properties += 1;
        else counts.required_properties += 1;
        if (PAGINATION_PARAMS.includes(name)) {
          counts.pagination_properties += 1;
          paginationNames.add(name);
        }
        visitType(member.type);
      }
      return;
    }
    if (ts.isArrayTypeNode(node)) return visitType(node.elementType);
    if (ts.isParenthesizedTypeNode(node)) return visitType(node.type);
    if (ts.isIntersectionTypeNode(node)) {
      for (const member of node.types) visitType(member);
      return;
    }
    if (ts.isTupleTypeNode(node)) {
      for (const element of node.elements) visitType(element);
      return;
    }
    if (
      ts.isLiteralTypeNode(node) &&
      (node.literal.kind === ts.SyntaxKind.NullKeyword || node.literal.kind === ts.SyntaxKind.UndefinedKeyword)
    ) {
      return;
    }
    ts.forEachChild(node, visitType);
  };

  for (const statement of file.statements) {
    if (ts.isInterfaceDeclaration(statement) && /^OpenApi(OperationMap|Schemas|WsTransports)$/.test(statement.name.text)) {
      continue;
    }
    if (ts.isTypeAliasDeclaration(statement) || ts.isInterfaceDeclaration(statement)) visitType(statement);
  }
  return { counts, paginationNames: sortStrings([...paginationNames]) };
}

export function schemaCoverageFrom(openapi, generated) {
  const measures = [
    {
      area: 'nullable',
      openapi: openapi.counts.nullable,
      generated: generated.counts.nullable,
      openapi_unit: '`nullable: true` schema nodes',
      generated_unit: 'unions that include `null`',
    },
    {
      area: 'union',
      openapi: openapi.counts.one_of + openapi.counts.any_of,
      generated: generated.counts.union_types,
      openapi_unit: '`oneOf` + `anyOf` arrays',
      generated_unit: 'multi-member unions that are not literal-only',
    },
    {
      area: 'required',
      openapi: openapi.counts.required_properties,
      generated: generated.counts.required_properties,
      openapi_unit: 'names inside `required:` lists',
      generated_unit: 'non-optional emitted members',
    },
    {
      area: 'enum',
      openapi: openapi.counts.enum_members,
      generated: generated.counts.enum_literal_members,
      openapi_unit: '`enum:` members',
      generated_unit: 'members of literal unions',
    },
    {
      area: 'pagination',
      openapi: openapi.counts.pagination_params,
      generated: generated.counts.pagination_properties,
      openapi_unit: 'limit/cursor/after/offset query parameters',
      generated_unit: 'limit/cursor/after/offset emitted properties',
    },
  ];
  const mismatches = [];
  for (const measure of measures) {
    if (measure.openapi !== measure.generated) {
      mismatches.push({
        area: measure.area,
        openapi: measure.openapi,
        generated: measure.generated,
        direction: measure.generated < measure.openapi ? 'generated_undercounts' : 'generated_overcounts',
        why: mismatchReason(measure.area),
      });
    }
  }
  return {
    method:
      'openapi.yaml is walked as parsed YAML; generated/openapi-types.ts is walked with the TypeScript AST. The two counters are independent populations - a mismatch is reported as a fact, and this script never edits the generator.',
    measures: measures.map((m) => ({ ...m, matches: m.openapi === m.generated })),
    mismatches,
    pagination: {
      openapi_sites: openapi.paginationSites,
      generated_property_names: generated.paginationNames,
    },
  };
}

function mismatchReason(area) {
  switch (area) {
    case 'nullable':
      return 'the contract marks 5 schemas `nullable: true`; the generated file writes `| null` for 4 of them and collapses the fifth (`LiteralValue.@value`, which has no `type:`) to `unknown`, so one nullable site loses its null at the consumer boundary.';
    case 'union':
      return 'the generator emits `A | B` for `oneOf`/`anyOf`; a `oneOf` nested in an array or response object adds a contract site without adding a top-level union, so the two populations coincide here only by counting method.';
    case 'required':
      return 'the contract counts every name in a `required:` list anywhere in the document (including query/path parameters); the generated file emits one non-optional member per property of an exported object schema, so parameter-level requireds have no generated member to land on.';
    case 'enum':
      return 'the contract counts `enum:` members in every schema; the generator emits a literal union per enum property and skips boolean-like literals, so reused or nested enums are emitted once per use and the member counts diverge.';
    case 'pagination':
      return 'a pagination parameter is a query parameter in the contract and a property of whichever generated schema models that response, so names overlap without being the same population (only `limit` and `after` appear as generated properties).';
    default:
      return '';
  }
}

// ---------------------------------------------------------------------------
// URL expression resolution
// ---------------------------------------------------------------------------

const MAX_RESOLVE_DEPTH = 16;

/** Split a template literal into ordered `{kind:'text'|'expr'}` parts. */
export function splitTemplate(text) {
  if (!text.startsWith('`')) return null;
  const parts = [];
  let i = 1;
  let buffer = '';
  while (i < text.length) {
    const ch = text[i];
    if (ch === '`') break;
    if (ch === '\\') {
      buffer += text[i + 1] ?? '';
      i += 2;
      continue;
    }
    if (ch === '$' && text[i + 1] === '{') {
      let depth = 1;
      let j = i + 2;
      let inner = '';
      while (j < text.length && depth > 0) {
        if (text[j] === '{') depth += 1;
        else if (text[j] === '}') {
          depth -= 1;
          if (depth === 0) break;
        }
        inner += text[j];
        j += 1;
      }
      if (buffer) {
        parts.push({ kind: 'text', value: buffer });
        buffer = '';
      }
      parts.push({ kind: 'expr', value: inner });
      i = j + 1;
      continue;
    }
    buffer += ch;
    i += 1;
  }
  if (buffer) parts.push({ kind: 'text', value: buffer });
  return parts;
}

const BASE_CALL_RE = /^(?:[A-Za-z_$][\w$]*\.)?(getApiBase|getWsBase)\(\)$/;
// Wrappers that only decorate an argument: the argument is the endpoint.
const RESOLVABLE_WRAPPERS = new Set(['String', 'stringify', 'encodeURIComponent', 'encodeURI']);
const SIMPLE_IDENTIFIER_RE = /^[A-Za-z_$][\w$]*$/;
const SIMPLE_PATH_RE = /^[A-Za-z_$][\w$]*(\.[\w$]+)+$/;

/** Split on a top-level operator, ignoring strings, templates and brackets. */
export function splitTopLevel(text, operator) {
  let depth = 0;
  let quote = null;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      if (ch === '\\') i += 1;
      else if (ch === quote) quote = null;
      continue;
    }
    if (ch === "'" || ch === '"' || ch === '`') {
      quote = ch;
      continue;
    }
    if (ch === '(' || ch === '[' || ch === '{') depth += 1;
    else if (ch === ')' || ch === ']' || ch === '}') depth -= 1;
    else if (depth === 0 && text.startsWith(operator, i)) {
      return [text.slice(0, i), text.slice(i + operator.length)];
    }
  }
  return null;
}

/**
 * Resolve a URL expression against one file's bindings.
 *
 * The rules, in order: `a ?? b` / `a || b` prefer the fallback side; a local
 * binding (const, helper function, endpoint table member, class field) is
 * inlined; `getApiBase()`/`getWsBase()` are the origin; a function parameter
 * in a path or query position becomes a `{param}` placeholder; a string
 * literal is the path. Anything else is `unknown`: a dynamic URL is reported,
 * never guessed.
 */
export function resolvePath(raw, env, depth = 0) {
  if (depth > MAX_RESOLVE_DEPTH) return { path: null, kind: 'unknown', raw: String(raw), note: 'resolution depth exceeded' };
  const text = String(raw ?? '').trim().replace(/;$/, '').trim();
  if (text === '') return { path: null, kind: 'unknown', raw: text, note: 'empty expression' };

  for (const operator of ['??', '||']) {
    const split = splitTopLevel(text, operator);
    if (split) {
      const right = resolvePath(split[1], env, depth + 1);
      if (right.kind === 'resolved') return { ...right, raw: text };
      const left = resolvePath(split[0], env, depth + 1);
      return { ...left, raw: text };
    }
  }

  if (BASE_CALL_RE.test(text)) {
    return { path: normalizePath(`${env.base}/`), kind: 'resolved', raw: text };
  }
  if (env.binds?.has(text)) {
    const bind = env.binds.get(text);
    if (bind.kind === 'alias' && bind.text !== text) {
      const followed = resolvePath(bind.text, env, depth + 1);
      return { ...followed, raw: text };
    }
    return { ...substituteBind(bind, [], [], env, depth), raw: text };
  }
  if (env.crossFile) {
    const dotted = text.match(/^([A-Za-z_$][\w$]*)\.([\w$]+)$/);
    if (dotted) {
      const foreign = env.crossFile(dotted[1], dotted[2]);
      if (foreign) return { ...resolvePath(foreign.text, { ...env, binds: foreign.binds }, depth + 1), raw: text };
    }
  }
  const call = matchCall(text);
  if (call) {
    const localBind = env.binds?.get(call.callee);
    if (localBind && localBind.params) {
      return { ...substituteBind(localBind, localBind.params, call.args, env, depth), raw: text };
    }
    if (env.crossFile) {
      const dotted = call.callee.match(/^([A-Za-z_$][\w$]*)\.([\w$]+)$/);
      if (dotted) {
        const foreign = env.crossFile(dotted[1], dotted[2]);
        if (foreign && foreign.params) {
          return { ...substituteBind(foreign, foreign.params, call.args, { ...env, binds: foreign.binds }, depth), raw: text };
        }
      }
    }
    if (RESOLVABLE_WRAPPERS.has(call.callee)) {
      return resolvePath(call.args[0] ?? '', env, depth + 1);
    }
  }

  if (/^['"]/.test(text)) {
    const literal = text.slice(1, -1);
    if (/^https?:\/\//.test(literal)) {
      // A loopback literal (`http://127.0.0.1:41234/...`) is a local harness
      // origin, not a third-party API: keep the path so the call is still
      // attributable. External providers stay unknown.
      if (!isLoopbackUrl(literal)) {
        return { path: null, kind: 'unknown', raw: text, note: 'absolute url literal outside the api base' };
      }
      return { path: normalizePath(literal.replace(/^[a-z][a-z0-9+.-]*:\/\/[^/]*/i, '')), kind: 'resolved', raw: text, note: 'loopback origin' };
    }
    return { path: normalizePath(literal), kind: 'resolved', raw: text };
  }

  const parts = splitTemplate(text);
  if (!parts) {
    if (env.params?.has(text)) return placeholder(text, 'path', text);
    return { path: null, kind: 'unknown', raw: text, note: 'not a string template' };
  }

  let out = '';
  let note = null;
  for (const part of parts) {
    if (part.kind === 'text') {
      out += part.value;
      continue;
    }
    const inner = part.value.trim();
    if (BASE_CALL_RE.test(inner)) {
      out += env.base;
      continue;
    }
    if (env.binds?.has(inner)) {
      const resolved = resolvePath(inner, env, depth + 1);
      if (resolved.kind === 'unknown') return { path: null, kind: 'unknown', raw: text, note: `unresolved part: ${inner}` };
      out += resolved.path;
      continue;
    }
    const innerCall = matchCall(inner);
    if (innerCall) {
      const resolvedInner = resolvePath(inner, env, depth + 1);
      if (resolvedInner.kind === 'resolved') {
        out += resolvedInner.path;
        continue;
      }
      if (isQueryPosition(out)) {
        // The query member is recorded by its bare name; the wrapper
        // (`encodeURIComponent`, an api helper) is not part of the key.
        out += `{${innerCall.callee.split('.').pop()}}`;
        note = note ?? 'query helper not inlined';
        continue;
      }
      if (isPathPosition(out)) {
        // Same for a path segment: `encodeURIComponent(projectId)` names the
        // placeholder even though the encoder itself is not inlined.
        const name = queryValueName(inner);
        if (name) {
          out += `{${name}}`;
          note = note ?? `wrapped path value: ${inner}`;
          continue;
        }
      }
      return { path: null, kind: 'unknown', raw: text, note: `unresolved part: ${inner}` };
    }
    if (isQueryPosition(out) || isPathPosition(out)) {
      // `id` and `pc.id` both name the segment. A table member is inlined
      // through the binding table first, so `API_ENDPOINTS.projects` keeps the
      // concrete `/api/v1/projects`; only a table that is genuinely unknown
      // falls through to the placeholder.
      if (env.binds?.has(inner)) {
        const resolved = resolvePath(inner, env, depth + 1);
        if (resolved.kind === 'resolved' && resolved.path !== null) {
          out += resolved.path;
          continue;
        }
      }
      // A dotted member whose head is an *import* (`API_ENDPOINTS.projects`,
      // `endpoints.workspace`) is a table member the import-aware pass can
      // still inline; the in-file snapshot only holds this file's own
      // bindings. Record it so the second pass substitutes the concrete path
      // instead of the ledger keying the endpoint as `{API_ENDPOINTS.projects}`
      // - a placeholder that makes a live client look like a call to a route
      // the backend never registered. A dotted member of a *parameter* object
      // (`pc.id`) is not an import and keeps its `{id}` placeholder.
      const head = inner.includes('.') ? inner.slice(0, inner.indexOf('.')) : inner;
      if (SIMPLE_PATH_RE.test(inner) && env.imports?.has(head)) {
        env.unresolvedParts?.push(inner);
        out += `{${inner}}`;
        note = note ?? `table member not inlined in-file: ${inner}`;
        continue;
      }
      const name = queryValueName(inner) ?? null;
      if (name && (SIMPLE_IDENTIFIER_RE.test(name) || env.params?.has(name))) {
        out += `{${name}}`;
        continue;
      }
      if (isQueryPosition(out)) {
        out += '{param}';
        note = note ?? 'unresolved query value';
        continue;
      }
      if (isPathPosition(out)) {
        // A dotted member in a path position is a table member
        // (`API_ENDPOINTS.projects`, `endpoints.workspace`). When the table is
        // not in this file's binding snapshot (it was imported, and only the
        // import-aware pass can inline it) the name is recorded on the
        // resolution so the second pass can still substitute the concrete
        // path; emitting `{projects}` here would be a guess AND it would make
        // a live client look like a call to an unregistered route.
        if (SIMPLE_PATH_RE.test(inner)) {
          note = note ?? `table member not inlined in-file: ${inner}`;
          if (env.unresolvedParts) env.unresolvedParts.push(inner);
          out += `{${inner}}`;
          continue;
        }
        out += '{param}';
        note = note ?? `unresolved path segment: ${inner}`;
        continue;
      }
    }
    // A trailing conditional that only decorates the query string keeps the
    // path we already resolved; the query is not part of the endpoint key.
    if (out.length > 0 && /[?&]/.test(inner)) {
      note = note ?? `query suffix not inlined: ${inner}`;
      break;
    }
    return { path: null, kind: 'unknown', raw: text, note: `unresolved part: ${inner}` };
  }
  const resolved = normalizePath(out);
  return {
    path: resolved,
    kind: 'resolved',
    raw: text,
    note,
    // Dotted table members the in-file snapshot could only placeholder; the
    // caller re-resolves with the import-aware binding table and drops the row
    // to `unknown` if that still fails.
    unresolvedParts: env.unresolvedParts?.length > 0 ? [...env.unresolvedParts] : undefined,
  };
}

/** localhost / 127.0.0.1 / [::1] / *.localhost, any port. */
export function isLoopbackUrl(url) {
  const host = String(url).match(/^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i)?.[1] ?? '';
  const name = host.replace(/^\[|\]$/g, '').split(':')[0].toLowerCase();
  return name === 'localhost' || name === '127.0.0.1' || name === '::1' || name === '0.0.0.0' || name.endsWith('.localhost');
}

function isPathPosition(out) {
  return out === '' || out.endsWith('/');
}

function isQueryPosition(out) {
  return /[?&=]$/.test(out);
}

function queryValueName(inner) {
  const encoded = inner.match(/^encodeURIComponent\s*\(\s*([^)]+?)\s*\)$/);
  const value = encoded ? encoded[1].trim() : inner;
  if (SIMPLE_IDENTIFIER_RE.test(value)) return value;
  if (SIMPLE_PATH_RE.test(value)) return value.split('.').pop();
  return null;
}

function placeholder(name, position, raw) {
  return { path: `{${name}}`, kind: 'resolved', raw, note: `${position} placeholder` };
}

function matchCall(text) {
  const match = text.match(/^([\w$.]+)\s*\(([\s\S]*)\)$/);
  if (!match) return null;
  return { callee: match[1], args: splitArguments(match[2]) };
}

function splitArguments(text) {
  const args = [];
  let depth = 0;
  let quote = null;
  let current = '';
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      current += ch;
      if (ch === '\\') {
        current += text[i + 1] ?? '';
        i += 1;
      } else if (ch === quote) quote = null;
      continue;
    }
    if (ch === "'" || ch === '"' || ch === '`') {
      quote = ch;
      current += ch;
      continue;
    }
    if (ch === '(' || ch === '[' || ch === '{') depth += 1;
    else if (ch === ')' || ch === ']' || ch === '}') depth -= 1;
    if (ch === ',' && depth === 0) {
      args.push(current.trim());
      current = '';
      continue;
    }
    current += ch;
  }
  if (current.trim() !== '') args.push(current.trim());
  return args;
}

function substituteBind(bind, params, args, env, depth) {
  let body = bind.text;
  for (let i = 0; i < params.length; i += 1) {
    const argument = args[i] ?? '';
    if (argument === '') continue;
    body = body.replace(new RegExp(`\\b${params[i]}\\b`, 'g'), argument);
  }
  const nestedEnv = { ...env, params: new Map([...params.map((p, i) => [p, args[i] ?? p]), ...(env.params ?? [])]) };
  const resolved = resolvePath(body, nestedEnv, depth + 1);
  return resolved;
}

/** Split `path?query` and keep the query parameter names (data, not scope). */
export function splitQuery(value) {
  const index = value.indexOf('?');
  if (index < 0) return { path: value, query: [] };
  const query = value
    .slice(index + 1)
    .split('&')
    .map((pair) => pair.split('=')[0].replace(/[{}]/g, '').trim())
    .filter(Boolean);
  return { path: value.slice(0, index), query: sortStrings(query) };
}

// ---------------------------------------------------------------------------
// TypeScript file model
// ---------------------------------------------------------------------------

const METHOD_BY_HELPER = { get: 'GET', post: 'POST', put: 'PUT', patch: 'PATCH', del: 'DELETE' };
const HELPER_NAMES = new Set(Object.keys(METHOD_BY_HELPER));
export const WRITE_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

function scriptKindFor(ts, filePath) {
  if (filePath.endsWith('.tsx')) return ts.ScriptKind.TSX;
  if (filePath.endsWith('.jsx')) return ts.ScriptKind.JSX;
  if (filePath.endsWith('.ts')) return ts.ScriptKind.TS;
  return ts.ScriptKind.JS;
}

/** Whitespace-collapsed node text: stable across reformatting. */
export function textOf(node) {
  return node ? node.getText().replace(/\s+/g, ' ').trim() : '';
}

function hasExport(ts, node) {
  const modifiers =
    typeof ts.canHaveModifiers === 'function' && ts.canHaveModifiers(node) ? ts.getModifiers(node) : node.modifiers;
  return Boolean(modifiers && modifiers.some((m) => m.kind === ts.SyntaxKind.ExportKeyword));
}

function isFunctionLike(ts, node) {
  return (
    node.kind === ts.SyntaxKind.ArrowFunction ||
    node.kind === ts.SyntaxKind.FunctionExpression ||
    node.kind === ts.SyntaxKind.FunctionDeclaration ||
    node.kind === ts.SyntaxKind.MethodDeclaration
  );
}

function returnExpressionText(ts, body) {
  if (!body) return '';
  if (ts.isBlock(body)) {
    for (const statement of body.statements) {
      if (ts.isReturnStatement(statement) && statement.expression) return textOf(statement.expression);
    }
    return '';
  }
  return textOf(body);
}

function bindFromFunction(ts, node) {
  const params = node.parameters
    .map((parameter) => (ts.isIdentifier(parameter.name) ? parameter.name.text : null))
    .filter(Boolean);
  return { kind: 'fn', text: returnExpressionText(ts, node.body), params };
}

/**
 * Build one file's model: imports (for alias-aware caller resolution), URL
 * bindings (endpoint tables, helper functions, class fields) and the exported
 * names, plus every HTTP/WS call site with its raw evidence.
 */
export function analyzeFile(ts, { abs, rel, source, layer, base, params }) {
  const file = ts.createSourceFile(abs, source, ts.ScriptTarget.Latest, true, scriptKindFor(ts, abs));
  const model = {
    rel,
    layer,
    imports: new Map(), // local -> { module, imported, namespace }
    binds: new Map(), // identifier | Table.member | this.field -> { kind, text, params }
    exports: new Map(), // exported name -> true
    /** `export default { f, g }` - names re-exported by the default object. */
    defaultExports: new Set(),
    /** Names that exist only as types (an interface / a type alias). */
    typeExports: new Set(),
    /**
     * Local -> exported name for `export { f }` / `export { f as g }` made
     * without a `from` clause (the re-export chains T12.03.b follows before
     * calling a client dead).
     */
    localReexports: new Map(),
    /** Local -> `{ module, imported }` for `export { f } from './x'`. */
    moduleReexports: new Map(),
    /** `export * from './x'` - every export of the target stays reachable. */
    starReexports: [],
    /** Local -> `{ exported, module }` for `export { x as default } from ...`. */
    defaultReexports: new Map(),
    callSites: [],
    mockImports: [],
    hasDevFlag: false,
    /** `const url = <template>` bodies, for call sites that pass the identifier. */
    constUrlBinds: new Map(),
  };

  const baseFileEnv = { base: base ?? '', binds: null, params: new Map() };

  const addImport = (local, moduleName, imported) => {
    model.imports.set(local, { module: moduleName, imported, namespace: imported === '*' });
  };

  const recordBind = (name, bind) => {
    if (!model.binds.has(name)) model.binds.set(name, bind);
  };

  /** `url = projectStreamUrl(pc.projectId)` -> `projectStreamUrl(pc.projectId)`. */
  const aliasNameOf = (initializer) => {
    if (ts.isIdentifier(initializer)) return initializer.text;
    if (ts.isPropertyAccessExpression(initializer)) return textOf(initializer);
    if (ts.isCallExpression(initializer)) return textOf(initializer);
    return null;
  };

  const collectEndpointTable = (initializer, owner) => {
    if (!ts.isObjectLiteralExpression(initializer)) return;
    for (const property of initializer.properties) {
      if (!property.name) continue;
      const memberName = property.name.getText().replace(/['"]/g, '');
      const key = `${owner}.${memberName}`;
      if (ts.isGetAccessorDeclaration(property) || ts.isMethodDeclaration(property)) {
        recordBind(key, { kind: 'expr', text: returnExpressionText(ts, property.body), params: [] });
        continue;
      }
      if (property.initializer) {
        if (ts.isArrowFunction(property.initializer)) recordBind(key, bindFromFunction(ts, property.initializer));
        else recordBind(key, { kind: 'expr', text: textOf(property.initializer), params: [] });
      }
    }
  };

  const visit = (node, scope) => {
    if (ts.isImportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      const moduleName = node.moduleSpecifier.text;
      if (/\/mocks/.test(moduleName)) model.mockImports.push(moduleName);
      const clause = node.importClause;
      if (clause) {
        if (clause.name) addImport(clause.name.text, moduleName, 'default');
        const bindings = clause.namedBindings;
        if (bindings && ts.isNamespaceImport(bindings)) addImport(bindings.name.text, moduleName, '*');
        if (bindings && ts.isNamedImports(bindings)) {
          for (const spec of bindings.elements) {
            addImport(spec.name.text, moduleName, (spec.propertyName ?? spec.name).text);
          }
        }
      }
    }

    if (ts.isExportDeclaration(node)) {
      const moduleName = node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier) ? node.moduleSpecifier.text : null;
      if (!node.exportClause && moduleName) {
        // `export * from './x'`: every export of the target stays reachable.
        model.starReexports.push(moduleName);
      } else if (node.exportClause && ts.isNamedExports(node.exportClause)) {
        for (const spec of node.exportClause.elements) {
          const local = (spec.propertyName ?? spec.name).text;
          const exported = spec.name.text;
          if (moduleName) model.moduleReexports.set(local, { module: moduleName, imported: exported });
          else model.localReexports.set(local, exported);
          model.exports.set(exported, true);
        }
      } else if (node.exportClause && ts.isNamespaceExport(node.exportClause) && moduleName) {
        model.starReexports.push(moduleName);
      }
    }

    // `export default { listProjects, createProject }` - the object literal is
    // the module's default export, and every member is reachable as
    // `default.listProjects` (or `api.listProjects` after an import).
    if (ts.isExportAssignment(node) && !node.isExportEquals && node.expression) {
      for (const statement of [node.expression]) {
        if (ts.isObjectLiteralExpression(statement)) {
          for (const property of statement.properties) {
            if (!property.name) continue;
            const name = property.name.getText().replace(/['"]/g, '');
            model.defaultExports.add(name);
            model.exports.set(name, true);
          }
        } else if (ts.isIdentifier(statement)) {
          model.defaultReexports.set(statement.text, { module: null, imported: 'default' });
        }
      }
    }

    // `const api = await import('../../api')` is the same namespace binding as
    // an import declaration, so dynamic imports enter the table too.
    if (ts.isVariableStatement(node)) {
      const exported = hasExport(ts, node);
      for (const declaration of node.declarationList.declarations) {
        if (ts.isIdentifier(declaration.name) && declaration.initializer) {
          const dynamic = dynamicImportModuleName(ts, declaration.initializer);
          if (dynamic) addImport(declaration.name.text, dynamic, '*');
        }
        if (!ts.isIdentifier(declaration.name) || !declaration.initializer) continue;
        const name = declaration.name.text;
        if (exported) model.exports.set(name, true);
        const alias = aliasNameOf(declaration.initializer);
        if (isFunctionLike(ts, declaration.initializer)) {
          recordBind(name, bindFromFunction(ts, declaration.initializer));
        } else if (alias) {
          // `const url = projectStreamUrl(pc.projectId)` is an alias, not a
          // value to inline: the alias is followed when the name is used.
          recordBind(name, { kind: 'alias', text: alias, params: [] });
        } else if (
          ts.isStringLiteral(declaration.initializer) ||
          declaration.initializer.kind === ts.SyntaxKind.NoSubstitutionTemplateLiteral ||
          ts.isTemplateExpression(declaration.initializer) // `const url = `${base}/x``
        ) {
          recordBind(name, { kind: 'expr', text: textOf(declaration.initializer), params: [] });
        } else if (ts.isObjectLiteralExpression(declaration.initializer)) {
          collectEndpointTable(declaration.initializer, name);
        } else if (ts.isCallExpression(declaration.initializer)) {
          recordBind(name, { kind: 'expr', text: textOf(declaration.initializer), params: [] });
        }
      }
    }

    if (ts.isFunctionDeclaration(node) && node.name) {
      if (hasExport(ts, node)) model.exports.set(node.name.text, true);
      recordBind(node.name.text, bindFromFunction(ts, node));
    }

    // Type-side exports: a name that only ever exists as a type still has to
    // resolve through a re-export chain, otherwise every type import would be
    // reported as "no scanned module declares ...". Kept in a second set so a
    // type import never counts as "someone could still call this".
    if ((ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) && node.name && hasExport(ts, node)) {
      model.exports.set(node.name.text, true);
      model.typeExports.add(node.name.text);
    }
    if ((ts.isEnumDeclaration(node) || ts.isClassDeclaration(node)) && node.name && hasExport(ts, node)) {
      model.exports.set(node.name.text, true);
    }

    if (ts.isExpressionStatement(node) && ts.isBinaryExpression(node.expression)) {
      const left = node.expression.left;
      if (ts.isPropertyAccessExpression(left) && left.expression.kind === ts.SyntaxKind.ThisKeyword) {
        recordBind(`this.${left.name.getText()}`, { kind: 'expr', text: textOf(node.expression.right), params: [] });
      }
    }
    ts.forEachChild(node, (child) => visit(child, scope));
  };
  ts.forEachChild(file, visit);

  // Snapshot the resolved bindings for the call-site walk: by then every
  // local helper (a `const` arrow returning a string) is in the table, so
  // `getApiBase()`-style indirection resolves instead of falling to unknown.
  const snapshot = { ...baseFileEnv };
  snapshot.binds = new Map(model.binds);
  snapshot.imports = new Set(model.imports.keys());
  collectCallSites(ts, file, model, { base, params, env: snapshot });

  // Every call site records the DEV mock branch of its enclosing function, so
  // "a failed request is answered from src/mocks" is reproducible from the
  // ledger alone (it is what retry: fallback_mock reports).
  for (const site of model.callSites) {
    const owner = enclosingFunction(ts, site.node);
    if (owner) site.mockBranch = mockBranchOf(ts, owner);
  }
  return { model, file, bindEnv: snapshot };
}

/** Nearest function-like ancestor of a node (the call site's owner). */
function enclosingFunction(ts, node) {
  let current = node ? node.parent : null;
  while (current) {
    if (
      ts.isFunctionDeclaration(current) ||
      ts.isFunctionExpression(current) ||
      ts.isArrowFunction(current) ||
      ts.isMethodDeclaration(current) ||
      ts.isGetAccessorDeclaration(current)
    ) {
      return current;
    }
    current = current.parent;
  }
  return null;
}

/** Walk the statements and record every HTTP/WS call site with its scope. */
function collectCallSites(ts, file, model, { params, env }) {
  // The scope is the enclosing *function*; a `const response = await ...`
  // must not rename it to a response variable.
  const scopeOf = (node, fallback) => {
    if (ts.isFunctionDeclaration(node) && node.name) return node.name.text;
    if (ts.isFunctionExpression(node) && node.name) return node.name.text;
    if (ts.isMethodDeclaration(node) && node.name) return node.name.getText();
    if (ts.isArrowFunction(node) && ts.isVariableDeclaration(node.parent) && ts.isIdentifier(node.parent.name)) {
      return node.parent.name.text;
    }
    if (ts.isPropertyAssignment(node) && node.name) return node.name.getText().replace(/['"]/g, '');
    return fallback;
  };

  const visit = (node, scope, paramsForScope) => {
    const nextScope = scopeOf(node, scope);
    let nextParams = paramsForScope;
    if (
      (ts.isFunctionDeclaration(node) || ts.isFunctionExpression(node) || ts.isArrowFunction(node) || ts.isMethodDeclaration(node)) &&
      node.parameters
    ) {
      nextParams = new Map(
        node.parameters
          .filter((parameter) => ts.isIdentifier(parameter.name))
          .map((parameter) => [parameter.name.text, parameter.name.text]),
      );
    }

    // `const url = `${base}/x`; get(url)` cannot be resolved in-file (the
    // helper call consumes an identifier, not a template), so the binding is
    // recorded here and re-read below; the import-aware pass can then inline
    // both `base` and `url`. Nothing else about the call site changes.
    if (
      ts.isVariableStatement(node) &&
      node.declarationList.flags & ts.NodeFlags.Const
    ) {
      for (const declaration of node.declarationList.declarations) {
        if (!ts.isIdentifier(declaration.name) || !declaration.initializer) continue;
        const name = declaration.name.text;
        // The same spelling inside the function shadows nothing for URL use,
        // but a *different* declaration of the same name must still win; the
        // walk is outside-in, so first write wins and later shadowing
        // declarations are ignored on purpose.
        if (model.constUrlBinds.has(name)) continue;
        if (
          ts.isStringLiteral(declaration.initializer) ||
          declaration.initializer.kind === ts.SyntaxKind.NoSubstitutionTemplateLiteral ||
          ts.isTemplateExpression(declaration.initializer) ||
          ts.isBinaryExpression(declaration.initializer)
        ) {
          model.constUrlBinds.set(name, textOf(declaration.initializer));
        }
      }
    }

    if (ts.isNewExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'WebSocket') {
      const urlArg = node.arguments?.[0];
      model.callSites.push(
        withResolution({
          kind: 'websocket',
          helper: 'WebSocket',
          method: 'GET',
          rawUrl: textOf(urlArg),
          scope: nextScope,
          line: lineOf(file.text, node.getStart()),
          params: nextParams ?? params,
          callText: textOf(node),
          node,
        }),
      );
      return; // the ctor arguments are not HTTP calls
    }

    if (ts.isCallExpression(node)) {
      const site = classifyCall(ts, node, nextScope, nextParams ?? params, file);
      if (site) model.callSites.push(withResolution(site));
    }
    // `new EventSource('/api/.../stream')` is the browser SSE entry point and
    // `navigator.sendBeacon('/api/...')` the fire-and-forget one; both carry a
    // URL that the ordinary helper/fetch classification would miss.
    if (ts.isNewExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'EventSource') {
      model.callSites.push(
        withResolution({
          kind: 'event-source',
          helper: 'EventSource',
          method: 'GET',
          rawUrl: textOf(node.arguments?.[0]),
          scope: nextScope,
          line: lineOf(file.text, node.getStart()),
          params: nextParams ?? params,
          callText: textOf(node),
          node,
        }),
      );
    }
    if (
      ts.isCallExpression(node) &&
      ts.isPropertyAccessExpression(node.expression) &&
      node.expression.name.text === 'sendBeacon'
    ) {
      model.callSites.push(
        withResolution({
          kind: 'beacon',
          helper: 'sendBeacon',
          method: 'POST',
          rawUrl: textOf(node.arguments?.[0]),
          scope: nextScope,
          line: lineOf(file.text, node.getStart()),
          params: nextParams ?? params,
          callText: textOf(node),
          node,
        }),
      );
    }
    ts.forEachChild(node, (child) => visit(child, nextScope, nextParams));
  };

  const withResolution = (site) => {
    if (!env) return site;
    const raw = urlExpressionOf(site.rawUrl);
    site.rawUrl = raw;
    const parts = [];
    if (/^[A-Za-z_$][\w$]*$/.test(raw) && model.constUrlBinds.has(raw)) {
      // `const url = ...` in the enclosing function body: resolve the
      // initializer, so `get(url)` reads the real path instead of `{url}`.
      site.constInitializer = model.constUrlBinds.get(raw);
      site.resolution = resolvePath(site.constInitializer, { ...env, params: site.params, unresolvedParts: parts });
      return site;
    }
    site.resolution = resolvePath(raw, { ...env, params: site.params, unresolvedParts: parts });
    return site;
  };

  for (const statement of file.statements) visit(statement, 'module', params);
}

function calleeParts(ts, call) {
  const expression = call.expression;
  if (ts.isIdentifier(expression)) return [expression.text, null];
  if (ts.isPropertyAccessExpression(expression) && ts.isIdentifier(expression.name)) {
    const owner = expression.expression;
    return [ts.isIdentifier(owner) ? owner.text : owner.getText(), expression.name.text];
  }
  return null;
}

function classifyCall(ts, call, scope, params, file) {
  const parts = calleeParts(ts, call);
  if (!parts) return null;
  const [head, member] = parts;
  const isApiHelper = member ? head === 'api' && HELPER_NAMES.has(member) : HELPER_NAMES.has(head);
  if (isApiHelper) {
    return {
      kind: 'helper',
      helper: member ? `${head}.${member}` : head,
      method: METHOD_BY_HELPER[member ?? head],
      rawUrl: textOf(call.arguments[0]),
      scope,
      line: lineOf(file.text, call.getStart()),
      params,
      callText: textOf(call),
      node: call,
      hasJsonBody: call.arguments.length > 1 && !isSignalExpression(ts, call.arguments[1]),
    };
  }
  if (member === 'fetch' || (head === 'fetch' && !member) || (head === 'window' && member === 'fetch')) {
    return {
      kind: 'fetch',
      helper: 'fetch',
      method: fetchMethod(ts, call),
      rawUrl: textOf(call.arguments[0]),
      scope,
      line: lineOf(file.text, call.getStart()),
      params,
      callText: textOf(call),
      node: call,
      fetchInfo: fetchInfoOf(ts, call, file.text),
    };
  }
  return null;
}

/** `await import('../../api')` (parenthesised or bare) -> `'../../api'`. */
function dynamicImportModuleName(ts, node) {
  let current = node;
  if (ts.isAwaitExpression(current)) current = current.expression;
  if (ts.isParenthesizedExpression(current)) current = current.expression;
  if (!ts.isCallExpression(current)) return null;
  if (current.expression.kind !== ts.SyntaxKind.ImportKeyword) return null;
  const arg = current.arguments[0];
  return arg && ts.isStringLiteral(arg) ? arg.text : null;
}

function isSignalExpression(ts, node) {
  if (!node) return false;
  if (ts.isIdentifier(node)) return /signal/i.test(node.text);
  if (ts.isPropertyAccessExpression(node)) return /signal/i.test(node.name.getText());
  return false;
}

function fetchMethod(ts, call) {
  const options = call.arguments[1];
  if (!options || !ts.isObjectLiteralExpression(options)) return 'GET';
  for (const property of options.properties) {
    if (!ts.isPropertyAssignment(property) || !property.name) continue;
    if (property.name.getText().replace(/['"]/g, '') !== 'method') continue;
    if (ts.isStringLiteral(property.initializer)) return property.initializer.text.toUpperCase();
    return 'UNKNOWN';
  }
  return 'GET';
}

function fetchInfoOf(ts, call, source) {
  const options = call.arguments[1];
  const ownerText = enclosingStatementText(ts, call);
  const wholeText = textOf(call);
  // Body reads (`body.success`, `response.blob()`, `response.body.getReader()`)
  // sit in the statements *after* the request, so the read-evidence scan
  // covers the enclosing function, not just the request statement. This is
  // still regex over source text: a read attributed to a different request in
  // the same function can colour this one, and a computed member reads
  // `unknown` (the note on the row names the rule that fired; the limitation
  // is recorded in "What this table can and cannot see").
  const evidenceText = `${ownerText}\n${enclosingFunctionText(ts, call, source)}`;
  const info = {
    authHeadersFor: /authHeadersFor\s*\(/.test(wholeText),
    bearerHeader: /Authorization/.test(wholeText),
    queryToken: /[?&]token=/.test(wholeText),
    idempotencyKey: /Idempotency-Key/.test(evidenceText),
    contentTypes: [],
    statuses: [],
    parsesJson: /\.json\s*\(\s*\)/.test(evidenceText),
    readsSuccess: /\.success\b/.test(evidenceText) || /success\s*===/.test(evidenceText),
    readsData: /\.data\b/.test(evidenceText),
    readsErrorMeta: /\berror\b\s*[?:]/.test(evidenceText) && /\bmeta\b/.test(evidenceText),
    multipart: /FormData/.test(evidenceText),
    blob: /\.blob\s*\(/.test(evidenceText),
    download: /createObjectURL|download\s*=|a\.download/.test(evidenceText),
    stream: /for await|getReader\s*\(/.test(evidenceText),
  };
  if (options && ts.isObjectLiteralExpression(options)) {
    for (const property of options.properties) {
      if (!ts.isPropertyAssignment(property) || !property.name) continue;
      const name = property.name.getText().replace(/['"]/g, '');
      if (name === 'method') continue;
      if (name === 'headers') {
        const text = textOf(property.initializer);
        if (/Content-Type|content-type/.test(text)) {
          for (const type of text.match(/'([\w/+.-]+\/[\w+.-]+)'/g) ?? []) {
            info.contentTypes.push(type.replace(/'/g, ''));
          }
        }
      }
    }
  }
  for (const match of ownerText.match(/status\s*===\s*(\d{3})/g) ?? []) {
    info.statuses.push(match.replace(/\D/g, ''));
  }
  info.statuses = sortStrings(info.statuses);
  info.contentTypes = sortStrings(info.contentTypes);
  void source;
  return info;
}

/**
 * The enclosing function's source text (capped), used as the read-evidence
 * window for a request: the body is read in later statements of the same
 * function. Module-level requests fall back to the file head, which can only
 * widen the evidence (a read is never attributed across functions).
 */
function enclosingFunctionText(ts, node, source) {
  const owner = enclosingFunction(ts, node);
  const text = owner ? owner.getText() : String(source ?? '').slice(0, 4000);
  return text.length > 8000 ? text.slice(0, 8000) : text;
}

function enclosingStatementText(ts, node) {
  let current = node;
  while (current.parent && !ts.isBlock(current.parent) && !ts.isSourceFile(current.parent)) {
    current = current.parent;
  }
  return textOf(current);
}

function lineOf(source, index) {
  let line = 1;
  for (let i = 0; i < index && i < source.length; i += 1) {
    if (source[i] === '\n') line += 1;
  }
  return line;
}

/**
 * The DEV-only mock branch at the top of a function: which src/mocks module it
 * imports, whether it answers from the mock store, and whether the function
 * also carries a try/catch (the retry/fallback signal).
 */
export function mockBranchOf(ts, node) {
  const text = node.getText();
  const devIndex = text.indexOf(DEV_FLAG);
  const hasDevBranch = devIndex >= 0;
  const mockImports = [];
  const importRe = /import\(\s*['"]([^'"]*mocks[^'"]*)['"]\s*\)/g;
  let match;
  while ((match = importRe.exec(text)) !== null) mockImports.push(match[1]);
  const hasTryCatch = /\bcatch\s*\(/.test(text);
  const fallbackStatic = /FALLBACK_[A-Z_]+\b/.test(text) && hasTryCatch;
  let devBody = '';
  if (hasDevBranch) {
    const braceIndex = text.indexOf('{', devIndex);
    const end = braceIndex >= 0 ? findBlockEnd(text, braceIndex) : -1;
    devBody = end > 0 ? text.slice(braceIndex, end) : text.slice(devIndex);
  }
  return {
    hasDevBranch,
    mockImports: sortStrings(mockImports),
    returnsMock: /return\s+[^;]*(MOCK_|mock)/.test(devBody),
    hasTryCatch,
    fallbackStatic,
  };
}

function findBlockEnd(text, open) {
  let depth = 0;
  for (let i = open; i < text.length; i += 1) {
    if (text[i] === '{') depth += 1;
    else if (text[i] === '}') {
      depth -= 1;
      if (depth === 0) return i + 1;
    }
  }
  return -1;
}

/**
 * Transport facts of the shared transport module (apps/workbench/api.ts): its
 * exported helpers are what carry auth, envelope and idempotency behaviour for
 * every legacy and bridge client that imports them.
 */
export function sharedTransportFacts(source) {
  const facts = {
    auth: /authHeadersFor\s*\(/.test(source) ? 'bearer' : /Authorization/.test(source) ? 'bearer' : 'unknown',
    envelope: /\.success\b/.test(source) && /\.data\b/.test(source) ? 'legacy' : /\.json\s*\(/.test(source) ? 'raw' : 'unknown',
    command_id: /Idempotency-Key/.test(source) ? 'yes' : 'no',
    retry: /\bretry|backoff|maxRetries\b/i.test(source) ? 'retry' : 'none',
    transport: 'json',
    evidence: 'apps/workbench/api.ts request() adds the origin-gated bearer and unwraps the {success,data} envelope',
  };
  return facts;
}

// ---------------------------------------------------------------------------
// per-call-site checks
// ---------------------------------------------------------------------------

const TRANSPORT_VALUES = ['json', 'sse', 'websocket', 'blob', 'download', 'multipart', 'stream', 'unknown'];
const AUTH_VALUES = ['bearer', 'cookie', 'query_token', 'none', 'unknown'];
const ENVELOPE_VALUES = ['v3', 'legacy', 'raw', 'unknown'];
const COMMAND_ID_VALUES = ['yes', 'no', 'n/a', 'unknown'];
const RETRY_VALUES = ['none', 'retry', 'fallback_mock', 'fallback_static', 'unknown'];

/**
 * The status vocabulary, in the order the report prints it. `legacy_unused`
 * replaced the old `migrated`, which conflated "already migrated" with "the
 * old client has no caller" - the second one is a deletion cand data with no
 * bridge to catch it, and the report must not sell it as progress.
 */
export const STATUS_ORDER = [
  'both',
  'legacy_only',
  'legacy_unused',
  'legacy_direct',
  'bridge_only',
  'contract_only',
  'route_only',
  'backend_unregistered',
  'orphan_client',
  'unknown',
];

export function transportOfRecord(record, contractTransport) {
  if (record.kind === 'websocket') return { transport: 'websocket', note: 'WebSocket upgrade; frames are not HTTP bodies' };
  if (record.kind === 'event-source') return { transport: 'sse', note: 'EventSource opens a text/event-stream response' };
  if (record.kind === 'beacon') return { transport: 'json', note: 'navigator.sendBeacon posts a body with no readable response' };
  // The bare `/stream` suffix heuristic only applies when the contract does
  // not already say what the response is: an operation declared as
  // text/event-stream is SSE even though its path ends in /stream.
  if (/\/stream$/.test(record.resolvedPath ?? '') && !contractTransport) {
    return { transport: 'websocket', note: 'path ends in /stream and the backend registers a WebSocket handler' };
  }
  const info = record.fetchInfo ?? {};
  const contentTypes = info.contentTypes ?? [];
  if (info.multipart) return { transport: 'multipart', note: 'FormData request body' };
  if (contentTypes.includes('text/event-stream')) return { transport: 'sse', note: 'Accept/Content-Type text/event-stream' };
  // `download` is read before `blob`: a blob consumed through
  // URL.createObjectURL / an `a.download` link is a download, while a plain
  // `.blob()` with no download marker stays `blob`. Both are non-JSON; the
  // distinction matters because the download path also gives the file a name.
  if (info.download) return { transport: 'download', note: 'URL.createObjectURL / download attribute' };
  if (info.blob) return { transport: 'blob', note: 'response read with .blob()' };
  if (info.stream) return { transport: 'stream', note: 'response body consumed as a stream (for await / getReader())' };
  if (contractTransport) return { transport: contractTransport, note: 'contract declares a non-JSON response' };
  return { transport: 'json', note: null };
}

function authOfRecord(record, shared) {
  if (record.kind === 'websocket') {
    return { auth: 'none', note: 'the token travels in the Sec-WebSocket-Protocol subprotocol, not an HTTP credential' };
  }
  const info = record.fetchInfo ?? {};
  if (info.queryToken || /[?&]token=/.test(record.rawUrl ?? '')) return { auth: 'query_token', note: 'token in the query string' };
  if (record.kind === 'helper') return { auth: shared.auth, note: `shared transport: ${shared.evidence}` };
  if (info.authHeadersFor || info.bearerHeader) return { auth: 'bearer', note: 'authHeadersFor(url) attaches the origin-gated bearer' };
  if (!/^https?:\/\//.test(record.resolvedPath ?? '') && record.kind === 'fetch') {
    return { auth: 'cookie', note: 'same-origin request with no explicit credential; relies on the ambient cookie' };
  }
  return { auth: 'unknown', note: 'no credential evidence on this call site' };
}

function envelopeOfRecord(record, shared) {
  if (record.kind === 'websocket') return { envelope: 'raw', note: 'WS frame payload, no HTTP envelope' };
  if (record.kind === 'helper') return { envelope: shared.envelope, note: `shared transport unwraps the envelope: ${shared.evidence}` };
  const info = record.fetchInfo ?? {};
  if (info.readsErrorMeta && !info.readsSuccess) return { envelope: 'v3', note: 'reads {error, meta}' };
  if (info.readsSuccess && info.readsData) return { envelope: 'legacy', note: 'reads {success, data}' };
  if (info.parsesJson) return { envelope: 'raw', note: 'reads the JSON body without a wrapper' };
  return { envelope: 'unknown', note: 'response body is not read' };
}

function commandIdOfRecord(record, { isWrite, registered }) {
  if (!isWrite) return { command_id: 'n/a', note: 'not a write' };
  if (!registered) return { command_id: 'unknown', note: 'the backend does not register this write, so no idempotency contract is defined' };
  const info = record.fetchInfo ?? {};
  if (info.idempotencyKey) return { command_id: 'yes', note: 'the call site sends Idempotency-Key' };
  return {
    command_id: 'no',
    note: 'registered write with no Idempotency-Key on the call site (api.ts post/put/patch/del never add one)',
  };
}

function retryOfRecord(record, mockBranch) {
  if (mockBranch?.hasDevBranch && (mockBranch.returnsMock || mockBranch.mockImports.length > 0)) {
    return {
      retry: 'fallback_mock',
      note: `DEV+isMockActive() answers from ${mockBranch.mockImports[0] ?? 'src/mocks'}; removed from production builds by the DEV flag`,
    };
  }
  if (mockBranch?.fallbackStatic) {
    return { retry: 'fallback_static', note: 'a request failure is answered from a hard-coded copy instead of propagating' };
  }
  if (record.kind === 'websocket') {
    return { retry: 'retry', note: 'the socket reconnects with backoff and replays its subscriptions' };
  }
  return { retry: 'none', note: 'a failure propagates to the caller' };
}

/**
 * Turn one call-site record into the evidence row the ledger keeps: the
 * resolved endpoint plus the auth/envelope/command-id/retry readings.
 */
export function classifyRecord(record, context) {
  const { shared, contractTransport, isWrite, registered } = context;
  const transport = transportOfRecord(record, contractTransport);
  const auth = authOfRecord(record, shared);
  const envelope = envelopeOfRecord(record, shared);
  const commandId = commandIdOfRecord(record, { isWrite, registered });
  const retry = retryOfRecord(record, record.mockBranch);
  return {
    ...record,
    transport: transport.transport,
    transport_note: transport.note,
    auth: auth.auth,
    auth_note: auth.note,
    envelope: envelope.envelope,
    envelope_note: envelope.note,
    command_id: commandId.command_id,
    command_id_note: commandId.note,
    retry: retry.retry,
    retry_note: retry.note,
  };
}

// ---------------------------------------------------------------------------
// caller index (import-and-call sites, alias aware)
// ---------------------------------------------------------------------------

function stripExtension(file) {
  return file.replace(/\.(tsx|ts|jsx|js|mjs|cjs)$/, '');
}

/**
 * A file's own bindings plus the bindings it imports, keyed by the local name
 * the importing file uses. That is what lets `API_ENDPOINTS.agents` (or an
 * aliased `import { API_ENDPOINTS as EP }`) resolve to the real path.
 */
function mergeImportedBinds(file, allFiles, fileByRel, lookup) {
  const merged = new Map(file.model.binds);
  for (const [local, info] of file.model.imports) {
    if (local === 'tmp') continue;
    const targetRel = resolveImportPath(file.rel, info.module, lookup);
    if (!targetRel) continue;
    const target = fileByRel.get(targetRel);
    if (!target) continue;
    if (info.namespace && info.imported === '*') {
      // `import * as ws from '../services'`: the namespace carries every export
      // of the target, so a re-exported binding (`ws.connect`, reached through
      // the barrel's `export * from './websocket'`) must land under the local
      // alias too, or the call site steals a `{connect}` placeholder - and, if
      // the namespace covers several targets, the shares would overwrite each
      // other instead of merging.
      for (const [key, bind] of target.model.binds) {
        if (key.includes('.')) merged.set(`${local}.${key.split('.').slice(1).join('.')}`, bind);
      }
      continue;
    }
    const direct = target.model.binds.get(info.imported);
    if (direct) {
      merged.set(local, direct);
      continue;
    }
    // A named import of a whole table (API_ENDPOINTS): copy its members
    // under the local prefix so `local.member` still resolves.
    for (const [key, bind] of target.model.binds) {
      if (key.startsWith(`${info.imported}.`)) {
        merged.set(`${local}${key.slice(info.imported.length)}`, bind);
      }
    }
  }
  return merged;
}

/** Resolve a relative import specifier to a scanned file (posix, root-relative). */
export function resolveImportPath(fromRel, specifier, lookup) {
  if (!specifier.startsWith('.')) return null;
  const joined = path.posix.normalize(path.posix.join(path.posix.dirname(fromRel), specifier));
  const candidates = [
    joined,
    `${joined}.ts`,
    `${joined}.tsx`,
    `${joined}/index.ts`,
    `${joined}/index.tsx`,
    `${joined}.js`,
    `${joined.replace(/\.js$/, '')}.ts`,
  ];
  for (const candidate of candidates) {
    if (lookup.has(candidate)) return candidate;
  }
  return null;
}

/** The scope (enclosing function) a node sits in; `module` outside any. */
function scopeNameOf(ts, node, fallback = 'module') {
  if (ts.isFunctionDeclaration(node) && node.name) return node.name.text;
  if (ts.isFunctionExpression(node) && node.name) return node.name.text;
  if (ts.isMethodDeclaration(node) && node.name) return node.name.getText();
  if (ts.isArrowFunction(node) && ts.isVariableDeclaration(node.parent) && ts.isIdentifier(node.parent.name)) {
    return node.parent.name.text;
  }
  if (ts.isPropertyAssignment(node) && node.name) return node.name.getText().replace(/['"]/g, '');
  return fallback;
}

/**
 * Pending exports of one module, as seen by the re-export walk: the module's
 * own exported names plus its `export { f } from` / `export * from` edges. A
 * named import of a name that only appears in `pending` is a re-export, and
 * the walk follows it (bounded) to the defining module.
 */
export function moduleExportSummary(files) {
  const lookup = new Set(files.map((file) => file.rel));
  const summary = new Map();
  for (const file of files) {
    const model = file.model;
    // `export default { f, g }`: each member is reachable as `api.f`, but `f`
    // may only be *imported* into this module (an object that re-exports named
    // imports is as common as one that lists local functions). Resolving the
    // member back to the module that declares it is what keeps
    // `api.f()` -> `services/x.ts#f` pointing at the ledger's owner key.
    const defaultObjectMembers = new Map();
    for (const name of model.defaultExports) {
      const info = model.imports.get(name);
      if (!info) {
        defaultObjectMembers.set(name, { rel: file.rel, imported: name, local: true });
        continue;
      }
      const target = resolveImportPath(file.rel, info.module, lookup);
      if (!target) continue;
      defaultObjectMembers.set(name, {
        rel: target,
        imported: info.namespace ? name : info.imported,
        local: false,
      });
    }
    summary.set(file.rel, {
      names: new Set(model.exports.keys()),
      typeExports: new Set(model.typeExports),
      defaultExports: new Set(model.defaultExports),
      defaultObjectMembers,
      namedEdges: new Map(
        [...model.moduleReexports].map(([local, edge]) => [
          local,
          { target: resolveImportPath(file.rel, edge.module, lookup), imported: edge.imported },
        ]),
      ),
      starTargets: model.starReexports.map((moduleName) => resolveImportPath(file.rel, moduleName, lookup)),
      defaultTo: model.defaultReexports.size > 0 ? file.rel : null,
      rel: file.rel,
    });
  }
  return summary;
}

/**
 * Where the member `member` of `sourceRel`'s *default export object* is
 * declared. `undefined` means "this module has no default object carrying that
 * member" - the caller then reports the shape instead of guessing.
 */
function resolveDefaultMember(summary, sourceRel, member) {
  const entry = summary.get(sourceRel);
  if (!entry) return undefined;
  const direct = entry.defaultObjectMembers?.get(member);
  if (direct) return followToDeclaration(summary, direct.rel, direct.imported) ?? undefined;
  return undefined;
}

/**
 * Follow an import to the module that actually declares the name: an imported
 * name that the target only re-exports (`export { f } from './x'`,
 * `export * from './x'`) is followed through, so "the old client is only
 * imported through a barrel" is still recognised as an import.
 */
function followToDeclaration(summary, targetRel, name, seen = new Set(), depth = 0) {
  if (targetRel === null || depth > 8) return null;
  const hop = `${targetRel}#${name}`;
  if (seen.has(hop)) return null;
  seen.add(hop);
  const entry = summary.get(targetRel);
  if (!entry) return null;
  if (name === 'default') return { rel: targetRel, name: 'default', summary: entry };
  // An explicit re-export edge wins over the module's own export set: the
  // module's `names` contains every name it re-exports too, so checking
  // `names` first would stop at the barrel and record the importer under
  // `barrel.ts#name` - a key no ledger row ever carries, silently losing the
  // caller (the exact shape mutation experiment 2 checks).
  const edge = entry.namedEdges.get(name);
  if (edge && edge.target) return followToDeclaration(summary, edge.target, edge.imported, seen, depth + 1);
  if (entry.names.has(name) || entry.defaultExports.has(name)) return { rel: targetRel, name, summary: entry };
  for (const star of entry.starTargets) {
    if (!star) continue;
    const followed = followToDeclaration(summary, star, name, seen, depth + 1);
    if (followed) return followed;
  }
  return null;
}

const VALUE_CONTEXTS = new Set([
  'CallExpression.callee',
  'CallExpression.head',
  'CallExpression.argument',
  'NewExpression.callee',
  'NewExpression.argument',
  'PropertyAccessExpression.expression',
  'ElementAccessExpression.expression',
  'PropertyAssignment.initializer',
  'ShorthandPropertyAssignment',
  'SpreadAssignment',
  'JsxExpression',
  'ReturnStatement',
  'AwaitExpression',
  'Decorator',
  'ExportAssignment',
]);

const VALUE_TYPE_KINDS = new Set([
  'AsExpression',
  'SatisfiesExpression',
  'TypeAssertionExpression',
  'ParenthesizedExpression',
  'NonNullExpression',
  'PartiallyEmittedExpression',
]);

const TYPE_ONLY_KINDS = new Set([
  'TypeReference',
  'TypeQuery',
  'ExpressionWithTypeArguments',
  'ImportType',
  'QualifiedName',
  'TypePredicate',
  'InterfaceDeclaration',
  'TypeAliasDeclaration',
  'PropertySignature',
  'MethodSignature',
  'CallSignature',
  'ConstructSignature',
  'IndexSignature',
  'TypeParameter',
  'HeritageClause',
]);

/**
 * Every *(scope, name)* pair this file evaluates as a value: `f()`, `x.f()`,
 * `api.f()`, `{ queryFn: f }`, `onClick={f}`, `new Thing(f)`, `return f`.
 * Type positions (`: Fn`) and type-only identifiers are skipped, so a type
 * of the same name never counts as a use of the function.
 */
export function collectValueUses(ts, file) {
  const uses = [];
  const record = (node, context) => {
    const scope = scopeNameOf(ts, node);
    if (ts.isIdentifier(node)) {
      uses.push({ name: node.text, scope, context, kind: 'binding' });
    } else if (ts.isPropertyAccessExpression(node) && ts.isIdentifier(node.name)) {
      uses.push({ name: node.name.text, scope, context, kind: 'property', owner: textOf(node.expression), member: node.name.text });
    } else if (ts.isElementAccessExpression(node) && node.argumentExpression && ts.isStringLiteral(node.argumentExpression)) {
      uses.push({ name: node.argumentExpression.text, scope, context, kind: 'property', owner: textOf(node.expression), member: node.argumentExpression.text });
    }
  };
  /** A name that a declaration introduces is not a *use* of an import. */
  const isDeclarationName = (node) => {
    const parent = node.parent;
    if (!parent) return false;
    if (ts.isVariableDeclaration(parent) && parent.name === node) return true;
    if (ts.isParameter(parent) && parent.name === node) return true;
    if ((ts.isFunctionDeclaration(parent) || ts.isFunctionExpression(parent) || ts.isClassDeclaration(parent)) && parent.name === node) return true;
    if (ts.isPropertyAssignment(parent) || ts.isPropertySignature(parent) || ts.isMethodDeclaration(parent)) return parent.name === node;
    return false;
  };
  const visit = (node, context) => {
    if (!node) return;
    // A plain identifier evaluated in a value position is itself the evidence:
    // `{...api}`, `Object.values(api)`, `return f`.
    if (ts.isIdentifier(node)) {
      if (!isDeclarationName(node)) record(node, context);
      return;
    }
    const kindName = ts.SyntaxKind[node.kind];
    // Type positions carry no runtime reference: skip them entirely (a value
    // import used only as a type must not make the client look "called").
    if (TYPE_ONLY_KINDS.has(kindName)) return;
    if (kindName === 'ImportDeclaration' || kindName === 'ExportDeclaration' || kindName === 'ImportEqualsDeclaration') return;
    if (VALUE_TYPE_KINDS.has(kindName)) return visit(node.expression, context);
    if (ts.isCallExpression(node)) {
      const expression = node.expression;
      if (ts.isPropertyAccessExpression(expression) || ts.isElementAccessExpression(expression)) {
        record(expression, 'CallExpression.callee');
        // `api.f()` - the head is evaluated as a value too.
        visit(expression.expression, 'CallExpression.head');
      } else {
        // `f()` and chains (`await f(x).catch(...)`, `f()()`) both land here.
        visit(expression, 'CallExpression.callee');
      }
      for (const argument of node.arguments) visit(argument, 'CallExpression.argument');
      return;
    }
    if (ts.isNewExpression(node)) {
      const expression = node.expression;
      if (ts.isPropertyAccessExpression(expression)) {
        record(expression, 'NewExpression.callee');
        visit(expression.expression, 'NewExpression.head');
      } else {
        record(expression, 'NewExpression.callee');
      }
      for (const argument of node.arguments ?? []) visit(argument, 'NewExpression.argument');
      return;
    }
    if (ts.isPropertyAccessExpression(node)) {
      if (!ts.isGetAccessorDeclaration(node.parent)) record(node, 'PropertyAccessExpression.expression');
      // `API_ENDPOINTS.projects` - the head is the binding being read.
      visit(node.expression, 'PropertyAccessExpression.head');
      return;
    }
    if (ts.isElementAccessExpression(node)) {
      record(node, 'ElementAccessExpression.expression');
      visit(node.expression, 'ElementAccessExpression.head');
      return;
    }
    if (ts.isShorthandPropertyAssignment(node)) {
      record(node.name, 'ShorthandPropertyAssignment');
      return;
    }
    if (ts.isObjectBindingPattern(node)) {
      // `const { f } = svc` destructures the module. Which member it is cannot
      // be read off the import binding, so this is recorded as an
      // unattributable shape, never as "the import is unused".
      for (const element of node.elements) {
        const itself = element.propertyName ?? element.name;
        if (ts.isIdentifier(itself)) uses.push({ name: '*', scope: scopeNameOf(ts, node), context: 'object binding destructuring', kind: 'objectBinding' });
        else if (ts.isObjectBindingPattern(itself)) visit(itself, 'object binding destructuring');
      }
      return;
    }
    if (ts.isSpreadAssignment(node) || ts.isSpreadElement(node)) {
      // `{ ...api }` / `[...api]`: the spread target is a value use, recorded
      // under its own name so an import can be attributed to it.
      visit(node.expression, 'SpreadAssignment');
      if (ts.isIdentifier(node.expression)) {
        uses.push({ name: node.expression.text, scope: scopeNameOf(ts, node), context: 'object spread', kind: 'objectSpread' });
      }
      return;
    }
    if (ts.isPropertyAssignment(node)) {
      visit(node.initializer, 'PropertyAssignment.initializer');
      return;
    }
    if (ts.isJsxExpression(node)) {
      if (node.expression) visit(node.expression, 'JsxExpression');
      return;
    }
    if (ts.isJsxAttribute(node) || ts.isJsxSpreadAttribute(node)) {
      ts.forEachChild(node, (child) => visit(child, 'JsxExpression'));
      return;
    }
    if (ts.isReturnStatement(node)) {
      visit(node.expression, 'ReturnStatement');
      return;
    }
    if (ts.isAwaitExpression(node)) {
      visit(node.expression, 'AwaitExpression');
      return;
    }
    if (ts.isDecorator(node)) {
      visit(node.expression, 'Decorator');
      return;
    }
    if (ts.isExportAssignment(node)) {
      visit(node.expression, 'ExportAssignment');
      return;
    }
    if (ts.isVariableDeclaration(node) || ts.isBinaryExpression(node) || ts.isArrayLiteralExpression(node) || ts.isObjectLiteralExpression(node)) {
      ts.forEachChild(node, (child) => visit(child, context));
      return;
    }
    ts.forEachChild(node, (child) => visit(child, context));
  };
  for (const statement of file.statements) visit(statement, 'module');
  return uses;
}

/**
 * Every import, keyed by `${declaring module}#${name}` - the exact name the
 * ledger's `old_clients`/`bridge` entries carry. A re-export chain is followed,
 * so importing through a barrel still counts as importing the module that owns
 * the function: `export { listProjects } from './projects'` re-exported from a
 * barrel and imported there resolves to `services/projects.ts#listProjects`.
 *
 * Recognised shapes (one record per import site):
 *   `import { f } from './x'`                  named, call `f()` or value `f`
 *   `import { f as g } from './x'`             named + alias
 *   `import * as x from './x'`                 namespace, member `x.f`
 *   `import api from './x'`                    default object, member `api.f`
 *   `const api = await import('./x')`          dynamic namespace, member `api.f`
 *
 * `usedBy[].context` and `.scope` say where the name was evaluated as a value
 * (call / value pass / property read / re-export), so a row can be audited
 * without re-reading the file. A namespace/default import that the walker
 * cannot attribute any member to is NOT silently dropped: it is recorded under
 * `usedBy: []` with `unresolved: true`, and the ledger keeps every name of
 * that module out of the "no importer" verdict.
 */
export function buildImporterIndex(ts, files) {
  const lookup = new Set(files.map((file) => file.rel));
  const summary = moduleExportSummary(files);
  // Only modules that can carry a client function matter for the "no importer"
  // verdict: the client layers themselves plus whatever re-exports from them.
  const interesting = interestingModules(files, summary, lookup);
  const index = new Map();
  const unresolved = [];
  const record = (targetRel, name, fileRel, detail) => {
    const key = `${targetRel}#${name}`;
    const bucket = index.get(key) ?? new Map();
    index.set(key, bucket);
    const importer = bucket.get(fileRel) ?? { aliases: [], usedBy: [], unresolved: false, reexported: false };
    bucket.set(fileRel, importer);
    if (detail.alias) importer.aliases.push(detail.alias);
    if (detail.reexported) importer.reexported = true;
    if (detail.unresolved) importer.unresolved = true;
    for (const use of detail.uses) importer.usedBy.push(use);
  };
  for (const file of files) {
    const sourceFile = ts.createSourceFile(file.abs, file.source, ts.ScriptTarget.Latest, true, scriptKindFor(ts, file.abs));
    const uses = collectValueUses(ts, sourceFile);
    const starReexporting = file.model.starReexports.length > 0;
    for (const [local, info] of file.model.imports) {
      const sourceRel = resolveImportPath(file.rel, info.module, lookup);
      if (!sourceRel) {
        // A bare package (`react`) or an unresolvable path. Only a relative or
        // client-layer specifier could have carried a client function; the
        // rest is noise and is not listed.
        if (isRelativeSpecifier(info.module) || interesting.has(info.module)) {
          unresolved.push({ file: file.rel, local, module: info.module, reason: 'import specifier does not resolve to a scanned file' });
        }
        continue;
      }
      const evidence = uses.filter((use) => useAppliesTo(use, local, info));
      const alias = local !== info.imported && !info.namespace ? `${local} as ${info.imported}` : null;
      const useRecords = evidence.map((use) => ({ name: local, scope: use.scope, context: use.context, member: use.member ?? null }));
      const leaves = file.model.localReexports.has(local) || file.model.moduleReexports.has(local);

      if (info.namespace || info.imported === 'default') {
        if (!interesting.has(sourceRel)) continue;
        // A namespace / default-object import names a *module*, so every name
        // the target exports is potentially reached. Names the walker saw as
        // `x.f` are recorded precisely; the rest are marked unresolved-by-shape
        // so the endpoint they might carry is never called "caller-free".
        const declared = info.namespace ? { rel: sourceRel, name: '*', summary: summary.get(sourceRel) } : followToDeclaration(summary, sourceRel, 'default');
        if (!declared) {
          unresolved.push({ file: file.rel, local, module: info.module, reason: `no scanned module declares '${info.imported}'` });
          continue;
        }
        const targetEntry = summary.get(declared.rel);
        const propertyUses = [...uses].filter((use) => use.kind === 'property' && use.owner === local);
        const memberNames = new Set([
          ...propertyUses.map((use) => use.member),
          ...(declared.name === '*' ? [] : followDefaultMembers(summary, declared.rel)),
        ]);
        // Where each member is declared: a member the imported module only
        // re-exports (`export * from './websocket'` seen through
        // `import * as ws from '../services'`, or a default object whose
        // members are imported names) belongs to the module those names come
        // from, not to the barrel - otherwise the recorded owner key would be
        // the barrel and would never match the ledger's `module#function` row.
        const memberTarget = new Map();
        for (const member of propertyUses.map((use) => use.member)) {
          if (!info.namespace && declared.name === 'default') {
            const resolved = resolveDefaultMember(summary, declared.rel, member);
            if (resolved) memberTarget.set(member, resolved);
            else memberTarget.set(member, { rel: declared.rel, name: member });
            continue;
          }
          const followed = followToDeclaration(summary, declared.rel, member);
          memberTarget.set(member, followed ? { rel: followed.rel, name: member } : { rel: declared.rel, name: member });
        }
        for (const use of propertyUses) {
          const target = memberTarget.get(use.member);
          const ownerRel = target ? target.rel : declared.rel;
          record(ownerRel, use.member, file.rel, {
            alias: null,
            uses: [{ name: local, scope: use.scope, context: use.context, member: use.member }],
            unresolved: false,
            reexported: false,
          });
        }
        // Every other exported name of the target is left unattributed: the
        // namespace import could reach it at runtime even though the walker
        // found no `x.name` use (a computed member, a spread, ...). The names
        // a re-export chain carries (`export * from` targets) count too, and
        // each reachable client name gets an unresolved importer recorded so
        // the ledger can honestly say no endpoint is caller-free because of
        // this import. One aggregated line per namespace import keeps the
        // report readable.
        const reachableNames = new Set(targetEntry?.names ?? []);
        const collectStarNames = (rel, depth) => {
          if (depth > 8) return;
          for (const star of summary.get(rel)?.starTargets ?? []) {
            if (!star || reachableNames.has(star)) continue;
            collectStarNames(star, depth + 1);
            for (const name of summary.get(star)?.names ?? []) reachableNames.add(name);
          }
        };
        collectStarNames(declared.rel, 0);
        // A `binding` use of the namespace/default local *itself* - `return api`,
        // `api[key]`, `Object.values(api)` - hands the whole object out, so the
        // walker cannot prove which members are reached even when the member
        // set is fully known (a computed read is invisible to the property
        // walk). `*.head` positions are excluded: `x.f()` / `x.f` visit the
        // head as a member read, which is exactly the evidence the walker
        // *does* have; counting it as an escape would mark every member
        // unreached on every ordinary namespace call. Every reachable member
        // is then treated as possibly imported, the same fail-closed rule as a
        // destructuring of the binding.
        const objectEscapes = uses.filter(
          (use) => use.kind === 'binding' && use.name === local && !String(use.context).endsWith('.head'),
        );
        const escapeContexts = sortStrings([...new Set(objectEscapes.map((use) => use.context))]);
        const unreached = [...reachableNames].filter(
          (name) => (escapeContexts.length > 0 || !memberNames.has(name)) && !targetEntry?.typeExports.has(name),
        );
        if (unreached.length > 0) {
          for (const name of unreached) {
            // A default-object member (`api.f` off `export default { f }`)
            // re-exports the name from the module it was imported from; only a
            // namespace export is declared by the target itself. Resolving
            // through `resolveDefaultMember` first keeps the recorded owner on
            // the client module (`services/x.ts#f`) instead of stopping at the
            // gatherer - a key no ledger row carries, which would silently
            // lose the fail-closed record.
            const followed =
              declared.name === 'default'
                ? (resolveDefaultMember(summary, declared.rel, name) ?? followToDeclaration(summary, declared.rel, name))
                : followToDeclaration(summary, declared.rel, name);
            if (!followed || !interesting.has(followed.rel)) continue;
            record(followed.rel, followed.name, file.rel, {
              alias: null,
              uses: [{ name: local, scope: 'module', context: 'unreached namespace member', member: name }],
              unresolved: true,
              reexported: false,
            });
          }
          const escapeNote =
            escapeContexts.length > 0
              ? `the namespace/default binding itself escapes as a value (${escapeContexts.join(', ')}), so no member set can be proven; `
              : '';
          unresolved.push({
            file: file.rel,
            local,
            module: info.module,
            reason: `${escapeNote}namespace/default import covers ${unreached.length} exported name(s) with no ${local}.<name> use in this file (${sortStrings(unreached).slice(0, 6).join(', ')}${unreached.length > 6 ? ', ...' : ''}); none of them, or anything they call, is treated as caller-free because of this import`,
          });
        }
        continue;
      }

      const declared = followToDeclaration(summary, sourceRel, info.imported);
      if (!declared) {
        // A module the scan does not model (a `.json`, an asset) or a name
        // added at runtime. Neither can be a client function.
        unresolved.push({ file: file.rel, local, module: info.module, reason: `no scanned module declares '${info.imported}'` });
        continue;
      }
      if (!interesting.has(declared.rel)) continue;
      const typeOnly = declared.summary?.typeExports?.has(declared.name) ?? false;
      // A member read off an *unresolvable shape* (`const { f } = svc`, a
      // computed `svc[name]()`, a spread of the module) cannot be tied to a
      // name at all: the file gets one aggregated line and every name the
      // target exports is treated as possibly imported, so no endpoint of it
      // is ever reported caller-free.
      const unattributable = uses.filter((use) => use.kind === 'objectBinding' || (use.kind === 'objectSpread' && use.owner === local));
      if (unattributable.length > 0) {
        const contexts = sortStrings([...new Set(unattributable.map((use) => use.context))]);
        for (const name of sortStrings([...(declared.summary?.names ?? [])])) {
          if (declared.summary?.typeExports?.has(name)) continue;
          record(declared.rel, name, file.rel, {
            alias: null,
            uses: [{ name: local, scope: 'module', context: 'unattributable import shape', member: null }],
            unresolved: true,
            reexported: false,
          });
        }
        unresolved.push({
          file: file.rel,
          local,
          module: info.module,
          reason: `the module is used through a shape the walker cannot attribute to a name (${contexts.join(', ')}); every export of ${declared.rel} is treated as possibly imported`,
        });
        continue;
      }
      record(declared.rel, declared.name, file.rel, {
        alias,
        uses: useRecords,
        unresolved: useRecords.length === 0 && !typeOnly,
        reexported: useRecords.length === 0 && (leaves || starReexporting),
      });
      if (useRecords.length === 0 && !typeOnly) {
        // A type leaves the file again through a re-export, or the shape is
        // beyond the walker. Both are reported rather than read as "no use".
        unresolved.push({ file: file.rel, local, module: info.module, reason: `imported '${info.imported}' but no call/value use found` });
      }
    }
  }
  return { index, unresolved, summary };
}

/** The member names of a module's default export object, if it has one. */
function followDefaultMembers(summary, targetRel) {
  return [...(summary.get(targetRel)?.defaultObjectMembers?.keys() ?? [])];
}

/**
 * The importers recorded for one `file#name` owner entry. An owner whose
 * namespace import could not be attributed carries `unresolved: true`, and the
 * ledger treats that as "an importer exists" rather than as "caller free".
 */
export function callersOf(index, ownerKey) {
  return [...(index.get(ownerKey)?.entries() ?? [])];
}

function isRelativeSpecifier(moduleName) {
  return String(moduleName).startsWith('.');
}

/**
 * Modules whose imports can mean "this endpoint is still reached": the old
 * client and bridge layers, plus every module that re-exports from one of them
 * (a barrel). A default-object gatherer counts as well: `export default { f }`
 * where `f` is imported from a client hands that client out as `api.f`, so a
 * page importing the gatherer's default can still call the client.
 * Everything else (a UI component, a store, `react`) is not a client and its
 * imports are not tracked.
 */
export function interestingModules(files, summary, lookup) {
  const interesting = new Set();
  for (const file of files) {
    if (file.layer === 'old_client' || file.layer === 'bridge' || file.layer === 'shared') interesting.add(file.rel);
  }
  let changed = true;
  while (changed) {
    changed = false;
    for (const file of files) {
      if (interesting.has(file.rel)) continue;
      const edges = [
        ...file.model.starReexports.map((moduleName) => resolveImportPath(file.rel, moduleName, lookup)),
        ...[...file.model.moduleReexports.values()].map((edge) => resolveImportPath(file.rel, edge.module, lookup)),
        // `export default { f, g }`: each member that is an imported name
        // re-exports the module that declares it.
        ...[...file.model.defaultExports]
          .map((name) => file.model.imports.get(name))
          .filter(Boolean)
          .map((info) => resolveImportPath(file.rel, info.module, lookup)),
      ];
      if (edges.some((target) => target && interesting.has(target))) {
        interesting.add(file.rel);
        changed = true;
      }
    }
  }
  return interesting;
}

/** Does one value-use in an importer actually touch this import binding? */
function useAppliesTo(use, local, info) {
  if (info.namespace || info.imported === 'default') {
    // `x.f()` / `api.f()` - the member is read off the imported namespace.
    return use.kind === 'property' && use.owner === local;
  }
  return use.kind === 'binding' && use.name === local;
}


// ---------------------------------------------------------------------------
// ledger assembly
// ---------------------------------------------------------------------------

function firstConcrete(values, fallback = 'unknown') {
  const pool = values.filter((v) => v !== undefined && v !== null);
  const concrete = pool.filter((v) => v !== 'unknown');
  if (concrete.length > 0) return concrete[0];
  return pool.length > 0 ? pool[0] : fallback;
}

export function buildEndpointMap(inputs) {
  const { openapi, routerRoutes, generatedOps, files, extractor, shared, scanRoot, ts } = inputs;

  const routeByShape = new Map();
  for (const route of routerRoutes) {
    const shape = `${route.method} ${shapeOf(route.path)}`;
    if (!routeByShape.has(shape)) routeByShape.set(shape, []);
    routeByShape.get(shape).push(route);
  }
  const openapiByShape = new Map();
  for (const [key, operation] of openapi.operations) {
    const shape = `${operation.method} ${shapeOf(operation.path)}`;
    if (!openapiByShape.has(shape)) openapiByShape.set(shape, []);
    openapiByShape.get(shape).push({ ...operation, key });
  }

  // Who imports each client module, and were any import shapes not understood?
  const importers = buildImporterIndex(ts, files);
  const callerIndex = importers.index;
  const unresolvedImports = importers.unresolved;
  const byRel = new Map(files.map((f) => [f.rel, f]));
  const entries = new Map();
  const unknownRecords = [];
  const extractorNotes = [];

  const upsert = (key) => {
    if (!entries.has(key)) {
      entries.set(key, {
        key,
        method: null,
        path: null,
        expression: null,
        operation_id: null,
        openapi: 'missing',
        experimental: false,
        backend_route: null,
        router_path: null,
        contract_security: null,
        contract_transports: [],
        old_clients: [],
        bridge: [],
        legacy_transport: [],
        callers: [],
        test_callers: [],
        query_params: [],
        checks: { auth: 'unknown', envelope: 'unknown', command_id: 'unknown', retry: 'unknown', project_scope: 'unknown', transport: 'unknown' },
        evidence: [],
        status: 'unknown',
        records: [],
      });
    }
    return entries.get(key);
  };

  // Contract-first pass: every OpenAPI operation belongs to the ledger even
  // with no frontend caller. The router lookup happens here too, because
  // "the contract declares it" and "the backend registers it" are different
  // facts (status contract_only vs backend_unregistered) - and a contract
  // operation with no frontend caller would otherwise never be matched against
  // the router at all.
  for (const [key, operation] of openapi.operations) {
    const entry = upsert(key);
    entry.method = operation.method;
    entry.path = operation.path;
    entry.operation_id = operation.operation_id;
    entry.openapi = operation.experimental ? 'experimental' : 'stable';
    entry.experimental = operation.experimental;
    entry.contract_security = operation.security === null ? 'inherit' : operation.security.length === 0 ? 'none' : 'SidecarBearer';
    entry.contract_transports = operation.content_types;
    entry.contract_response_statuses = operation.response_statuses;
    entry.contract_header_params = operation.header_params;
    const routeMatches = routeByShape.get(`${operation.method} ${shapeOf(operation.path)}`) ?? [];
    if (routeMatches.length > 0) {
      entry.backend_route = routeMatches[0].handler;
      entry.router_path = routeMatches[0].path;
    }
  }

  // Source pass. `crossFile` inlines a binding imported from another module
  // (e.g. `import { API_ENDPOINTS } from '../api'`), which is how a services
  // module's `getBase()` indirection reaches a concrete `/api/v1/...` path.
  const fileByRel = new Map(files.map((file) => [file.rel, file]));
  const lookupFiles = new Set(files.map((file) => file.rel));
  const mergedByRel = new Map();
  const mergedBindsFor = (file) => {
    if (!mergedByRel.has(file.rel)) {
      mergedByRel.set(file.rel, mergeImportedBinds(file, files, fileByRel, lookupFiles));
    }
    return mergedByRel.get(file.rel);
  };
  const crossFileFor = (file) => (namespace, member) => {
    const info = file.model.imports.get(namespace);
    if (!info) return null;
    const targetRel = resolveImportPath(file.rel, info.module, lookupFiles);
    if (!targetRel) return null;
    const target = fileByRel.get(targetRel);
    if (!target) return null;
    const bind = target.model.binds.get(`${info.imported}.${member}`);
    if (!bind) return null;
    return {
      kind: bind.kind,
      text: bind.text,
      params: bind.params,
      binds: mergedBindsFor(target),
    };
  };

  for (const file of files) {
    for (const site of file.model.callSites) {
      // In-file resolution happened while walking the module (it can see the
      // whole file). Only when that failed is the import-aware pass tried,
      // which can inline a binding that lives in another module.
      let resolution = site.resolution ?? { path: null, kind: 'unknown', raw: site.rawUrl, note: 'not resolved in file' };
      if (resolution.kind !== 'resolved' || resolution.unresolvedParts?.length > 0) {
        // `unresolvedParts` lists dotted table members the in-file pass could
        // only placeholder (`{API_ENDPOINTS.projects}`); the import-aware pass
        // can inline them, and a failure here is reported as unknown rather
        // than as a call to a path the backend never registered.
        const env = { base: '', binds: mergedBindsFor(file), params: site.params, crossFile: crossFileFor(file) };
        const raw = site.constInitializer ?? site.rawUrl;
        const cross = resolvePath(raw, env);
        if (cross.kind === 'resolved') resolution = cross;
        else if (resolution.unresolvedParts?.length > 0) resolution = { path: null, kind: 'unknown', raw, note: cross.note };
      }
      if (resolution.kind !== 'resolved' || !resolution.path || resolution.path === '{param}') {
        unknownRecords.push({
          file: file.rel,
          function: site.scope,
          line: site.line,
          method: site.method,
          expression: site.rawUrl,
          reason: resolution.note ?? 'url expression is not statically resolvable',
          layer: file.layer,
        });
        continue;
      }
      const { path: rawPath, query } = splitQuery(resolution.path);
      const shape = `${site.method} ${shapeOf(rawPath)}`;
      const openapiMatches = openapiByShape.get(shape) ?? [];
      const routeMatches = routeByShape.get(shape) ?? [];
      const contract = openapiMatches.length > 0 ? openapiMatches[0] : null;
      const registered = routeMatches.length > 0;
      const isWrite = WRITE_METHODS.has(site.method);
      const key = contract ? contract.key : `${site.method} ${rawPath}`;
      const entry = upsert(key);
      entry.method = site.method;
      if (contract) {
        entry.path = contract.path;
        entry.operation_id = contract.operation_id;
        entry.openapi = contract.experimental ? 'experimental' : 'stable';
        entry.experimental = contract.experimental;
        entry.contract_security = contract.security === null ? 'inherit' : contract.security.length === 0 ? 'none' : 'SidecarBearer';
        entry.contract_transports = contract.content_types;
        entry.contract_response_statuses = contract.response_statuses;
        entry.contract_header_params = contract.header_params;
      } else {
        entry.path = entry.path ?? rawPath;
        if (!entry.openapi_note) {
          entry.openapi_note = 'a frontend call reaches this path but no OpenAPI operation matches its shape';
        }
      }
      entry.expression = entry.expression ?? resolution.path;
      entry.query_params = sortStrings([...entry.query_params, ...query]);
      if (registered && !entry.backend_route) {
        entry.backend_route = routeMatches[0].handler;
        entry.router_path = routeMatches[0].path;
      }
      entry.records.push({
        ...site,
        file: file.rel,
        layer: file.layer,
        resolvedPath: rawPath,
      });
    }
  }

  // Fold records into the user-facing entry.
  for (const entry of entries.values()) {
    const records = entry.records.map((record) =>
      classifyRecord(record, {
        shared,
        contractTransport: contractTransportOf(entry.contract_transports ?? []),
        isWrite: WRITE_METHODS.has(record.method),
        registered: entry.backend_route !== null,
      }),
    );

    entry.old_clients = sortStrings(records.filter((r) => r.layer === 'old_client').map((r) => `${r.file}#${r.scope}`));
    entry.bridge = sortStrings(records.filter((r) => r.layer === 'bridge').map((r) => `${r.file}#${r.scope}`));
    entry.legacy_transport = sortStrings(records.filter((r) => r.layer === 'shared').map((r) => `${r.file}#${r.scope}`));

    const directCallers = new Set();
    const testCallers = new Set();
    for (const record of records.filter((r) => r.layer === 'caller' || r.layer === 'test')) {
      const symbol = `${record.file}#${record.scope}`;
      if (isTestPath(record.file)) testCallers.add(symbol);
      else directCallers.add(symbol);
    }

    /**
     * Every import of an owner module counts as reachability, whether or not
     * the imported name is called in this file: `useQuery({ queryFn: f })`,
     * `onClick={f}`, a member of a default-exported object and a re-export
     * chain are all recorded by buildImporterIndex. `symbol` is the file plus
     * the scope where the name is evaluated (or `module` for a re-export).
     */
    const importEvidence = [];
    for (const owner of [...entry.old_clients, ...entry.bridge]) {
      if (!byRel.has(owner.split('#')[0])) continue;
      for (const [caller, detail] of callersOf(callerIndex, owner)) {
        const uses = detail.usedBy.length > 0 ? detail.usedBy : [{ scope: 'module', context: 're-export or unattributable import', member: null }];
        for (const use of uses) {
          const symbol = `${caller}#${use.scope}`;
          if (isTestPath(caller)) testCallers.add(symbol);
          else directCallers.add(symbol);
          importEvidence.push({
            client: owner,
            importer: caller,
            scope: use.scope,
            context: use.context,
            member: use.member,
            aliases: detail.aliases,
            reexported: detail.reexported,
            unresolved: detail.unresolved,
            test: isTestPath(caller),
          });
        }
      }
    }
    entry.callers = sortStrings([...directCallers]);
    entry.test_callers = sortStrings([...testCallers]);
    if (importEvidence.length > 0) {
      entry.import_evidence = uniqBy(
        importEvidence.map((evidence) => pruneUndefined({ ...evidence, aliases: evidence.aliases.length > 0 ? sortStrings(evidence.aliases) : undefined })),
        'importer',
      );
    }

    const bridgeCalledFromOld = [];
    for (const owner of entry.bridge) {
      for (const [caller] of callersOf(callerIndex, owner)) {
        const source = byRel.get(caller);
        if (source && source.layer === 'old_client') bridgeCalledFromOld.push(`${caller} -> ${owner}`);
      }
    }
    if (bridgeCalledFromOld.length > 0) entry.bridge_called_from_old = sortStrings(bridgeCalledFromOld);

    const isWrite = WRITE_METHODS.has(entry.method);
    const registered = entry.backend_route !== null;
    entry.checks = {
      auth: firstConcrete(records.map((r) => r.auth)),
      envelope: firstConcrete(records.map((r) => r.envelope), contractEnvelopeOf(entry)),
      command_id: !isWrite ? 'n/a' : !registered ? 'unknown' : firstConcrete(records.map((r) => r.command_id), 'no'),
      retry: firstConcrete(records.map((r) => r.retry)),
      project_scope: projectScopeOf(entry.path),
      transport: transportOfEntry(entry, records),
    };
    entry.check_notes = uniqBy(
      records
        .map((r) => ({
          site: `${r.file}#${r.scope}`,
          transport: r.transport_note,
          auth: r.auth_note,
          envelope: r.envelope_note,
          command_id: r.command_id_note,
          retry: r.retry_note,
        }))
        .filter((n) => n.transport || n.auth || n.envelope || n.command_id || n.retry),
      'site',
    );
    entry.evidence = uniqBy(
      records.map((r) => ({
        site: `${r.file}#${r.scope}`,
        line: r.line,
        method: r.method,
        url: r.resolvedPath,
        expression: r.rawUrl,
        transport: r.transport,
        auth: r.auth,
        envelope: r.envelope,
        command_id: r.command_id,
        retry: r.retry,
      })),
      'site',
    );

    entry.status = statusOf(entry);
    entry.deletion_condition = deletionCondition(entry);
    delete entry.records;
    delete entry.contract_response_statuses;
    delete entry.contract_header_params;
  }

  const list = [...entries.values()].sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
  attachGeneratedOperations(list, generatedOps);
  const cleaned = list.map((entry) => pruneUndefined(entry));

  const nonJsonEntries = cleaned
    .filter((entry) => entry.checks.transport !== 'json')
    .map((entry) => ({
      key: entry.key,
      transport: entry.checks.transport,
      status: entry.status,
      old_clients: entry.old_clients,
      bridge: entry.bridge,
      callers: entry.callers,
      note: entry.check_notes.find((n) => n.transport)?.transport ?? null,
    }));

  const mockEntryPoints = summarizeMockEntryPoints(inputs.mockInputs);

  return {
    ledger: {
      generator: 'scripts/frontend-endpoint-map.mjs',
      plan_step: 'T12.03.a (plan section 28) / plan section 15 T12.03',
      // Forward slashes on every platform: the committed ledger is compared
      // byte for byte by --check, on Windows and on Linux CI alike.
      generated_from: {
        openapi: toPosix(inputs.rel.openapi),
        router: toPosix(inputs.rel.router),
        generated_types: toPosix(inputs.rel.generatedTypes),
        scanned_root: toPosix(scanRoot),
      },
      extractor,
      extractor_note: 'types parsed with the TypeScript compiler API; router.go with group-prefix resolution',
      methodology: METHODOLOGY,
      counts: {},
      entries: cleaned,
      non_json_entries: nonJsonEntries,
      unknown_entries: unknownRecords.sort((a, b) => (a.expression < b.expression ? -1 : 1)),
      unresolved_imports: unresolvedImports.sort((a, b) => `${a.file}${a.local}`.localeCompare(`${b.file}${b.local}`)),
      schema_coverage: inputs.schemaCoverage,
      mock_entry_points: mockEntryPoints,
      notes: extractorNotes,
    },
  };
}

function contractEnvelopeOf(entry) {
  if (!entry.contract_transports?.includes('application/json')) return 'unknown';
  // The 3 contract-first operations answer `{error, meta}`; everything else
  // under the legacy backend answers `{success, data, error}`.
  return entry.operation_id ? 'v3' : 'legacy';
}

/**
 * The entry's transport: the call sites are authoritative. An endpoint nobody
 * calls is still classified from the contract (an SSE or CSV export route is
 * not "unknown"), so T12.03.b can see the non-JSON set up front.
 */
function transportOfEntry(entry, records) {
  if (records.length > 0) return firstConcrete(records.map((r) => r.transport));
  const declared = entry.contract_transports ?? [];
  const fromContract = contractTransportOf(declared);
  if (fromContract) return fromContract;
  if (declared.length > 0 && !declared.some((type) => type.includes('json'))) return 'unknown';
  return 'json';
}

/**
 * The status of one endpoint, from the evidence the table collected. The
 * vocabulary and what a deletion does to the row are documented next to
 * STATUS_RULES below; there is deliberately no `migrated` state, because "the
 * old client is gone" and "the old client has no caller" are different facts
 * and only the second one is safe to act on.
 */
export function statusOf(entry) {
  const hasClients = entry.old_clients.length > 0 || entry.bridge.length > 0;
  const hasCallers = entry.callers.length > 0;
  const hasTests = entry.test_callers.length > 0;
  const knownToBackend = entry.openapi !== 'missing' || entry.backend_route !== null;

  // A path that is nothing but placeholders (`{url}`, `{param}`,
  // `{API_ENDPOINTS.projects}`) is not a route: the URL expression was never
  // attributed, so no migration or deletion decision may be based on the row.
  if (/^\{[^}]*\}(?:\/\{[^}]*\})*$/.test(entry.path ?? '')) return 'unknown';

  if (!hasClients && !hasCallers) {
    if (!knownToBackend) {
      // A test-only call to a path the backend never registered is still a
      // call the table can attribute; a bare URL fragment is not.
      return hasTests ? 'orphan_client' : 'unknown';
    }
    if (entry.openapi !== 'missing') {
      // Contract declared. Two distinct facts: the router registering it is
      // what makes "contract drift" impossible.
      return entry.backend_route !== null ? 'contract_only' : 'backend_unregistered';
    }
    // No contract operation, but router.go registers the path. Neither
    // `contract_only` (no contract) nor `orphan_client` (the router knows it)
    // describes this, and it is exactly the state a legacy_unused row lands in
    // once its client is deleted.
    return 'route_only';
  }

  // Frontend code calls a path neither the contract nor the router knows.
  if (!knownToBackend) return 'orphan_client';
  if (entry.old_clients.length > 0 && entry.bridge.length > 0) return 'both';
  if (entry.bridge.length > 0) return 'bridge_only';
  // No client module holds this path: it is called through the shared
  // transport or a raw fetch. There is nothing to migrate and nothing to
  // delete; T12.03.b must not hunt for a module here.
  if (entry.old_clients.length === 0) return 'legacy_direct';
  // Old client only. `legacy_only` while any production code still calls that
  // module - another old client counts too: a module the services layer itself
  // calls is not dead, and deleting it first would break that caller.
  // `legacy_unused` only when no production caller is left (test callers do not
  // count): dead code with the module still sitting there, the state the old
  // report mislabelled `migrated`.
  return entry.callers.length > 0 ? 'legacy_only' : 'legacy_unused';
}

/**
 * What the row becomes when T12.03.b deletes everything the row's *status*
 * allows it to delete. This is deliberately not prose written per status: it
 * calls `statusOf` on a copy of the entry after the deletion the status
 * documents, so the result can never drift away from the predicate - the
 * failure mode the first review caught in the old `migrated` text, where
 * deleting a client with no bridge was said to leave `bridge_only`.
 *
 *   both           delete the old client (its callers move onto the existing
 *                  bridge first when there are any) -> `statusOf` with
 *                  `old_clients` emptied
 *   legacy_only    the callers move onto the bridge client T12.03.b writes for
 *                  this row, then the old client is deleted -> `statusOf` with
 *                  `old_clients` emptied and a bridge placeholder; the
 *                  placeholder is only there so `statusOf` can classify, and it
 *                  never appears in any output
 *   legacy_unused  delete the old client -> `statusOf` with `old_clients`
 *                  emptied; `unknown` (nothing seeds the row any more) means
 *                  the row leaves the table
 *   legacy_direct  remove the direct call site(s) -> `statusOf` with callers
 *                  emptied; there is no module to delete
 *   bridge_only    retire the bridge call site -> `statusOf` with the bridge
 *                  and the callers emptied; nothing legacy is deleted here
 *
 * States that are not "delete one of our clients" rows (`contract_only`,
 * `route_only`, `backend_unregistered`, `orphan_client`) repair the contract
 * or the router, or add the missing attribution, and report no after-state;
 * `unknown` is not a route and reports none either.
 */
export function deleteImpact(entry) {
  if (entry.status === 'both') return statusOf({ ...entry, old_clients: [] });
  if (entry.status === 'legacy_only') {
    return statusOf({ ...entry, old_clients: [], bridge: ['(the bridge client T12.03.b writes)'] });
  }
  if (entry.status === 'legacy_unused') return statusOf({ ...entry, old_clients: [] });
  if (entry.status === 'legacy_direct') return statusOf({ ...entry, callers: [], test_callers: [] });
  if (entry.status === 'bridge_only') return statusOf({ ...entry, bridge: [], callers: [], test_callers: [] });
  return null;
}

/**
 * A standalone clause naming the state the row reads after the deletion
 * `deleteImpact` simulated. `unknown` is spelled out because it means the row
 * leaves the table rather than staying in it under a different name.
 */
export function afterStateClause(entry, after) {
  if (after === null) return '';
  if (after === 'unknown') {
    return 'after the edit the row leaves this table (no client, caller, contract operation or route is left on it)';
  }
  const declares = entry.openapi !== 'missing' ? 'the contract declares it' : 'the contract does not declare it';
  const registers = entry.backend_route !== null ? 'the router registers it' : 'the router does not register it';
  if (after === 'contract_only' || after === 'route_only' || after === 'backend_unregistered' || after === 'orphan_client') {
    return `after the edit the row reads ${after} (${declares}; ${registers})`;
  }
  return `after the edit the row reads ${after}`;
}

/**
 * What T12.03.b must do to this row. Every status gets a line, and the "after"
 * half comes from `deleteImpact` (which asks `statusOf` itself), so the text
 * cannot drift away from the predicate.
 */
function deletionCondition(entry) {
  const old = entry.old_clients.join(', ');
  const bridge = entry.bridge.join(', ');
  const callers = entry.callers.length > 0 ? entry.callers.join(', ') : '(no external caller found)';
  const after = afterStateClause(entry, deleteImpact(entry));
  switch (entry.status) {
    case 'both':
      return entry.callers.length > 0
        ? `T12.03.b moves the caller(s) ${callers} from ${old} onto ${bridge} and then deletes ${old} once this row's old_clients empties; ${after}. The bridge call must stay - dropping it too would leave the endpoint with no client at all.`
        : `the old client ${old} has no caller left (nothing imports it as a value), so it can be deleted directly; the bridge ${bridge} keeps the endpoint covered; ${after}.`;
    case 'legacy_only':
      return `T12.03.b writes the bridge client for this row, rewrites the caller(s) ${callers} from ${old} onto it, and then deletes ${old} (nothing else imports that module); ${after}. Until that bridge client exists the caller cannot simply be dropped: the endpoint (${entry.key}) is still in use.`;
    case 'contract_only':
      return `the contract${entry.backend_route !== null ? ' and router.go' : ''} declare it and no frontend caller exists; remove the operation from openapi.yaml (and the route from router.go) to drop the row - there is no frontend code to change.`;
    case 'route_only':
      return `router.go registers the path but openapi.yaml declares no such operation and no frontend caller exists; register the contract operation (or remove the route) - a frontend change cannot clear it.`;
    case 'backend_unregistered':
      return `the contract declares this operation but backend/internal/api/router.go registers no matching route; remove the contract operation (or register the route) to drop the row - a frontend change cannot clear it.`;
    case 'orphan_client':
      return `frontend code calls a path the contract and the router both miss; remove the call site or add the route + operation. T12.03.b must not delete anything here on the strength of this row alone.`;
    case 'unknown':
      return `no endpoint could be attributed to this call site, so it is not a route and must not be migrated or deleted from this table; the row disappears when the call site does.`;
    case 'legacy_unused': {
      const testNote =
        entry.test_callers.length > 0
          ? `only test files evaluate ${old} as a value (${entry.test_callers.join(', ')}), and a test-only caller does not keep the module alive`
          : `nothing imports ${old} as a value at all`;
      const breakNote = entry.test_callers.length > 0 ? ', but deleting the module will break that test' : '';
      return `no production caller exists - ${testNote}, and no call site calls it - so there is no caller to migrate; deleting ${old} drops the old client from this row; ${after}. This is NOT a migration: no caller moves onto a bridge${breakNote}.`;
    }
    case 'legacy_direct':
      return `no client module holds this path - the call site(s) ${callers} call it directly through the shared transport/fetch, so there is no module to delete; the migration is removing or rewriting those call sites, and ${after}.`;
    case 'bridge_only':
      return `only the bridge calls it, so no legacy client has to be deleted here; if the bridge call site ${bridge} is retired too, ${after}, and the row leaves the table only once nothing backend-side is left on it (the contract operation and/or the route removed).`;
    default:
      return null;
  }
}

const METHODOLOGY = {
  key: '`METHOD /openapi/path/template`. A frontend call whose shape matches no contract operation keeps its resolved literal path; a URL expression this extractor cannot resolve is listed in unknown_entries instead of being guessed.',
  sources: {
    router: 'backend/internal/api/router.go: group aliases resolve to full prefixes, `:param` normalizes to `{param}`, the handler name is the registered function.',
    openapi: 'backend/docs/openapi.yaml: operation set, experimental flag, response content types.',
    generated_types: 'apps/workbench/generated/openapi-types.ts: OpenApiOperationMap is read back to tie the endpoint to the generator contract.',
    clients: 'apps/workbench/services/** is the old client layer; apps/workbench/src/services-bridge/** is the bridge; apps/workbench/api.ts is the shared transport both import helpers from.',
    callers:
      'every .ts/.tsx under apps/workbench is parsed with the TypeScript AST. An import binding is followed (aliases, namespace imports, default-object imports, dynamic import() and re-export chains included) and a module counts as imported whenever the imported name is evaluated as a value in that file: called, passed (`useQuery({ queryFn: f })`, `onClick={f}`), spread, or returned. Type-only positions never count. Anything the walker cannot attribute goes to `unresolved_imports` and its endpoint is never reported as caller-free.',
  },
  values: {
    auth: AUTH_VALUES.join(' | '),
    envelope: ENVELOPE_VALUES.join(' | '),
    command_id: COMMAND_ID_VALUES.join(' | '),
    retry: RETRY_VALUES.join(' | '),
    transport: TRANSPORT_VALUES.join(' | '),
    project_scope: 'project | global',
    status: STATUS_ORDER.join(' | '),
  },
  rulings: [
    '`command_id` is only asked of the methods that change state (POST/PUT/PATCH/DELETE); `n/a` means the method does not change state, `unknown` means the backend does not register it so no idempotency contract exists, and only a registered state-changing route with no Idempotency-Key on the call site reads `no`.',
    '`retry: fallback_mock` is reserved for a src/mocks answer behind import.meta.env.DEV; a hard-coded fallback that answers a *failed request* in production is `fallback_static` and is called out separately because the plan forbids it in production.',
    '`project_scope` is path-based. A project id carried only as a query filter or a request-body field is `global` - migrating such a call is not a scope change.',
    '`orphan_client` is a frontend call to a path the backend never registered; `contract_only` is a contract+router endpoint nobody calls, and `backend_unregistered` is a contract operation the router never registers - the three are different facts and are never merged.',
    '`legacy_only` vs `legacy_unused`: both mean only the old client calls it. The difference is whether any production code - another old client included - still calls that client module. Only `legacy_unused` is dead code; `legacy_only` means T12.03.b has a caller to rewrite first. `legacy_direct` covers a path called through the shared transport with no client module at all - nothing to delete there.',
    'auth/envelope/command_id are read off the call site source (regex over the enclosing statement plus the AST shape); each row carries a `check_notes` line naming the evidence, and a call site with no readable evidence reads `unknown` rather than a guess.',
  ],
};

// ---------------------------------------------------------------------------
// DEV mock entry points (for the T12.03.c ProductionBuildHasNoMockTransport)
// ---------------------------------------------------------------------------

export function summarizeMockEntryPoints({ storeFiles, injectionSites, nonDevFallbacks }) {
  return {
    dev_flag: DEV_FLAG,
    dev_flag_note:
      'Vite statically replaces import.meta.env.DEV; every mock import sits behind that flag, so a production bundle drops the branch.',
    flags: [DEV_FLAG],
    store_files: sortStrings(storeFiles),
    injection_sites: uniqBy(injectionSites, 'site'),
    non_dev_fallbacks: sortStrings(nonDevFallbacks),
    production_transport:
      'A production build must contain no mock transport: every injection site below must stay behind the DEV flag, and non_dev_fallbacks must stay empty. Entries whose note says production_unreachable never run outside DEV.',
  };
}

// ---------------------------------------------------------------------------
// analysis driver
// ---------------------------------------------------------------------------

const TS_EXTENSIONS = ['.ts', '.tsx'];

function classifyLayer(rel, options) {
  if (options.sharedFiles.has(rel)) return 'shared';
  // A test file never counts as production evidence: its hits feed
  // `test_callers`, whichever directory it lives in.
  if (isTestPath(rel)) return 'test';
  if (options.oldClientFiles.has(rel)) return 'old_client';
  if (options.bridgeFiles.has(rel)) return 'bridge';
  return 'caller';
}

/**
 * Run the whole extraction over a checkout. `paths` entries are relative to
 * `root`; the return value is the object written to endpoint-map.json.
 */
export function analyze({ root, paths, check = false }) {
  // Fail closed before anything else: no compiler, no ledger (and no output
  // file is touched, because the caller never gets past this line).
  const ts = loadTypescript();
  const extractor = 'typescript';
  const openapiPath = path.join(root, paths.openapi);
  const routerPath = path.join(root, paths.router);
  const generatedPath = path.join(root, paths.generatedTypes);

  const YAML = loadYaml();
  const doc = YAML.parse(fs.readFileSync(openapiPath, 'utf8'));
  const openapi = parseOpenApiDocument(doc);
  const routerRoutes = parseRouterRoutes(fs.readFileSync(routerPath, 'utf8'));
  const generatedSource = fs.readFileSync(generatedPath, 'utf8');
  const generatedOps = parseGeneratedOperationMap(generatedSource);

  const scanRoot = path.join(root, paths.scanDir);
  const scanned = walkFiles(scanRoot, TS_EXTENSIONS);
  const relToRoot = (abs) => toPosix(path.relative(root, abs));
  const oldClientFiles = new Set(
    paths.oldClientDirs.flatMap((dir) => walkFiles(path.join(root, dir), TS_EXTENSIONS)).map(relToRoot),
  );
  const bridgeFiles = new Set(
    paths.bridgeDirs.flatMap((dir) => walkFiles(path.join(root, dir), TS_EXTENSIONS)).map(relToRoot),
  );
  const sharedFiles = new Set(paths.sharedTransportFiles.map((file) => toPosix(file)));
  const layerOptions = { oldClientFiles, bridgeFiles, sharedFiles };

  const files = scanned.map((abs) => {
    const rel = relToRoot(abs);
    const source = fs.readFileSync(abs, 'utf8');
    const layer = classifyLayer(rel, layerOptions);
    const { model } = analyzeFile(ts, { abs, rel, source, layer, base: '', params: new Map() });
    return { rel, abs, layer, source, model };
  });

  const sharedSource = sharedFiles.size > 0 ? fs.readFileSync(path.join(root, [...sharedFiles][0]), 'utf8') : '';
  const shared = sharedTransportFacts(sharedSource);

  const mockInputs = collectMockInputs(files, sharedFiles);

  const openapiFeatures = countOpenApiSchemaFeatures(doc);
  const generatedFeatures = countGeneratedTypeFeatures(ts, generatedSource, generatedPath);

  const built = buildEndpointMap({
    openapi,
    routerRoutes,
    generatedOps,
    files,
    extractor,
    shared,
    ts,
    scanRoot: paths.scanDir,
    mockInputs,
    rel: { openapi: paths.openapi, router: paths.router, generatedTypes: paths.generatedTypes },
    schemaCoverage: schemaCoverageFrom(openapiFeatures, generatedFeatures),
  });

  const unmatched = built.ledger.entries.filter((e) => e.openapi === 'missing' && e.backend_route === null);
  const backendUnregistered = built.ledger.entries.filter((e) => e.status === 'backend_unregistered');
  built.ledger.notes = [
    ...built.ledger.notes,
    `${scanned.length} workbench source files scanned (${files.filter((f) => f.layer === 'old_client').length} old client, ${files.filter((f) => f.layer === 'bridge').length} bridge, ${files.filter((f) => f.layer === 'shared').length} shared transport).`,
    unmatched.length > 0
      ? `${unmatched.length} endpoint(s) are neither in the contract nor in the router: ${unmatched.map((e) => e.key).join(', ')}`
      : 'every entry either matches a contract operation or a registered route.',
    backendUnregistered.length > 0
      ? `${backendUnregistered.length} contract operation(s) have no matching router registration (status backend_unregistered): ${backendUnregistered.map((e) => e.key).join(', ')}`
      : 'every contract operation has a matching router registration.',
    built.ledger.unresolved_imports.length > 0
      ? `${built.ledger.unresolved_imports.length} import(s) could not be attributed to a scanned module or a value use; they are listed in unresolved_imports and no endpoint is treated as caller-free because of one.`
      : 'every import resolved to a scanned module and a value use.',
  ];
  built.ledger.counts = summarize(built.ledger.entries, built.ledger.unknown_entries.length, built.ledger.non_json_entries.length);
  return { ledger: built.ledger, markdown: renderMarkdown(built.ledger) };
}

function summarize(entries, unknownCount, nonJsonCount) {
  const byStatus = {};
  for (const entry of entries) byStatus[entry.status] = (byStatus[entry.status] ?? 0) + 1;
  const contract = entries.filter((e) => e.openapi !== 'missing');
  return {
    total: entries.length,
    by_status: Object.fromEntries(Object.entries(byStatus).sort(([a], [b]) => (a < b ? -1 : 1))),
    by_openapi: {
      stable: entries.filter((e) => e.openapi === 'stable').length,
      experimental: entries.filter((e) => e.openapi === 'experimental').length,
      missing: entries.filter((e) => e.openapi === 'missing').length,
    },
    // "no client module holds it" - NOT the same as the contract_only status:
    // an orphan_client or a legacy_direct row can also carry no client.
    without_client: entries.filter((e) => e.old_clients.length === 0 && e.bridge.length === 0).length,
    reached_by_frontend: entries.filter((e) => e.old_clients.length > 0 || e.bridge.length > 0 || e.callers.length > 0).length,
    with_bridge: entries.filter((e) => e.bridge.length > 0).length,
    with_old_client: entries.filter((e) => e.old_clients.length > 0).length,
    write_endpoints: entries.filter((e) => WRITE_METHODS.has(e.method)).length,
    command_id_yes: entries.filter((e) => e.checks.command_id === 'yes').length,
    command_id_no: entries.filter((e) => e.checks.command_id === 'no').length,
    command_id_unknown: entries.filter((e) => e.checks.command_id === 'unknown').length,
    non_json: nonJsonCount,
    unknown_urls: unknownCount,
    contract_operations: contract.length,
    registered_routes: entries.filter((e) => e.backend_route !== null).length,
  };
}

function collectMockInputs(files, sharedFiles) {
  const isMockFile = (rel) => /\/src\/mocks\//.test(toPosix(rel));
  const storeFiles = files.map((file) => file.rel).filter(isMockFile);
  const injectionSites = [];
  const nonDevFallbacks = [];
  const importRe = /(?:from\s+|import\(\s*)['"]([^'"]+)['"]/g;
  for (const file of files) {
    if (isMockFile(file.rel) || sharedFiles.has(file.rel)) continue;
    const modules = new Set();
    let importReMatch;
    while ((importReMatch = importRe.exec(file.source)) !== null) {
      // `../mocks`, `../mocks/workspace`, `../src/mocks` - any module whose
      // last segment is `mocks` or which lives under a mocks directory.
      if (/(^|\/)mocks(\/|$)/.test(importReMatch[1])) modules.add(importReMatch[1]);
    }
    if (modules.size === 0) continue;
    const devCount = countOccurrences(file.source, DEV_FLAG);
    const staticImport = /from\s+['"][^'"]*mocks[^'"]*['"]/.test(file.source);
    const guarded = devCount > 0;
    injectionSites.push({
      site: file.rel,
      layer: file.rel.startsWith('apps/workbench/services/') ? 'old_client_unmigrated' : file.layer,
      rule: ruleOf(file.rel),
      mode: staticImport ? 'static import' : 'dynamic import()',
      modules: sortStrings([...modules]),
      dev_flag_occurrences: devCount,
      guarded,
      note: guarded
        ? 'inside an `if (import.meta.env.DEV)` branch: absent from production builds'
        : 'NOT behind the DEV flag - production_critical',
    });
    // The failure mode T12.03.c guards against: a mocks import that a
    // production build can reach (no DEV guard, or a static top-level one).
    if (!guarded) nonDevFallbacks.push(`${file.rel} imports src/mocks without a DEV guard`);
    else if (staticImport) nonDevFallbacks.push(`${file.rel} has a static mocks import; only dynamic import() inside the DEV branch is production-safe`);
  }
  return {
    storeFiles: sortStrings(storeFiles),
    injectionSites,
    nonDevFallbacks,
  };
}

function countOccurrences(source, needle) {
  let count = 0;
  let index = source.indexOf(needle);
  while (index >= 0) {
    count += 1;
    index = source.indexOf(needle, index + needle.length);
  }
  return count;
}

// ---------------------------------------------------------------------------
// markdown renderer
// ---------------------------------------------------------------------------

function escapeCell(value) {
  return String(value ?? '')
    .replace(/\|/g, '\\|')
    .replace(/\n/g, ' ')
    .trim();
}

function joinOrDash(values) {
  return values && values.length > 0 ? values.map((v) => `\`${v}\``).join('<br>') : '-';
}

/**
 * Recognised caller shapes and the limits of the static scan. The plan
 * forbids "replace imports globally and let `any` compile"; the same honesty
 * applies to the *detection* side, so what cannot be seen is written down
 * instead of being counted as "nobody calls it".
 */
const CALLER_LIMITATIONS = [
  'Recognised import shapes: `import { f } from "../services/x"`, `import { f as g } from ...`, `import * as x from ...` with `x.f()` or `x.f` passed as a value, `import api from ...` with `api.f()` (also when the default object merely gathers the module\'s named exports, `export default { f, g }`, followed through to the client that declares each member), `const api = await import("...")` with `api.f()`, and `export { f } from "./x"` / `export * from "./x"` barrels. A name the walker cannot tie to a member of a namespace/default import is covered by the fail-closed rule below, not dropped.',
  'A module counts as imported whenever the imported name is *evaluated as a value* in the importing file: called (`f()`, `x.f()`), passed (`useQuery({ queryFn: f })`, `onClick={f}`), spread (`{...f}`), returned, awaited, or used in JSX. A type-only use (`let g: typeof f`, `F extends Fn`) does NOT count, so a client kept alive only by a type import can still read `legacy_unused`.',
  'Not statically resolvable, so they land in `unresolved_imports` rather than being treated as callers: a name built at runtime (`import(`./${x}`)`, `require(variable)`), a member computed from a variable (`svc[name]()`), a module destructured or spread (`const { f } = svc`, `{...svc}`), a namespace/default binding escaping as a whole value (`return api`, `api[key]`, `Object.values(api)`), a function reached through `window`/globals, and a re-export the walker cannot follow past 8 hops. When a shape like that touches a client module, *every* name that module exports is recorded as possibly imported, so nothing inside it can read `legacy_unused` (or any other "no importer" verdict) on the strength of that file. The destructure/spread scan is deliberately coarse: one such shape anywhere in an importing file marks *every* import of that file as unattributable, so `unresolved_imports` can over-report (a file that destructures `props` in a component and separately imports a client marks that client too). It never under-reports, which is the direction deletion safety needs.',
  'The scan covers `.ts`/`.tsx` under apps/workbench only. Callers in `apps/`, `packages/` or a test in another package are outside it; `package.json` `exports`/alias imports (`@/...`) are not resolved either.',
  '`auth`, `envelope`, `command_id` and `retry` are read off the call-site source: the shared transport (`apps/workbench/api.ts`) supplies them for `get/post/put/patch/del` callers, while a raw `fetch` is judged from its own options and source text - the enclosing statement plus the enclosing function body (capped at 8000 characters; a call outside any function falls back to the first 4000 characters of the file), so a header or body read written a few statements away is still seen (regex-based, so a computed header name reads `unknown`). Every row carries `check_notes` with the exact evidence; `unknown` always means "no readable evidence", never a guess.',
  '`transport` is decided from the call site (WebSocket constructor, FormData, `.blob()`, `createObjectURL`/`download`, `for await`/`getReader`, an `Accept: text/event-stream` header) and, when nobody calls the endpoint, from the contract content types. A route that streams without any of those markers still reads `json`.',
];

/** One line per status, for the header table (kept beside STATUS_RULES). */
const STATUS_MEANING = {
  both: 'old client and bridge both call it - a caller has to move first',
  legacy_only: 'old client only, and production code (another old client included) still calls that client',
  legacy_unused: 'old client only and no production code calls that client - dead code, but no bridge behind it',
  legacy_direct: 'called through the shared transport / a raw fetch; there is no client module at all',
  bridge_only: 'only the bridge calls it; no legacy client left',
  contract_only: 'contract + router declare it, no frontend caller',
  route_only: 'router.go registers the path, the contract declares no operation, no frontend caller',
  backend_unregistered: 'the contract declares it, router.go registers no matching route',
  orphan_client: 'a frontend call reaches a path neither contract nor router knows',
  unknown: 'a call site whose URL could not be attributed to any endpoint (never guessed); placeholder-only paths land here too',
};

export function renderMarkdown(ledger) {
  const lines = [];
  const counts = ledger.counts;
  lines.push('# Frontend endpoint map (T12.03.a)');
  lines.push('');
  lines.push('Generated by `scripts/frontend-endpoint-map.mjs` - do not edit by hand.');
  lines.push('Regenerate with `node scripts/frontend-endpoint-map.mjs`; verify with `--check`.');
  lines.push('');
  lines.push(`Extractor: \`${ledger.extractor}\` - ${ledger.extractor_note}`);
  lines.push('');
  lines.push('## Counts');
  lines.push('');
  lines.push(`- endpoints: **${counts.total}** (contract operations: ${counts.contract_operations}, reached by the frontend: ${counts.reached_by_frontend})`);
  lines.push(`- contract: stable ${counts.by_openapi.stable} / experimental ${counts.by_openapi.experimental} / missing from the contract ${counts.by_openapi.missing}`);
  lines.push(`- clients: old ${counts.with_old_client} / bridge ${counts.with_bridge}`);
  lines.push(`- writes: ${counts.write_endpoints} (Idempotency-Key sent: ${counts.command_id_yes}, missing: ${counts.command_id_no})`);
  lines.push(`- non-JSON transports: ${counts.non_json} (see the section below before migrating)`);
  lines.push(`- unresolvable URL expressions: ${counts.unknown_urls}`);
  lines.push(`- imports the extractor could not attribute: ${ledger.unresolved_imports.length} (listed below; no endpoint is caller-free because of one)`);
  lines.push('');
  lines.push('| status | endpoints | meaning |');
  lines.push('| --- | ---: | --- |');
  for (const status of STATUS_ORDER) {
    if (counts.by_status[status] === undefined) continue;
    lines.push(`| ${status} | ${counts.by_status[status]} | ${STATUS_MEANING[status]} |`);
  }
  for (const [status, value] of Object.entries(counts.by_status)) {
    if (STATUS_ORDER.includes(status)) continue;
    lines.push(`| ${status} | ${value} | (unknown status) |`);
  }
  lines.push('');
  lines.push('## Ledger');
  lines.push('');
  lines.push('| endpoint | contract | backend route | old client | bridge | callers | auth | envelope | command id | retry | scope | transport | status |');
  lines.push('| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |');
  for (const entry of ledger.entries) {
    lines.push(
      `| \`${escapeCell(entry.key)}\` | ${entry.openapi}${entry.operation_id ? ` (\`${entry.operation_id}\`)` : ''} | ${
        entry.backend_route ? `\`${entry.backend_route}\`` : '-'
      } | ${joinOrDash(entry.old_clients)} | ${joinOrDash(entry.bridge)} | ${entry.callers.length} | ${entry.checks.auth} | ${
        entry.checks.envelope
      } | ${entry.checks.command_id} | ${entry.checks.retry} | ${entry.checks.project_scope} | ${entry.checks.transport} | ${entry.status} |`,
    );
  }
  lines.push('');

  if (ledger.entries.some((entry) => entry.deletion_condition)) {
    lines.push('## Deletion conditions (T12.03.b hand-off)');
    lines.push('');
    for (const entry of ledger.entries.filter((e) => e.deletion_condition)) {
      lines.push(`- \`${entry.key}\` (${entry.status}): ${entry.deletion_condition}`);
    }
    lines.push('');
  }

  lines.push('## Non-JSON entries (cannot migrate as ordinary JSON)');
  lines.push('');
  if (ledger.non_json_entries.length === 0) {
    lines.push('None.');
  } else {
    lines.push('| endpoint | transport | status | old client | bridge | note |');
    lines.push('| --- | --- | --- | --- | --- | --- |');
    for (const entry of ledger.non_json_entries) {
      lines.push(
        `| \`${escapeCell(entry.key)}\` | ${entry.transport} | ${entry.status} | ${joinOrDash(entry.old_clients)} | ${joinOrDash(entry.bridge)} | ${escapeCell(entry.note ?? '-')} |`,
      );
    }
  }
  lines.push('');

  if (ledger.unknown_entries.length > 0) {
    lines.push('## Unresolvable URL expressions (not guessed)');
    lines.push('');
    lines.push('| file | function | line | method | expression | reason |');
    lines.push('| --- | --- | ---: | --- | --- | --- |');
    for (const entry of ledger.unknown_entries) {
      lines.push(
        `| \`${entry.file}\` | \`${entry.function}\` | ${entry.line} | ${entry.method} | \`${escapeCell(entry.expression)}\` | ${escapeCell(entry.reason)} |`,
      );
    }
    lines.push('');
  }

  if (ledger.unresolved_imports.length > 0) {
    lines.push('## Imports the extractor could not attribute');
    lines.push('');
    lines.push(
      'These imports are neither a call/value use the walker could see nor a re-export it could follow. They are listed so a reader can tell "the extractor is sure nobody imports this client" from "the extractor gave up": no endpoint is reported caller-free because of a line here.',
    );
    lines.push('');
    lines.push('| file | local | module | reason |');
    lines.push('| --- | --- | --- | --- |');
    for (const entry of ledger.unresolved_imports) {
      lines.push(`| \`${escapeCell(entry.file)}\` | \`${escapeCell(entry.local)}\` | \`${escapeCell(entry.module)}\` | ${escapeCell(entry.reason)} |`);
    }
    lines.push('');
  }

  lines.push('## What this table can and cannot see');
  lines.push('');
  for (const line of CALLER_LIMITATIONS) lines.push(`- ${line}`);
  lines.push('');

  lines.push('## Schema coverage (T12.03.a)');
  lines.push('');
  lines.push(ledger.schema_coverage.method);
  lines.push('');
  lines.push('| area | openapi | generated types | matches |');
  lines.push('| --- | ---: | ---: | --- |');
  for (const measure of ledger.schema_coverage.measures) {
    lines.push(`| ${measure.area} | ${measure.openapi} | ${measure.generated} | ${measure.matches ? 'yes' : 'no'} |`);
  }
  lines.push('');
  if (ledger.schema_coverage.mismatches.length > 0) {
    lines.push('Mismatches (recorded as facts; the generator is not changed here):');
    lines.push('');
    for (const mismatch of ledger.schema_coverage.mismatches) {
      lines.push(
        `- **${mismatch.area}**: openapi ${mismatch.openapi} vs generated ${mismatch.generated} (${mismatch.direction}). ${mismatch.why}`,
      );
    }
    lines.push('');
  }

  lines.push('## DEV mock entry points (T12.03.c hand-off)');
  lines.push('');
  lines.push(`Flag: \`${ledger.mock_entry_points.dev_flag}\` - ${ledger.mock_entry_points.dev_flag_note}`);
  lines.push('');
  lines.push(`Mock store files: ${ledger.mock_entry_points.store_files.map((f) => `\`${f}\``).join(', ') || '-'}`);
  lines.push('');
  lines.push('| injection site | layer | mode | modules | DEV-gated | note |');
  lines.push('| --- | --- | --- | --- | --- | --- |');
  for (const site of ledger.mock_entry_points.injection_sites) {
    lines.push(
      `| \`${site.site}\` | ${site.layer} | ${site.mode} | ${escapeCell(site.modules.join(', ')) || '-'} | ${site.guarded ?? site.dev_flag_occurrences > 0 ? 'yes' : 'no'} | ${escapeCell(site.note)} |`,
    );
  }
  lines.push('');
  if (ledger.mock_entry_points.non_dev_fallbacks.length > 0) {
    lines.push(`Not DEV-gated: ${ledger.mock_entry_points.non_dev_fallbacks.map((f) => `\`${f}\``).join(', ')}`);
    lines.push('');
  }

  lines.push('## Notes');
  lines.push('');
  for (const note of ledger.notes) lines.push(`- ${note}`);
  lines.push('');
  return `${lines.join('\n')}\n`;
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

/**
 * Compare the generated text with what is on disk, after normalizing CRLF.
 * A Windows checkout with core.autocrlf=true materialises the committed LF
 * blob as CRLF, so only a real content difference counts as stale.
 */
export function diffAgainstDisk(generated, filePath, relPath) {
  if (!fs.existsSync(filePath)) return [`${relPath}: missing (not generated yet)`];
  const current = fs.readFileSync(filePath, 'utf8').replace(/\r\n/g, '\n');
  if (current === generated) return [];
  const currentLines = current.split('\n');
  const generatedLines = generated.split('\n');
  const differences = [];
  const max = Math.max(currentLines.length, generatedLines.length);
  for (let i = 0; i < max && differences.length < 20; i += 1) {
    if (currentLines[i] !== generatedLines[i]) {
      differences.push(`${relPath}:${i + 1}: committed ${JSON.stringify((currentLines[i] ?? '<eof>').slice(0, 160))} != generated ${JSON.stringify((generatedLines[i] ?? '<eof>').slice(0, 160))}`);
    }
  }
  if (differences.length === 0) differences.push(`${relPath}: content differs`);
  return differences;
}

export function main({
  root = repoRoot,
  paths = {},
  check = false,
  log = console.log,
  error = console.error,
} = {}) {
  const resolved = { ...DEFAULT_PATHS, ...paths };
  const { ledger, markdown } = analyze({ root, paths: resolved, check });
  const json = stableStringify(ledger);
  const outputs = [
    { file: resolved.outJson, text: json },
    { file: resolved.outMd, text: markdown },
  ];

  if (check) {
    const differences = [];
    for (const output of outputs) {
      const abs = path.join(root, output.file);
      differences.push(...diffAgainstDisk(output.text, abs, toPosix(output.file)));
    }
    if (differences.length > 0) {
      error(`Frontend endpoint map is out of date (${differences.length} difference${differences.length === 1 ? '' : 's'}):`);
      for (const line of differences) error(`  ${line}`);
      error('Run: node scripts/frontend-endpoint-map.mjs');
      return 1;
    }
    log(`Frontend endpoint map is up to date: ${toPosix(resolved.outJson)}, ${toPosix(resolved.outMd)}`);
    return 0;
  }

  for (const output of outputs) {
    const abs = path.join(root, output.file);
    fs.mkdirSync(path.dirname(abs), { recursive: true });
    fs.writeFileSync(abs, output.text, 'utf8');
  }
  const counts = ledger.counts;
  log(
    `Generated ${toPosix(resolved.outJson)} and ${toPosix(resolved.outMd)}: ${counts.total} endpoints ` +
      `(${Object.entries(counts.by_status)
        .map(([status, value]) => `${status} ${value}`)
        .join(', ')}), ${counts.non_json} non-JSON, ${counts.unknown_urls} unresolvable URL expression(s)`,
  );
  return 0;
}

function parseArgs(argv) {
  const paths = {};
  let check = false;
  let root = repoRoot;
  const map = {
    '--openapi': 'openapi',
    '--router': 'router',
    '--generated-types': 'generatedTypes',
    '--out-json': 'outJson',
    '--out-md': 'outMd',
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--check') check = true;
    else if (arg === '--root') root = path.resolve(argv[(i += 1)]);
    else if (map[arg]) paths[map[arg]] = argv[(i += 1)];
  }
  return { root, paths, check };
}

const invokedAsScript =
  process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url;
if (invokedAsScript) {
  const options = parseArgs(process.argv.slice(2));
  if (options.paths.scanDir === undefined) delete options.paths.scanDir;
  let code;
  try {
    code = main(options);
  } catch (cause) {
    // Fail closed: a missing compiler (or an unreadable input) is a hard error
    // with a readable message, never a half-written ledger and never a regex
    // guess. analyze() runs before any output file is opened.
    console.error(`frontend-endpoint-map: ${cause instanceof Error ? cause.message : String(cause)}`);
    code = 1;
  }
  process.exit(code);
}
