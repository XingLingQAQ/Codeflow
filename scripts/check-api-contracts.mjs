import fs from 'fs';
import path from 'path';
import { repoRoot } from './_shared/runtime.mjs';

const openapiPath = path.resolve(repoRoot, 'backend/docs/openapi.yaml');
const content = fs.readFileSync(openapiPath, 'utf8');

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
];

const failures = [];
for (const check of checks) {
  const missing = check.expected.filter((needle) => !content.includes(needle));
  if (missing.length > 0) {
    failures.push({ id: check.id, missing });
  }
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

console.log(`API contract check passed for ${checks.length} rules.`);
console.log(`Checked file: ${openapiPath}`);
