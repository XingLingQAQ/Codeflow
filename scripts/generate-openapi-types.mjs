#!/usr/bin/env node
// Generates apps/workbench/generated/openapi-types.ts from backend/docs/openapi.yaml
// using a real YAML parser (yaml 2.x from the repo's node_modules; adds no new
// dependencies — falls back to the pnpm store path when yaml is not hoisted).
// Schema shapes become precise TS types: required -> non-optional property,
// enum -> string-literal union, nullable -> `| null`, $ref/oneOf/allOf ->
// reference/union/intersection. `Record<string, unknown>` is emitted only for
// objects that declare no properties (bare `type: object` or
// `additionalProperties: true`); anything else would be a generator gap.
// Usage: node scripts/generate-openapi-types.mjs [--check]
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import { repoRoot } from './_shared/runtime.mjs';

const repoRequire = createRequire(path.join(repoRoot, 'package.json'));

export function loadYaml() {
  try {
    return repoRequire('yaml');
  } catch {
    // pnpm layout: yaml is not hoisted to the root node_modules
  }
  const pnpmDir = path.join(repoRoot, 'node_modules', '.pnpm');
  const candidates = fs
    .readdirSync(pnpmDir, { withFileTypes: true })
    .filter((d) => d.isDirectory() && d.name.startsWith('yaml@'))
    .map((d) => ({ name: d.name, dir: path.join(pnpmDir, d.name, 'node_modules', 'yaml') }))
    .filter((c) => fs.existsSync(c.dir))
    .sort((a, b) => b.name.localeCompare(a.name, undefined, { numeric: true }));
  if (candidates.length === 0) {
    throw new Error('yaml package not found under node_modules; cannot parse openapi.yaml');
  }
  return repoRequire(candidates[0].dir);
}

const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete'];

function collectOperations(doc) {
  const operations = [];
  for (const [apiPath, item] of Object.entries(doc.paths ?? {})) {
    if (!item || typeof item !== 'object') continue;
    for (const [key, value] of Object.entries(item)) {
      if (HTTP_METHODS.includes(key) && value && typeof value === 'object') {
        operations.push({ path: apiPath, method: key.toUpperCase() });
      }
    }
  }
  return operations;
}

