#!/usr/bin/env node
// T0.03.a — validates backend/schemas/*.schema.json against fixtures/valid and fixtures/invalid.
// Usage (cwd = repo root): node backend/schemas/validate-fixtures.mjs
// Exit 0 only if every valid fixture validates and every invalid fixture is rejected
// with one of its expected ajv keywords. Requires ajv 6.x from the repo's node_modules;
// adds no new dependencies.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import fs from 'node:fs';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..', '..');
const repoRequire = createRequire(path.join(repoRoot, 'package.json'));

function loadAjv() {
  try {
    const mod = repoRequire('ajv');
    let version = 'unknown';
    try {
      version = repoRequire('ajv/package.json').version;
    } catch {
      // version reporting only
    }
    return { Ajv: mod, from: 'node_modules (hoisted)', version };
  } catch {
    // pnpm layout: ajv is not hoisted to the root node_modules
  }
  const pnpmDir = path.join(repoRoot, 'node_modules', '.pnpm');
  const candidates = fs
    .readdirSync(pnpmDir, { withFileTypes: true })
    .filter((d) => d.isDirectory() && d.name.startsWith('ajv@6.'))
    .map((d) => path.join(pnpmDir, d.name, 'node_modules', 'ajv'))
    .filter((p) => fs.existsSync(p))
    .sort();
  if (candidates.length === 0) {
    throw new Error('ajv 6.x not found under node_modules; cannot validate fixtures');
  }
  const mod = repoRequire(candidates[0]);
  let version = 'unknown';
  try {
    version = repoRequire(path.join(candidates[0], 'package.json')).version;
  } catch {
    // version reporting only
  }
  return { Ajv: mod, from: candidates[0], version };
}

const { Ajv, from: ajvFrom, version: ajvVersion } = loadAjv();
console.log(`ajv: ${ajvVersion} (${ajvFrom})`);

const ajv = new Ajv({ allErrors: true });

const SCHEMAS = {
  identity: 'execution-identity.schema.json',
  event: 'execution-event.schema.json',
  error: 'error.schema.json',
  response: 'response.schema.json',
  custom: 'custom-backend.schema.json',
};

