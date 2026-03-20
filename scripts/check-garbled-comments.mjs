import fs from 'fs';
import path from 'path';
import { getBooleanEnv, getEnv, repoRoot } from './_shared/runtime.mjs';

const includeDist = getBooleanEnv('CODEFLOW_SCAN_DIST', false);
const roots = getEnv(
  'CODEFLOW_SCAN_ROOTS',
  [
    path.resolve(repoRoot, 'packages'),
    path.resolve(repoRoot, 'scripts'),
    path.resolve(repoRoot, 'backend'),
    path.resolve(repoRoot, 'docs'),
  ].join(path.delimiter)
)
  .split(path.delimiter)
  .map((s) => s.trim())
  .filter(Boolean);

const textExt = new Set([
  '.ts',
  '.tsx',
  '.js',
  '.mjs',
  '.cjs',
  '.go',
  '.md',
  '.json',
  '.yml',
  '.yaml',
  '.ps1',
  '.sh',
]);

const skipDirs = new Set(['node_modules', '.git', 'artifacts', 'coverage']);

const extraMarkers = getEnv('CODEFLOW_MOJIBAKE_MARKERS', '')
  .split(',')
  .map((s) => s.trim())
  .filter(Boolean);

function isCommentLine(line) {
  return /^\s*(\/\/|\/\*|\*|\*\/)/.test(line);
}

function hasGarbledText(line) {
  if (line.includes('\uFFFD')) return true;
  if (/[\uE000-\uF8FF]/u.test(line)) return true;
  return extraMarkers.some((m) => line.includes(m));
}

function scanFile(filePath, hits) {
  let content = '';
  try {
    content = fs.readFileSync(filePath, 'utf8');
  } catch {
    return;
  }

  const lines = content.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (!isCommentLine(line)) continue;
    if (!hasGarbledText(line)) continue;
    hits.push({
      file: filePath,
      line: i + 1,
      text: line.trim(),
    });
  }
}

function walk(dir, hits) {
  let entries = [];
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return;
  }

  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (skipDirs.has(entry.name)) continue;
      if (!includeDist && entry.name === 'dist') continue;
      walk(full, hits);
      continue;
    }

    const ext = path.extname(entry.name).toLowerCase();
    if (!textExt.has(ext)) continue;
    scanFile(full, hits);
  }
}

const hits = [];
for (const root of roots) {
  if (fs.existsSync(root)) {
    walk(root, hits);
  }
}

if (hits.length === 0) {
  console.log('No garbled comments detected.');
  process.exit(0);
}

for (const hit of hits) {
  console.log(`${hit.file}:${hit.line}: ${hit.text}`);
}
console.log(`Detected ${hits.length} garbled comment line(s).`);
process.exit(1);
