import { spawnSync } from 'child_process';
import fs from 'fs';
import path from 'path';
import { repoRoot } from './_shared/runtime.mjs';

function candidatesForGit() {
  const envGit = process.env.GIT_EXECUTABLE || process.env.GIT_BIN || process.env.GIT;
  const list = [];
  if (envGit) list.push(envGit);
  list.push('git');

  if (process.platform === 'win32') {
    const home = process.env.USERPROFILE || process.env.HOME || '';
    const localAppData = process.env.LOCALAPPDATA || '';
    const programFiles = process.env.ProgramFiles || 'C:\\Program Files';
    const programFilesX86 = process.env['ProgramFiles(x86)'] || 'C:\\Program Files (x86)';
    list.push(
      path.join(home, 'scoop', 'apps', 'git', 'current', 'bin', 'git.exe'),
      path.join(localAppData, 'Programs', 'Git', 'cmd', 'git.exe'),
      path.join(programFiles, 'Git', 'cmd', 'git.exe'),
      path.join(programFilesX86, 'Git', 'cmd', 'git.exe'),
    );
  }

  return list;
}

function resolveGit() {
  for (const candidate of candidatesForGit()) {
    if (!candidate) continue;
    if (candidate !== 'git' && !fs.existsSync(candidate)) continue;
    const probe = spawnSync(candidate, ['--version'], {
      encoding: 'utf8',
      windowsHide: true,
    });
    if (probe.status === 0) return candidate;
  }
  return null;
}

function listTrackedFiles() {
  const gitBin = resolveGit();
  if (!gitBin) {
    throw new Error(
      'git executable not found. Install Git, add it to PATH, or set GIT_EXECUTABLE to the git binary.',
    );
  }

  const result = spawnSync(gitBin, ['ls-files', '-z'], {
    cwd: repoRoot,
    encoding: 'buffer',
    maxBuffer: 64 * 1024 * 1024,
    windowsHide: true,
  });
  if (result.status !== 0) {
    const stderr = result.stderr?.toString('utf8') || 'git ls-files failed';
    throw new Error(`${stderr.trim()} (git=${gitBin})`);
  }
  const stdout = result.stdout.toString('utf8');
  return stdout
    .split('\0')
    .map((s) => s.trim())
    .filter(Boolean);
}

function toPosix(p) {
  return p.split(path.sep).join('/');
}

const rules = [
  {
    id: 'node_modules',
    message:
      'Tracked node_modules paths are forbidden. Untrack with: git rm -r --cached <path>',
    test: (file) => /(^|\/)node_modules(\/|$)/.test(file),
  },
  {
    id: 'apps-dist',
    message:
      'Tracked apps/**/dist outputs are forbidden (embed/sync should copy, not commit).',
    test: (file) => /^apps\/[^/]+\/dist(\/|$)/.test(file),
  },
  {
    id: 'embed-dist',
    message:
      'Tracked backend/internal/web/dist is forbidden (generated embed payload).',
    test: (file) => /^backend\/internal\/web\/dist(\/|$)/.test(file),
  },
  {
    id: 'codeflow-template',
    message: 'Legacy codeflow_template paths must not reappear in the index.',
    test: (file) => /(^|\/)codeflow_template(\/|$)/.test(file),
  },
];

const tracked = listTrackedFiles().map(toPosix);
const violations = [];

for (const rule of rules) {
  const hits = tracked.filter((file) => rule.test(file));
  if (hits.length > 0) {
    violations.push({
      id: rule.id,
      message: rule.message,
      count: hits.length,
      samples: hits.slice(0, 12),
    });
  }
}

if (violations.length === 0) {
  console.log(`[check-repo-hygiene] OK (${tracked.length} tracked files)`);
  process.exit(0);
}

console.error('[check-repo-hygiene] FAILED');
for (const v of violations) {
  console.error(`- ${v.id}: ${v.count} path(s)`);
  console.error(`  ${v.message}`);
  for (const sample of v.samples) {
    console.error(`    ${sample}`);
  }
  if (v.count > v.samples.length) {
    console.error(`    ... and ${v.count - v.samples.length} more`);
  }
}
process.exit(1);