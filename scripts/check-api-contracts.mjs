import fs from 'fs';
import path from 'path';
import { repoRoot } from './_shared/runtime.mjs';

const openapiPath = path.resolve(repoRoot, 'backend/docs/openapi.yaml');
const content = fs.readFileSync(openapiPath, 'utf8');
const lines = content.split(/\r?\n/);

function parseOpenApiOperations() {
  const operations = new Map();
  let inPaths = false;
  let currentPath = '';

  for (const line of lines) {
    if (line === 'paths:') {
      inPaths = true;
      continue;
    }
    if (line === 'components:') break;
    if (!inPaths) continue;

    const pathMatch = line.match(/^  (\/[^:]+):\s*$/);
    if (pathMatch) {
      currentPath = pathMatch[1];
      if (!operations.has(currentPath)) operations.set(currentPath, new Set());
      continue;
    }

    const methodMatch = line.match(/^    (get|post|put|patch|delete):\s*$/);
    if (currentPath && methodMatch) {
      operations.get(currentPath).add(methodMatch[1].toUpperCase());
    }
  }

  return operations;
}

function countOperations(operations) {
  let count = 0;
  for (const methods of operations.values()) count += methods.size;
  return count;
}

const operations = parseOpenApiOperations();

const checks = [
  {
    id: 'memory-items-list-envelope',
    expected: [
      '/api/v1/memory/items:',
      "$ref: '#/components/schemas/MemoryListEnvelope'",
      'enum: [active, archived, pending_archive, pending_delete]',
      'enum: [timestamp, heat, surprise]',
    ],
  },
  {
    id: 'config-global-envelope',
    expected: [
      '/api/v1/config/global:',
      "$ref: '#/components/schemas/GlobalConfigEnvelope'",
    ],
  },
  {
    id: 'config-session-envelope',
    expected: [
      '/api/v1/config/sessions/{id}:',
      "$ref: '#/components/schemas/SessionConfigEnvelope'",
    ],
  },
  {
    id: 'config-role-envelope',
    expected: [
      '/api/v1/config/roles/{role}:',
      "$ref: '#/components/schemas/RoleConfigEnvelope'",
    ],
  },
  {
    id: 'config-resolve-envelope',
    expected: [
      '/api/v1/config/resolve:',
      "$ref: '#/components/schemas/ResolvedConfigEnvelope'",
    ],
  },
  {
    id: 'samg-triples-list-envelope',
    expected: [
      '/api/v1/samg/triples:',
      "$ref: '#/components/schemas/TripleListEnvelope'",
      "$ref: '#/components/schemas/GenericMessageCountEnvelope'",
    ],
  },
  {
    id: 'samg-triple-detail-envelope',
    expected: [
      '/api/v1/samg/triples/{id}:',
      "$ref: '#/components/schemas/TripleEnvelope'",
    ],
  },
  {
    id: 'samg-relations-envelope',
    expected: [
      '/api/v1/samg/triples/{id}/relations:',
      "$ref: '#/components/schemas/RelationsEnvelope'",
    ],
  },
  {
    id: 'samg-query-memory-envelope',
    expected: [
      '/api/v1/samg/query-memory:',
      "$ref: '#/components/schemas/QueryMemoryEnvelope'",
    ],
  },
  {
    id: 'samg-node-pointers-envelope',
    expected: [
      '/api/v1/samg/nodes/{id}/pointers:',
      "$ref: '#/components/schemas/NodePointersEnvelope'",
      "$ref: '#/components/schemas/GenericMessageEnvelope'",
    ],
  },
  {
    id: 'samg-stats-envelope',
    expected: [
      '/api/v1/samg/stats:',
      "$ref: '#/components/schemas/SAMGStatsEnvelope'",
    ],
  },
  {
    id: 'snapshot-raw-responses',
    expected: [
      '/api/v1/snapshots/{id}:',
      "$ref: '#/components/schemas/SnapshotDeleteResponse'",
      '/api/v1/snapshots/{id}/restore:',
      "$ref: '#/components/schemas/RestoreResult'",
    ],
  },
  {
    id: 'schema-definitions-present',
    expected: [
      '    GlobalConfigEnvelope:',
      '    ResolvedConfigEnvelope:',
      '    TripleEnvelope:',
      '    QueryMemoryEnvelope:',
      '    NodePointersEnvelope:',
      '    SAMGStatsEnvelope:',
      '    SnapshotDeleteResponse:',
      '    RestoreResult:',
    ],
  },
  {
    id: 'p2-007-schema-definitions-present',
    expected: [
      '    AtomicMemory:',
      '    AtomicMemoryCreateRequest:',
      '    AtomicMemoryListEnvelope:',
      '    RawArchiveEntry:',
      '    RawArchiveStatsEnvelope:',
      '    RawArchiveSearchEnvelope:',
      '    RawArchiveCreateEnvelope:',
      '    MemoryAgentIngestRequest:',
      '    MemoryAgentIngestEnvelope:',
      '    MemoryAgentRetrieveEnvelope:',
      '    MemoryAgentContextEnvelope:',
      '    Project:',
      '    ProjectListEnvelope:',
      '    ProjectPlansResponse:',
      '    PlanDocumentEnvelope:',
      '    PluginManifest:',
      '    PluginDetailEnvelope:',
      '    WorkflowOverviewEnvelope:',
      '    WorkflowTimelineEnvelope:',
      '    WorkflowReplayEnvelope:',
    ],
  },
  {
    id: 'p2-007-critical-field-alignment',
    expected: [
      '/api/v1/memory/atomic/search:',
      '- name: query',
      '/api/v1/memory/atomic/{id}/boost:',
      "$ref: '#/components/schemas/GenericMessageEnvelope'",
      '        boost:',
      '      required: [content, session_id, source]',
      '      required: [content, type]',
      '        type:',
      '          enum: [conversation, code_diff, document]',
      "      required: [content, type, session_id, source]",
      '        max_results:',
      '        max_tokens:',
      "$ref: '#/components/schemas/ProjectPlansResponse'",
      "$ref: '#/components/schemas/PluginListResponse'",
      "$ref: '#/components/schemas/PluginCatalogResponse'",
      "$ref: '#/components/schemas/PluginDetailResponse'",
    ],
  },
];

