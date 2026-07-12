import fs from 'fs';
import path from 'path';
import { spawn } from 'child_process';
import { ensureArtifactsDir, getBooleanEnv, getEnv, repoRoot } from './_shared/runtime.mjs';

const artifactsDir = ensureArtifactsDir();
const baseUrl = getEnv('CODEFLOW_BASE_URL', 'http://127.0.0.1:3000');
const autoStartDev = getBooleanEnv('CODEFLOW_AUTOSTART_DEV_SERVER', true);
const runRealUser = getBooleanEnv('CODEFLOW_RUN_REAL_USER', true);
const devCwd = getEnv('CODEFLOW_DEV_CWD', path.resolve(repoRoot, 'apps', 'desktop'));
const devPort = getEnv('CODEFLOW_DEV_PORT', '3000');
const devPm = getEnv('CODEFLOW_DEV_PM', 'pnpm').toLowerCase();
const coreDistEntry = path.resolve(repoRoot, 'packages', 'core', 'dist', 'index.js');

const report = {
  timestamp: new Date().toISOString(),
  baseUrl,
  autoStartDev,
  runRealUser,
  devPm,
  steps: [],
};

const step = async (name, fn) => {
  const start = Date.now();
  try {
    const detail = await fn();
    report.steps.push({
      name,
      ok: true,
      durationMs: Date.now() - start,
      ...(detail || {}),
    });
    return true;
  } catch (error) {
    report.steps.push({
      name,
      ok: false,
      durationMs: Date.now() - start,
      error: String(error),
    });
    return false;
  }
};

const ensureCoreDist = async () => {
  if (fs.existsSync(coreDistEntry)) {
    return { reused: true };
  }

  const command = 'pnpm --filter @codeflow/core build';
  await new Promise((resolve, reject) => {
    const child =
      process.platform === 'win32'
        ? spawn('cmd.exe', ['/d', '/s', '/c', command], {
            cwd: repoRoot,
            env: { ...process.env },
            stdio: ['ignore', 'pipe', 'pipe'],
          })
        : spawn('sh', ['-lc', command], {
            cwd: repoRoot,
            env: { ...process.env },
            stdio: ['ignore', 'pipe', 'pipe'],
          });

    child.stdout.on('data', (buf) => process.stdout.write(String(buf)));
    child.stderr.on('data', (buf) => process.stderr.write(String(buf)));
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`Command failed (${command}) with exit code ${code}`));
    });
  });

  if (!fs.existsSync(coreDistEntry)) {
    throw new Error(`Missing core dist after build: ${coreDistEntry}`);
  }

  return { built: true, command };
};

const normalizeDevPm = () => {
  if (devPm === 'npm' || devPm === 'pnpm') {
    return devPm;
  }
  throw new Error(`Unsupported CODEFLOW_DEV_PM: ${devPm}`);
};

const resolvedDevPm = normalizeDevPm();

const ensureFrontendDeps = async () => {
  const lockfile = path.resolve(devCwd, 'package-lock.json');
  const nodeModulesDir = path.resolve(devCwd, 'node_modules');
  const command = resolvedDevPm === 'npm' && fs.existsSync(lockfile) ? 'npm ci' : `${resolvedDevPm} install`;

  if (fs.existsSync(nodeModulesDir)) {
    return { reused: true, packageManager: resolvedDevPm };
  }

  await new Promise((resolve, reject) => {
    const child =
      process.platform === 'win32'
        ? spawn('cmd.exe', ['/d', '/s', '/c', command], {
            cwd: devCwd,
            env: { ...process.env },
            stdio: ['ignore', 'pipe', 'pipe'],
          })
        : spawn('sh', ['-lc', command], {
            cwd: devCwd,
            env: { ...process.env },
            stdio: ['ignore', 'pipe', 'pipe'],
          });

    child.stdout.on('data', (buf) => process.stdout.write(String(buf)));
    child.stderr.on('data', (buf) => process.stderr.write(String(buf)));
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`Command failed (${command}) with exit code ${code}`));
    });
  });

  return { installed: true, packageManager: resolvedDevPm, command };
};

