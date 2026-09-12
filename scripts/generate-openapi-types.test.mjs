#!/usr/bin/env node
// Tests for scripts/generate-openapi-types.mjs (node:test, no new dependencies).
// Covers: precise type emission (required/enum/nullable/$ref), the documented
// `Record<string, unknown>` exception, tsc-level negative proof that a missing
// required property and an out-of-enum value are compile errors against the
// generated types, and --check mode exit codes (stale vs up to date).
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { generateTypes, loadYaml, main } from './generate-openapi-types.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..');

const MINI_SPEC = `
openapi: 3.0.3
info:
  title: mini
  version: '1.0'
paths:
  /widgets:
    get:
      responses:
        '200':
          description: ok
    post:
      responses:
        '200':
          description: ok
components:
  schemas:
    Widget:
      type: object
      required: [id, kind]
      properties:
        id:
          type: string
        kind:
          type: string
          enum: [alpha, beta]
        note:
          type: string
          nullable: true
        gadget:
          $ref: '#/components/schemas/Gadget'
        meta:
          type: object
          additionalProperties: true
        opaque:
          type: object
    Gadget:
      type: object
      required: [size]
      properties:
        size:
          type: integer
        widget:
          $ref: '#/components/schemas/Widget'
`;

function makeTempDir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'openapi-types-test-'));
}