function toOperationKey(operation) {
  const suffix = operation.path
    .replace(/^\//, '')
    .replace(/\{([^}]+)\}/g, 'by_$1')
    .replace(/[^A-Za-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .toLowerCase();
  return `${operation.method.toLowerCase()}_${suffix}`;
}

function tsStringLiteral(value) {
  return `'${String(value).replace(/\\/g, '\\\\').replace(/'/g, "\\'")}'`;
}

function tsPropertyName(name) {
  return /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(name) ? name : tsStringLiteral(name);
}

class SchemaCompiler {
  constructor(schemas) {
    this.schemas = schemas ?? {};
    this.warnings = [];
    this.recordSites = 0;
    this.warnedRefs = new Set();
  }

  resolveRef(ref) {
    const match = typeof ref === 'string' ? ref.match(/^#\/components\/schemas\/(.+)$/) : null;
    if (match && Object.prototype.hasOwnProperty.call(this.schemas, match[1])) {
      return match[1];
    }
    if (!this.warnedRefs.has(ref)) {
      this.warnedRefs.add(ref);
      this.warnings.push(`unresolvable $ref: ${ref} (emitted as unknown)`);
    }
    return 'unknown';
  }

  // Compiles a schema node to a TS type expression. Counts every
  // `Record<string, unknown>` emission so tests can prove it stays the
  // documented exception (objects without properties), never the norm.
  compile(schema) {
    if (!schema || typeof schema !== 'object') return 'unknown';
    if (schema.$ref) return this.resolveRef(schema.$ref);
    if (Array.isArray(schema.oneOf) && schema.oneOf.length > 0) {
      return this.wrapNullable(schema.oneOf.map((sub) => this.compile(sub)).join(' | '), schema);
    }
    if (Array.isArray(schema.allOf) && schema.allOf.length > 0) {
      const parts = schema.allOf.map((sub) => this.compile(sub));
      if (schema.properties) parts.push(this.compileObjectProperties(schema));
      return this.wrapNullable(parts.join(' & '), schema);
    }
    if (Array.isArray(schema.enum) && schema.enum.length > 0) {
      const union = schema.enum
        .map((value) => (typeof value === 'string' ? tsStringLiteral(value) : String(value)))
        .join(' | ');
      return this.wrapNullable(union, schema);
    }
    switch (schema.type) {
      case 'string':
        return this.wrapNullable('string', schema);
      case 'integer':
      case 'number':
        return this.wrapNullable('number', schema);
      case 'boolean':
        return this.wrapNullable('boolean', schema);
      case 'array': {
        const itemType = this.compile(schema.items);
        const arrayType = /^[A-Za-z_$][A-Za-z0-9_$]*(\[\])?$|^(string|number|boolean|unknown)$/.test(itemType)
          ? `${itemType}[]`
          : `Array<${itemType}>`;
        return this.wrapNullable(arrayType, schema);
      }
      case 'object':
        return this.wrapNullable(this.compileObject(schema), schema);
      default:
        if (schema.properties) return this.wrapNullable(this.compileObjectProperties(schema), schema);
        return 'unknown';
    }
  }

  wrapNullable(type, schema) {
    return schema.nullable === true ? `${type} | null` : type;
  }

  compileObject(schema) {
    if (schema.properties) return this.compileObjectProperties(schema);
    // Documented exception: an object that declares no properties is an
    // open map, the only place `Record<string, unknown>` is legitimate.
    this.recordSites += 1;
    return 'Record<string, unknown>';
  }

  compileObjectProperties(schema) {
    const required = new Set(Array.isArray(schema.required) ? schema.required : []);
    const entries = Object.entries(schema.properties ?? {}).map(([name, sub]) => {
      const optional = required.has(name) ? '' : '?';
      return `${tsPropertyName(name)}${optional}: ${this.compile(sub)};`;
    });
    return `{ ${entries.join(' ')} }`;
  }

  compileTopLevel(name, schema) {
    if (schema && typeof schema === 'object' && schema.type === 'object' && schema.properties && !schema.allOf && !schema.oneOf) {
      const required = new Set(Array.isArray(schema.required) ? schema.required : []);
      const lines = Object.entries(schema.properties).map(([prop, sub]) => {
        const optional = required.has(prop) ? '' : '?';
        return `  ${tsPropertyName(prop)}${optional}: ${this.compile(sub)};`;
      });
      return `export interface ${name} {\n${lines.join('\n')}\n}`;
    }
    return `export type ${name} = ${this.compile(schema)};`;
  }
}

export function generateTypes(source) {
  const YAML = loadYaml();
  const doc = YAML.parse(source);
  const operations = collectOperations(doc);
  const schemas = doc.components?.schemas ?? {};
  const schemaNames = Object.keys(schemas);
  if (operations.length === 0) {
    throw new Error('No OpenAPI paths found');
  }
  if (schemaNames.length === 0) {
    throw new Error('No OpenAPI schemas found');
  }
  for (const name of schemaNames) {
    if (!/^[A-Za-z_$][A-Za-z0-9_$]*$/.test(name)) {
      throw new Error(`Schema name is not a valid TS identifier: ${name}`);
    }
  }

  const compiler = new SchemaCompiler(schemas);
  const declarations = schemaNames.map((name) => compiler.compileTopLevel(name, schemas[name]));

  const operationEntries = operations
    .map((operation) => `  '${toOperationKey(operation)}': { method: '${operation.method}'; path: '${operation.path}' };`)
    .join('\n');
  const schemaEntries = schemaNames.map((name) => `  ${name}: ${name};`).join('\n');
  const methods = Array.from(new Set(operations.map((operation) => operation.method))).sort();

  const text =
    `// Auto-generated by scripts/generate-openapi-types.mjs. Do not edit manually.\n` +
    `// Source: backend/docs/openapi.yaml\n\n` +
    `export type OpenApiMethod = ${methods.map((method) => `'${method}'`).join(' | ')};\n\n` +
    `export interface OpenApiOperationMap {\n${operationEntries}\n}\n\n` +
    `${declarations.join('\n\n')}\n\n` +
    `export interface OpenApiSchemas {\n${schemaEntries}\n}\n\n` +
    `export type OpenApiPath = OpenApiOperationMap[keyof OpenApiOperationMap]['path'];\n` +
    `export type OpenApiSchemaName = keyof OpenApiSchemas;\n`;

  return {
    text,
    warnings: compiler.warnings,
    stats: {
      operations: operations.length,
      schemas: schemaNames.length,
      recordSites: compiler.recordSites,
    },
  };
}

export function main({ openapiPath, outputPath, check = false, log = console.log, error = console.error } = {}) {
  const resolvedOpenapi = openapiPath ?? path.resolve(repoRoot, 'backend/docs/openapi.yaml');
  const resolvedOutput = outputPath ?? path.resolve(repoRoot, 'apps/workbench/generated/openapi-types.ts');
  const { text: generated, warnings } = generateTypes(fs.readFileSync(resolvedOpenapi, 'utf8'));
  for (const warning of warnings) {
    error(`warning: ${warning}`);
  }

  if (check) {
    // The generator always emits LF. A Windows checkout with core.autocrlf=true
    // materialises the committed LF blob as CRLF, so compare line-ending-agnostic
    // content: only a real content difference counts as stale.
    const current = fs.existsSync(resolvedOutput)
      ? fs.readFileSync(resolvedOutput, 'utf8').replace(/\r\n/g, '\n')
      : '';
    if (current !== generated) {
      error(`Generated OpenAPI types are out of date: ${path.relative(repoRoot, resolvedOutput)}`);
      error('Run: pnpm generate:api-types');
      return 1;
    }
    log(`OpenAPI types are up to date: ${path.relative(repoRoot, resolvedOutput)}`);
    return 0;
  }
  fs.mkdirSync(path.dirname(resolvedOutput), { recursive: true });
  fs.writeFileSync(resolvedOutput, generated, 'utf8');
  log(`Generated ${path.relative(repoRoot, resolvedOutput)} from ${path.relative(repoRoot, resolvedOpenapi)}`);
  return 0;
}

const invokedAsScript = process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url;
if (invokedAsScript) {
  process.exit(main({ check: process.argv.includes('--check') }));
}