function readJson(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

for (const file of Object.values(SCHEMAS)) {
  ajv.addSchema(readJson(path.join(here, file)));
}
const validators = {};
for (const [key, file] of Object.entries(SCHEMAS)) {
  const validate = ajv.getSchema(file);
  if (!validate) throw new Error(`schema not registered: ${file}`);
  validators[key] = validate;
}

// Each invalid fixture must be rejected with at least one of these ajv keywords.
const EXPECT = {
  'identity.missing-project-id.json': ['required'],
  'identity.unknown-actor-type.json': ['enum'],
  'identity.missing-actor.json': ['required'],
  'identity.project-id-too-long.json': ['maxLength'],
  'event.missing-payload.json': ['required'],
  'event.unknown-type.json': ['enum'],
  'event.sequence-zero.json': ['minimum'],
  'event.schema-version-2.json': ['const'],
  'event.bad-occurred-at.json': ['format'],
  'event.sequence-string.json': ['type'],
  'error.unknown-code.json': ['enum'],
  'error.missing-retryable.json': ['required'],
  'error.retryable-wrong-type.json': ['type'],
  'error.message-too-long.json': ['maxLength'],
  'response.both-data-and-error.json': ['oneOf'],
  'response.legacy-shape.json': ['oneOf'],
  'response.missing-meta.json': ['oneOf', 'required'],
  'response.error-unknown-code.json': ['oneOf', 'enum'],
  // T4.03.a — 自定义执行器协议 v1。后端→服务端帧的序号/身份/审计字段必须被拒绝，
  // 未知必需 capability / 未知工具效果 / schema_version≠1 / 未知 type 同样拒绝。
  // 帧 schema 是 oneOf，ajv 会在每个分支上报错，因此期望关键字包含 oneOf 与内层关键字。
  'custom.observation-with-project-seq.json': ['oneOf', 'additionalProperties'],
  'custom.exit-with-actor.json': ['oneOf', 'additionalProperties'],
  'custom.hello-schema-version-2.json': ['oneOf', 'const'],
  'custom.unknown-type.json': ['oneOf', 'const', 'enum'],
  'custom.start-missing-identity.json': ['oneOf', 'required'],
  'custom.observation-bad-kind.json': ['oneOf', 'enum'],
  'custom.hello-bad-effect.json': ['oneOf', 'enum'],
  'custom.hello-unknown-required-capability.json': ['oneOf', 'enum'],
  'custom.hello-duplicate-capability.json': ['oneOf', 'uniqueItems'],
  'custom.observation-missing-observed-at.json': ['oneOf', 'required'],
  'custom.exit-bad-reason.json': ['oneOf', 'enum'],
  'custom.cancel-bad-mode.json': ['oneOf', 'enum'],
};

function listFixtures(kind) {
  const dir = path.join(here, 'fixtures', kind);
  return fs
    .readdirSync(dir)
    .filter((name) => name.endsWith('.json'))
    .sort()
    .map((name) => ({ name, file: path.join(dir, name) }));
}

function formatError(err) {
  const bits = [err.keyword, `at=${err.dataPath || '/'}`];
  if (err.params.missingProperty) bits.push(`missing=${err.params.missingProperty}`);
  if (err.params.allowedValue !== undefined) bits.push(`allowed=${JSON.stringify(err.params.allowedValue)}`);
  if (err.params.allowedValues) bits.push(`allowed=[${err.params.allowedValues.length} values]`);
  if (err.params.limit !== undefined) bits.push(`limit=${err.params.limit}`);
  if (err.params.type) bits.push(`type=${err.params.type}`);
  if (err.params.format) bits.push(`format=${err.params.format}`);
  return bits.join(' ');
}

let failures = 0;
let validCount = 0;
let invalidCount = 0;

for (const { name, file } of listFixtures('valid')) {
  validCount += 1;
  const key = name.split('.')[0];
  const validate = validators[key];
  if (!validate) {
    failures += 1;
    console.log(`FAIL valid/${name}: unknown schema prefix "${key}"`);
    continue;
  }
  const ok = validate(readJson(file));
  if (ok) {
    console.log(`PASS valid/${name}`);
  } else {
    failures += 1;
    console.log(`FAIL valid/${name}: expected VALID, got: ${validate.errors.map(formatError).join(' | ')}`);
  }
}

for (const { name, file } of listFixtures('invalid')) {
  invalidCount += 1;
  const key = name.split('.')[0];
  const validate = validators[key];
  if (!validate) {
    failures += 1;
    console.log(`FAIL invalid/${name}: unknown schema prefix "${key}"`);
    continue;
  }
  const ok = validate(readJson(file));
  if (ok) {
    failures += 1;
    console.log(`FAIL invalid/${name}: expected REJECTION, but it validated`);
    continue;
  }
  const reasons = validate.errors.map(formatError);
  const keywords = new Set(validate.errors.map((err) => err.keyword));
  const expected = EXPECT[name];
  const matched = (expected || []).filter((keyword) => keywords.has(keyword));
  if (!expected || matched.length === 0) {
    failures += 1;
    console.log(
      `FAIL invalid/${name}: rejected, but reason not as expected; wanted one of [${(expected || []).join(', ')}], got [${[...keywords].join(', ')}] :: ${reasons.join(' | ')}`
    );
  } else {
    console.log(`PASS invalid/${name}: rejected as expected (${matched.join(', ')}) :: ${reasons.join(' | ')}`);
  }
}

console.log(`\nsummary: valid=${validCount} invalid=${invalidCount} unexpected_failures=${failures}`);
if (failures > 0) {
  console.log('RESULT: FAIL');
  process.exit(1);
}
console.log('RESULT: OK');