const requiredOperations = [
  ['/api/v1/memory/atomic', 'POST'],
  ['/api/v1/memory/atomic/search', 'GET'],
  ['/api/v1/memory/atomic/session/{id}', 'GET'],
  ['/api/v1/memory/atomic/{id}', 'PUT'],
  ['/api/v1/memory/atomic/{id}', 'DELETE'],
  ['/api/v1/memory/atomic/decay', 'POST'],
  ['/api/v1/memory/atomic/recompute-tiers', 'POST'],
  ['/api/v1/memory/atomic/{id}/boost', 'POST'],
  ['/api/v1/memory/atomic/tier/{tier}', 'GET'],
  ['/api/v1/memory/archive', 'POST'],
  ['/api/v1/memory/archive', 'GET'],
  ['/api/v1/memory/archive/search', 'GET'],
  ['/api/v1/memory/archive/stats', 'GET'],
  ['/api/v1/memory/archive/{id}', 'GET'],
  ['/api/v1/memory/agent/ingest', 'POST'],
  ['/api/v1/memory/agent/retrieve', 'POST'],
  ['/api/v1/memory/agent/context', 'POST'],
  ['/api/v1/projects', 'GET'],
  ['/api/v1/projects', 'POST'],
  ['/api/v1/projects/{id}', 'GET'],
  ['/api/v1/projects/{id}', 'PUT'],
  ['/api/v1/projects/{id}', 'DELETE'],
  ['/api/v1/projects/{id}/plans', 'GET'],
  ['/api/v1/projects/{id}/plans', 'POST'],
  ['/api/v1/projects/{id}/plans/{planId}', 'DELETE'],
  ['/api/v1/projects/{id}/plan', 'POST'],
  ['/api/v1/projects/{id}/plan', 'GET'],
  ['/api/v1/projects/{id}/plan/revise', 'POST'],
  ['/api/v1/projects/{id}/plan/approve', 'POST'],
  ['/api/v1/projects/{id}/plan/execute', 'POST'],
  ['/api/v1/plugins', 'GET'],
  ['/api/v1/plugins/marketplace', 'GET'],
  ['/api/v1/plugins/{id}', 'GET'],
  ['/api/v1/plugins/{id}', 'PATCH'],
  ['/api/v1/plugins/{id}/install', 'POST'],
  ['/api/v1/workflows/{projectId}/overview', 'GET'],
  ['/api/v1/workflows/{projectId}/timeline', 'GET'],
  ['/api/v1/workflows/{projectId}/replay', 'GET'],
];

const failures = [];
for (const check of checks) {
  const missing = check.expected.filter((needle) => !content.includes(needle));
  if (missing.length > 0) {
    failures.push({ id: check.id, missing });
  }
}

for (const [pathName, method] of requiredOperations) {
  const methods = operations.get(pathName);
  if (!methods?.has(method)) {
    failures.push({ id: 'p2-007-route-coverage', missing: [`${method} ${pathName}`] });
  }
}

const minPathCount = 132;
const minOperationCount = 166;
if (operations.size < minPathCount) {
  failures.push({ id: 'openapi-path-count', missing: [`at least ${minPathCount} paths, got ${operations.size}`] });
}
const operationCount = countOperations(operations);
if (operationCount < minOperationCount) {
  failures.push({ id: 'openapi-operation-count', missing: [`at least ${minOperationCount} operations, got ${operationCount}`] });
}

if (failures.length > 0) {
  console.error(`API contract check failed for ${failures.length} rule(s):`);
  for (const failure of failures) {
    console.error(`- ${failure.id}`);
    for (const needle of failure.missing) {
      console.error(`  missing: ${needle}`);
    }
  }
  process.exit(1);
}

console.log(`API contract check passed for ${checks.length} string rules and ${requiredOperations.length} route operations.`);
console.log(`OpenAPI paths: ${operations.size}; operations: ${operationCount}.`);
console.log(`Checked file: ${openapiPath}`);
