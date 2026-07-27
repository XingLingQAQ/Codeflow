import fs from 'fs';
import path from 'path';
import { chromium } from 'playwright';
import { ensureArtifactsDir, getBooleanEnv, getEnv } from './_shared/runtime.mjs';

// New-shell smoke test against the dev mock layer (?mock=1). No backend required.
// Verifies: startup gate passes with mock fixtures → Dashboard renders a known
// project → Cmd+K opens and navigates to Projects → workbench FlowProgress
// renders distinguishable node states → theme toggle flips the .dark class.

const baseUrl = getEnv('CODEFLOW_BASE_URL', 'http://127.0.0.1:3000');
const headless = getBooleanEnv('CODEFLOW_HEADLESS', true);
const artifactsDir = ensureArtifactsDir();

const report = {
  timestamp: new Date().toISOString(),
  baseUrl,
  steps: [],
};

const mark = async (name, fn) => {
  const start = Date.now();
  try {
    const detail = await fn();
    report.steps.push({ name, ok: true, durationMs: Date.now() - start, ...(detail || {}) });
    return true;
  } catch (error) {
    report.steps.push({ name, ok: false, durationMs: Date.now() - start, error: String(error) });
    return false;
  }
};

const waitAnyText = async (page, texts, timeout = 12000) => {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    for (const text of texts) {
      const locator = page.getByText(text).first();
      if (await locator.count()) {
        await locator.waitFor({ timeout: 1000 });
        return text;
      }
    }
    await page.waitForTimeout(150);
  }
  throw new Error(`None of the texts appeared: ${texts.join(', ')}`);
};

const browser = await chromium.launch({ headless });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

// 1. Load the new shell with mock mode forced on. The startup gate reads the
//    /ready endpoint via the mock intercept (which returns all-green), so the
//    gate should pass quickly and render the Dashboard.
await mark('startup_gate_with_mock', async () => {
  await page.goto(`${baseUrl}?mock=1`, { waitUntil: 'domcontentloaded' });
  // The mock readiness returns ready immediately; the gate fades the splash.
  // Wait for the dashboard greeting or a fixture project name to confirm we
  // are past the gate and inside the shell.
  await waitAnyText(page, ['Nebula Console', '早上好', '下午好', '晚上好', '夜深了'], 20000);
  await page.screenshot({ path: path.join(artifactsDir, 'new-shell-smoke-dashboard.png'), fullPage: true });
  return { found: 'dashboard' };
});

// 2. Dashboard renders a known fixture project name.
await mark('dashboard_fixture_project', async () => {
  await page.getByText('Nebula Console').first().waitFor({ timeout: 10000 });
  return { project: 'Nebula Console' };
});

// 3. Open Cmd+K and navigate to the Projects page.
await mark('cmdk_navigate_to_projects', async () => {
  await page.keyboard.press('Meta+KeyK').catch(() => {});
  // Fallback for non-meta keyboards / CI: also try Control+K
  const paletteInput = page.getByPlaceholder('搜索命令、页面、阶段…').first();
  if (!(await paletteInput.count())) {
    await page.keyboard.press('Control+KeyK').catch(() => {});
  }
  await paletteInput.waitFor({ timeout: 5000 });
  await paletteInput.fill('项目');
  // Wait for the Projects item to be present and select it.
  const projectsItem = page.getByText('项目', { exact: false }).first();
  await projectsItem.waitFor({ timeout: 5000 });
  await page.keyboard.press('Enter');
  // Projects page subtitle text confirms the route.
  await waitAnyText(page, ['管理你的活跃工作区', '还没有项目', '项目'], 10000);
  await page.screenshot({ path: path.join(artifactsDir, 'new-shell-smoke-projects.png'), fullPage: true });
  return { route: page.url() };
});

// 4. Navigate to a workbench stage via direct URL and assert FlowProgress
//    renders distinguishable node states (done vs active vs pending).
//    BrowserRouter reads the path from the URL path (not hash), so the route
//    goes in the path and the mock flag stays in the query string.
await mark('workbench_flow_progress_states', async () => {
  await page.goto(`${baseUrl}/workbench/proj-nebula-001/coding?mock=1`, { waitUntil: 'domcontentloaded' });
  // FlowProgress renders 7 nodes immediately (all 'pending' until the flow
  // query resolves). Poll until at least one node shows a non-pending state
  // (the mock Nebula flow has done/active/skipped stages), then collect.
  const start = Date.now();
  let states = [];
  while (Date.now() - start < 20000) {
    const nodes = page.getByTestId('flow-progress-node');
    const count = await nodes.count();
    if (count >= 3) {
      states = await nodes.evaluateAll((els) => els.map((e) => e.getAttribute('data-state')));
      if (states.some((s) => s && s !== 'pending')) break;
    }
    await page.waitForTimeout(300);
  }
  const count = states.length;
  if (count < 3) throw new Error(`Expected >=3 flow progress nodes, got ${count}`);

  const stateCounts = states.reduce((acc, s) => {
    acc[s] = (acc[s] ?? 0) + 1;
    return acc;
  }, {});

  // The Nebula fixture: idea/design/planning=done, research=skipped, coding=active,
  // review/submit=pending. Assert at least one done, one active, one pending so
  // the states are visually distinguishable.
  if (!stateCounts.done) throw new Error(`No done nodes; states=${JSON.stringify(stateCounts)}`);
  if (!stateCounts.active) throw new Error(`No active nodes; states=${JSON.stringify(stateCounts)}`);
  if (!stateCounts.pending) throw new Error(`No pending nodes; states=${JSON.stringify(stateCounts)}`);

  await page.screenshot({ path: path.join(artifactsDir, 'new-shell-smoke-workbench.png'), fullPage: true });
  return { nodeCount: count, stateCounts };
});

// 5. Toggle theme via the NavRail button and assert the .dark class flips.
await mark('theme_toggle_flips_dark_class', async () => {
  const htmlEl = await page.locator('html');
  const beforeDark = await htmlEl.evaluate((el) => el.classList.contains('dark'));

  // The theme toggle button is labeled "切换主题".
  const toggleBtn = page.getByLabel('切换主题').first();
  await toggleBtn.waitFor({ timeout: 5000 });
  await toggleBtn.click();

  // Allow the zustand effect to apply the class.
  await page.waitForTimeout(300);
  const afterDark = await htmlEl.evaluate((el) => el.classList.contains('dark'));

  if (afterDark === beforeDark) {
    throw new Error(`Theme class did not flip: before=${beforeDark}, after=${afterDark}`);
  }
  await page.screenshot({ path: path.join(artifactsDir, 'new-shell-smoke-theme-toggled.png'), fullPage: true });
  return { beforeDark, afterDark };
});

report.passed = report.steps.filter((s) => s.ok).length;
report.failed = report.steps.filter((s) => !s.ok).length;
report.finalUrl = page.url();

const reportPath = path.join(artifactsDir, 'playwright-new-shell-smoke-report.json');
fs.writeFileSync(reportPath, JSON.stringify(report, null, 2), 'utf8');
console.log(JSON.stringify(report, null, 2));

await browser.close();

if (report.failed > 0) {
  process.exitCode = 1;
}
