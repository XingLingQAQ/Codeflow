import fs from 'fs';
import path from 'path';
import { chromium } from 'playwright';
import { ensureArtifactsDir, getBooleanEnv, getEnv } from './_shared/runtime.mjs';

const artifactsDir = ensureArtifactsDir();
const baseUrl = getEnv('CODEFLOW_BASE_URL', 'http://127.0.0.1:3000');
const headless = getBooleanEnv('CODEFLOW_HEADLESS', true);

const report = {
  timestamp: new Date().toISOString(),
  baseUrl,
  steps: [],
};

const mark = async (name, fn) => {
  try {
    await fn();
    report.steps.push({ name, ok: true });
  } catch (error) {
    report.steps.push({ name, ok: false, error: String(error) });
  }
};

const waitAnyText = async (page, texts, timeout = 8000) => {
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

await mark('home_loaded', async () => {
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await page.getByText('What would you like to build?').first().waitFor({ timeout: 10000 });
  await page.screenshot({ path: path.join(artifactsDir, 'ui-e2e-home.png'), fullPage: true });
});

await mark('home_to_plan', async () => {
  await page
    .getByPlaceholder('Describe your intent or search context...')
    .fill('Generate an executable plan for the current project');
  await page.keyboard.press('Enter');
  await page.getByText('CodeFlow Plan').first().waitFor({ timeout: 10000 });
  await page.screenshot({ path: path.join(artifactsDir, 'ui-e2e-plan.png'), fullPage: true });
});

await mark('plan_execute_modal', async () => {
  await page.getByRole('button', { name: 'Execute Plan' }).first().click();
  await waitAnyText(page, ['Workflow replay', 'Replay stream', 'No replay events captured yet'], 10000);

  const legacyTexts = ['Coder', 'Task: Create types/checkout.ts', 'Receiving context from Director...'];
  for (const legacyText of legacyTexts) {
    if (await page.getByText(legacyText).count()) {
      throw new Error(`Legacy mock replay text is still visible: ${legacyText}`);
    }
  }

  await page.screenshot({ path: path.join(artifactsDir, 'ui-e2e-plan-modal.png'), fullPage: true });
  const closeBtn = page.getByRole('button', { name: 'Close' }).first();
  if (await closeBtn.count()) {
    await closeBtn.click();
  } else {
    await page.keyboard.press('Escape');
  }
});

await mark('projects_open_and_back_to_plan', async () => {
  await page.getByRole('button', { name: 'Projects' }).first().click();
  await waitAnyText(page, ['Manage your active workspaces', 'Projects'], 10000);
  await page.getByRole('button', { name: 'New Project' }).first().click();
  await waitAnyText(page, ['Project title *', 'New Project'], 10000);
  const cancelBtn = page.getByRole('button', { name: 'Cancel' }).first();
  if (await cancelBtn.count()) {
    await cancelBtn.click();
  } else {
    await page.keyboard.press('Escape');
  }
  await page.getByRole('button', { name: 'Plan' }).first().click();
  await page.getByText('CodeFlow Plan').first().waitFor({ timeout: 10000 });
});

await mark('memory_panel_switch', async () => {
  await page.getByRole('button', { name: 'Memory' }).first().click();
  await waitAnyText(page, ['Atomic Memory', 'SAMG Graph', 'Raw Archive'], 10000);
  await page.screenshot({ path: path.join(artifactsDir, 'ui-e2e-memory.png'), fullPage: true });
});

await mark('settings_loaded', async () => {
  await page.getByRole('button', { name: 'Settings' }).first().click();
  await waitAnyText(page, ['Manage preferences and configurations', 'Profile & Account'], 10000);
  await page.getByRole('button', { name: 'Save Changes' }).first().waitFor({ timeout: 10000 });
  await page.screenshot({ path: path.join(artifactsDir, 'ui-e2e-settings.png'), fullPage: true });
});

await mark('agents_open_and_back_to_plan', async () => {
  await page.getByRole('button', { name: 'Agents' }).first().click();
  await waitAnyText(page, ['Active AI agents in your workspace', 'No agents'], 10000);
  await page.screenshot({ path: path.join(artifactsDir, 'ui-e2e-agents.png'), fullPage: true });
  await page.getByRole('button', { name: 'Plan' }).first().click();
  await page.getByText('CodeFlow Plan').first().waitFor({ timeout: 10000 });
});

report.passed = report.steps.filter((s) => s.ok).length;
report.failed = report.steps.filter((s) => !s.ok).length;
report.finalUrl = page.url();

const reportPath = path.join(artifactsDir, 'playwright-frontend-current-ui-report.json');
fs.writeFileSync(reportPath, JSON.stringify(report, null, 2), 'utf8');
console.log(JSON.stringify(report, null, 2));

await browser.close();

if (report.failed > 0) {
  process.exitCode = 1;
}
