import { readFileSync, readdirSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const targets = ['src', 'test'];

const rules = [
  { name: 'no-eval', test: (line) => !line.includes('eval(') },
  { name: 'no-var', test: (line) => !/(^|\s)var\s/.test(line) },
  { name: 'no-trailing-whitespace', test: (line) => !/[ \t]+$/.test(line) },
  { name: 'no-tab-indent', test: (line) => !/^\t/.test(line) },
];

let failures = 0;

for (const dir of targets) {
  const absDir = join(root, dir);
  for (const entry of readdirSync(absDir)) {
    if (!entry.endsWith('.ts')) {
      continue;
    }
    const file = join(absDir, entry);
    const rel = relative(root, file);
    const text = readFileSync(file, 'utf8');
    text.split('\n').forEach((line, index) => {
      for (const rule of rules) {
        if (!rule.test(line)) {
          console.error(`${rel}:${index + 1}: ${rule.name}`);
          failures += 1;
        }
      }
    });
    if (!text.endsWith('\n')) {
      console.error(`${rel}: file must end with a newline`);
      failures += 1;
    }
  }
}

if (failures > 0) {
  console.error(`lint: ${failures} violation(s)`);
  process.exit(1);
}
console.log('lint: ok');