const runNodeScript = (scriptName, env = {}) =>
  new Promise((resolve, reject) => {
    const scriptPath = path.resolve(repoRoot, 'scripts', scriptName);
    const child = spawn(process.execPath, [scriptPath], {
      cwd: repoRoot,
      env: { ...process.env, ...env },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    let stdout = '';
    let stderr = '';

    child.stdout.on('data', (buf) => {
      const text = String(buf);
      stdout += text;
      process.stdout.write(text);
    });

    child.stderr.on('data', (buf) => {
      const text = String(buf);
      stderr += text;
      process.stderr.write(text);
    });

    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve({ code, stdout, stderr });
      } else {
        reject(new Error(`Script failed (${scriptName}) with exit code ${code}`));
      }
    });
  });

const isBaseUrlReady = async () => {
  try {
    const res = await fetch(baseUrl, { method: 'GET' });
    return res.ok;
  } catch {
    return false;
  }
};

const waitBaseUrlReady = async (timeoutMs = 120000) => {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (await isBaseUrlReady()) return true;
    await new Promise((r) => setTimeout(r, 1000));
  }
  return false;
};

const startDevServer = async () => {
  const command =
    resolvedDevPm === 'npm'
      ? `npm run dev -- --host 127.0.0.1 --port ${devPort}`
      : `pnpm dev --host 127.0.0.1 --port ${devPort}`;

  const child =
    process.platform === 'win32'
      ? spawn('cmd.exe', ['/d', '/s', '/c', command], {
          cwd: devCwd,
          env: { ...process.env },
          stdio: ['ignore', 'pipe', 'pipe'],
        })
      : spawn('sh', ['-lc', command], {
          cwd: devCwd,
          env: { ...process.env },
          stdio: ['ignore', 'pipe', 'pipe'],
        });

  child.stdout.on('data', (buf) => process.stdout.write(String(buf)));
  child.stderr.on('data', (buf) => process.stderr.write(String(buf)));

  const ready = await waitBaseUrlReady(120000);
  if (!ready) {
    throw new Error(`Dev server did not become ready at ${baseUrl}`);
  }

  return child;
};

const stopDevServer = async (child) => {
  if (!child || child.killed) return;

  if (process.platform === 'win32') {
    await new Promise((resolve) => {
      const killer = spawn('taskkill', ['/PID', String(child.pid), '/T', '/F'], {
        stdio: 'ignore',
      });
      killer.on('close', () => resolve());
      killer.on('error', () => resolve());
    });
  } else {
    child.kill('SIGTERM');
  }
};

let startedDevServer = null;

try {
  await step('prepare_core_dist', ensureCoreDist);
  await step('prepare_frontend_dependencies', ensureFrontendDeps);

  const prepared = await step('prepare_server', async () => {
    const ready = await isBaseUrlReady();
    if (ready) return { mode: 'reuse_existing' };
    if (!autoStartDev) {
      throw new Error(`Base URL not reachable and auto-start is disabled: ${baseUrl}`);
    }
    startedDevServer = await startDevServer();
    return { mode: 'started', pid: startedDevServer.pid, devCwd, devPm: resolvedDevPm };
  });

  if (prepared) {
    await step('cli_hook_actual_e2e', async () => {
      await runNodeScript('cli_hook_actual_e2e.mjs');
    });

    await step('cli_hook_smoke', async () => {
      await runNodeScript('cli_hook_smoke.mjs');
    });

    await step('playwright_frontend_current_ui_e2e', async () => {
      await runNodeScript('playwright_frontend_current_ui_e2e.mjs', {
        CODEFLOW_BASE_URL: baseUrl,
      });
    });

    if (runRealUser) {
      await step('playwright_real_user_test', async () => {
        await runNodeScript('playwright_real_user_test.mjs', {
          CODEFLOW_BASE_URL: baseUrl,
        });
      });
    }
  }
} finally {
  await step('cleanup_server', async () => {
    if (startedDevServer) {
      await stopDevServer(startedDevServer);
      return { stoppedPid: startedDevServer.pid };
    }
    return { skipped: true };
  });
}

report.passed = report.steps.filter((s) => s.ok).length;
report.failed = report.steps.filter((s) => !s.ok).length;

const reportPath = path.join(artifactsDir, 'e2e-all-report.json');
fs.writeFileSync(reportPath, JSON.stringify(report, null, 2), 'utf8');
console.log(JSON.stringify(report, null, 2));

if (report.failed > 0) {
  process.exitCode = 1;
}
