#!/usr/bin/env node
/**
 * M0.3 embed path smoke (Node).
 * Syncs apps/workbench/dist -> backend/internal/web/dist and validates SPA assets.
 * Does not require Go. Use scripts/smoke-embed.ps1 for optional go build.
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(__dirname, '..');
const buildFrontend = process.argv.includes('--build');
const frontendDir = path.join(root, 'apps', 'workbench');
const frontendDist = path.join(frontendDir, 'dist');
const embedDist = path.join(root, 'backend', 'internal', 'web', 'dist');
const staticGo = path.join(root, 'backend', 'internal', 'web', 'static.go');

function fail(msg) {
  console.error(`[embed-smoke] FAIL: ${msg}`);
  process.exit(1);
}

function ok(msg) {
  console.log(`[embed-smoke] ok: ${msg}`);
}

function rmrf(p) {
  fs.rmSync(p, { recursive: true, force: true });
}

function copyDir(src, dst) {
  if (!fs.existsSync(src)) fail(`source missing: ${src}`);
  rmrf(dst);
  fs.mkdirSync(dst, { recursive: true });
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const from = path.join(src, entry.name);
    const to = path.join(dst, entry.name);
    if (entry.isDirectory()) copyDir(from, to);
    else fs.copyFileSync(from, to);
  }
}

console.log('=== CodeFlow embed smoke (M0.3, node) ===');
console.log('Root:', root);

if (!fs.existsSync(frontendDir)) fail(`frontend missing: ${frontendDir}`);
if (!fs.existsSync(staticGo)) fail(`static.go missing: ${staticGo}`);
const staticText = fs.readFileSync(staticGo, 'utf8');
if (!/\/\/go:embed\s+all:dist/.test(staticText)) fail('static.go missing //go:embed all:dist');
ok('static.go embed directive');

if (buildFrontend) {
  console.log('[1] pnpm --filter @codeflow/workbench build');
  const r = spawnSync(
    process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm',
    ['--filter', '@codeflow/workbench', 'build'],
    { cwd: root, stdio: 'inherit', shell: process.platform === 'win32' }
  );
  if (r.status !== 0) fail('frontend build failed');
} else {
  console.log('[1] using existing frontend dist (pass --build to rebuild)');
}

if (!fs.existsSync(path.join(frontendDist, 'index.html'))) {
  fail(`frontend dist/index.html missing: ${frontendDist}`);
}

console.log(`[2] sync ${frontendDist} -> ${embedDist}`);
copyDir(frontendDist, embedDist);

const indexPath = path.join(embedDist, 'index.html');
if (!fs.existsSync(indexPath)) fail('embed dist missing index.html');
const html = fs.readFileSync(indexPath, 'utf8');
if (!/<script/i.test(html)) fail('index.html has no script tags');

const assetsDir = path.join(embedDist, 'assets');
if (!fs.existsSync(assetsDir)) fail('embed dist missing assets/');
const assets = fs.readdirSync(assetsDir).filter((n) => fs.statSync(path.join(assetsDir, n)).isFile());
if (assets.length < 1) fail('embed dist assets/ is empty');

const refs = [...html.matchAll(/\/assets\/[^"'?\s>]+/g)].map((m) => m[0]);
for (const ref of refs) {
  const rel = ref.replace(/^\//, '');
  const full = path.join(embedDist, ...rel.split('/'));
  if (!fs.existsSync(full)) fail(`index.html references missing file: ${ref}`);
}
ok(`dist synced (index.html + ${assets.length} assets)`);

console.log('');
console.log('=== M0.3 embed smoke PASSED ===');
console.log('Embed path:', embedDist);