test('generates precise field types from required/enum/nullable/$ref', () => {
  const { text } = generateTypes(MINI_SPEC);

  assert.match(text, /export interface Widget \{/);
  // required -> non-optional, non-required -> optional
  assert.match(text, /\n {2}id: string;/);
  assert.match(text, /\n {2}kind: 'alpha' \| 'beta';/);
  assert.match(text, /\n {2}note\?: string \| null;/);
  assert.match(text, /\n {2}gadget\?: Gadget;/);
  assert.match(text, /export interface Gadget \{/);
  assert.match(text, /\n {2}size: number;/);
  assert.match(text, /\n {2}widget\?: Widget;/);
  // operations still map method/path
  assert.match(text, /'get_widgets': \{ method: 'GET'; path: '\/widgets' \};/);
  assert.match(text, /'post_widgets': \{ method: 'POST'; path: '\/widgets' \};/);
});

test('required and enum are not silently widened (text-level negatives)', () => {
  const { text } = generateTypes(MINI_SPEC);
  const widgetBlock = text.match(/export interface Widget \{[\s\S]*?\n\}/)[0];
  // a required field must NOT become optional
  assert.ok(!widgetBlock.includes('id?:'), 'required field leaked into optional');
  assert.ok(!widgetBlock.includes('kind?:'), 'required enum field leaked into optional');
  // an enum must NOT widen back to plain string
  assert.ok(!/\bkind: string;/.test(widgetBlock), 'enum widened to string');
  assert.ok(!/\bkind\?: string;/.test(widgetBlock), 'enum widened to optional string');
});

test('Record<string, unknown> stays the documented exception, not the norm', () => {
  const { text, stats } = generateTypes(MINI_SPEC);
  const records = text.match(/Record<string, unknown>/g) ?? [];
  // exactly the two property-less objects (meta, opaque), nowhere else
  assert.equal(records.length, 2);
  assert.equal(stats.recordSites, 2);
  assert.match(text, /meta\?: Record<string, unknown>;/);
  assert.match(text, /opaque\?: Record<string, unknown>;/);
  // typed object schemas must compile to interfaces, not Record placeholders
  assert.ok(!/export type Widget = Record<string, unknown>;/.test(text));
  assert.ok(!/export type Gadget = Record<string, unknown>;/.test(text));
});

test('unresolvable $ref degrades to unknown with a warning', () => {
  const spec = MINI_SPEC.replace("$ref: '#/components/schemas/Gadget'", "$ref: '#/components/schemas/Missing'");
  const { text, warnings } = generateTypes(spec);
  assert.match(text, /gadget\?: unknown;/);
  assert.equal(warnings.length, 1);
  assert.match(warnings[0], /unresolvable \$ref: #\/components\/schemas\/Missing/);
});

test('tsc rejects missing required property and out-of-enum value', () => {
  const dir = makeTempDir();
  const { text } = generateTypes(MINI_SPEC);
  fs.writeFileSync(path.join(dir, 'widget-types.ts'), text);
  const fixture = [
    "import { Widget } from './widget-types';",
    '',
    "const good: Widget = { id: 'w1', kind: 'alpha' };",
    'void good;',
    '',
    '// @ts-expect-error required property id is missing',
    "const missingRequired: Widget = { kind: 'alpha' };",
    'void missingRequired;',
    '',
    "// @ts-expect-error 'gamma' is not in the kind enum",
    "const badEnum: Widget = { id: 'w2', kind: 'gamma' };",
    'void badEnum;',
    '',
  ].join('\n');
  const fixturePath = path.join(dir, 'widget-types.fixture.ts');
  fs.writeFileSync(fixturePath, fixture);

  const tscBin = path.join(repoRoot, 'node_modules', 'typescript', 'bin', 'tsc');
  const result = spawnSync(
    process.execPath,
    [tscBin, '--noEmit', '--strict', '--skipLibCheck', fixturePath],
    { encoding: 'utf8' }
  );
  // Exit 0 proves both @ts-expect-error directives suppressed real errors:
  // a missing required property and an out-of-enum value are compile errors.
  // If the generator widened the types, the directives would be "unused" and
  // tsc would exit non-zero with TS2578.
  assert.equal(result.status, 0, `tsc output:\n${result.stdout}\n${result.stderr}`);
});

test('--check exits 1 when stale, 0 after regeneration', () => {
  const dir = makeTempDir();
  const openapiPath = path.join(dir, 'openapi.yaml');
  const outputPath = path.join(dir, 'openapi-types.ts');
  fs.writeFileSync(openapiPath, MINI_SPEC);
  const quiet = () => {};

  // no file yet -> stale
  assert.equal(main({ openapiPath, outputPath, check: true, log: quiet, error: quiet }), 1);
  // generate, then check -> up to date
  assert.equal(main({ openapiPath, outputPath, log: quiet, error: quiet }), 0);
  assert.equal(main({ openapiPath, outputPath, check: true, log: quiet, error: quiet }), 0);
  // drift -> stale again
  fs.appendFileSync(outputPath, '// tampered\n');
  assert.equal(main({ openapiPath, outputPath, check: true, log: quiet, error: quiet }), 1);
  // regenerate -> up to date
  assert.equal(main({ openapiPath, outputPath, log: quiet, error: quiet }), 0);
  assert.equal(main({ openapiPath, outputPath, check: true, log: quiet, error: quiet }), 0);
});

test('real openapi.yaml: every Record is a property-less object site', () => {
  const source = fs.readFileSync(path.join(repoRoot, 'backend', 'docs', 'openapi.yaml'), 'utf8');
  const { text, stats, warnings } = generateTypes(source);

  // Independent recount over the parsed spec: nodes that are objects without
  // properties/$ref/oneOf/allOf are the only legitimate Record sites.
  const doc = loadYaml().parse(source);
  let expectedSites = 0;
  (function walk(node) {
    if (!node || typeof node !== 'object') return;
    if (node.type === 'object' && !node.properties && !node.allOf && !node.oneOf && !node.$ref) {
      expectedSites += 1;
    }
    for (const value of Object.values(node)) {
      if (value && typeof value === 'object') walk(value);
    }
  })(doc.components.schemas);

  const records = text.match(/Record<string, unknown>/g) ?? [];
  assert.equal(stats.recordSites, expectedSites);
  assert.equal(records.length, expectedSites);
  // the exception exists but stays small relative to the schema count
  assert.ok(expectedSites > 0, 'expected at least one property-less object site');
  assert.ok(records.length < stats.schemas / 4, 'Record emission looks like the norm again');
  assert.ok(stats.schemas > 200, `unexpectedly few schemas: ${stats.schemas}`);

  // the contract is free of dangling refs (Plugin*Response refs were repaired in T0.03.c);
  // synthetic dangling-ref coverage lives in 'unresolvable $ref degrades to unknown with a warning'
  assert.deepEqual(warnings, [], `unexpected warnings for the current contract: ${warnings.join('; ')}`);
});
