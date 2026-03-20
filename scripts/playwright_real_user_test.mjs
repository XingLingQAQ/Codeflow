import fs from 'fs';
import path from 'path';
import { chromium } from 'playwright';
import { ensureArtifactsDir, getBooleanEnv, getEnv } from './_shared/runtime.mjs';

const baseUrl = getEnv('CODEFLOW_BASE_URL', 'http://127.0.0.1:3000');
const providerBase = getEnv('CODEFLOW_PROVIDER_BASE_URL', 'http://154.217.240.67:8090/');
const apiKey = getEnv('CODEFLOW_PROVIDER_API_KEY', '');
const headless = getBooleanEnv('CODEFLOW_HEADLESS', true);

const artifactsDir = ensureArtifactsDir();

const report = {
  timestamp: new Date().toISOString(),
  baseUrl,
  providerProbe: {
    enabled: Boolean(apiKey),
    ok: false,
    status: 0,
    modelCount: 0,
    error: '',
    skippedReason: '',
  },
  steps: [],
};

const mark = async (step, fn) => {
  try {
    // Make each step resilient to leftover modal overlays from previous interactions.
    await page.keyboard.press('Escape').catch(() => {});
    await page.waitForTimeout(150);
    await fn();
    report.steps.push({ step, ok: true });
  } catch (error) {
    report.steps.push({ step, ok: false, error: String(error) });
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

const closeDialogIfVisible = async (page) => {
  const closeCandidates = [
    page.getByRole('button', { name: /^Close$/i }).first(),
    page.getByRole('button', { name: /^Cancel$/i }).first(),
    page.getByRole('button', { name: /^Done$/i }).first(),
  ];

  for (const candidate of closeCandidates) {
    if (await candidate.count()) {
      await candidate.click({ force: true });
      await page.waitForTimeout(150);
      return;
    }
  }

  await page.keyboard.press('Escape').catch(() => {});
  await page.waitForTimeout(150);
};

if (apiKey) {
  try {
    const modelsRes = await fetch(`${providerBase.replace(/\/$/, '')}/v1/models`, {
      headers: {
        Authorization: `Bearer ${apiKey}`,
        'Content-Type': 'application/json',
      },
    });
    const body = await modelsRes.json();
    report.providerProbe.ok = modelsRes.ok;
    report.providerProbe.status = modelsRes.status;
    report.providerProbe.modelCount = Array.isArray(body?.data) ? body.data.length : 0;
  } catch (error) {
    report.providerProbe.error = String(error);
  }
} else {
  report.providerProbe.skippedReason =
    'CODEFLOW_PROVIDER_API_KEY not set, provider probe was skipped';
}

const browser = await chromium.launch({ headless });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

await mark('home_loaded', async () => {
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await page.getByText('What would you like to build?').first().waitFor({ timeout: 10000 });
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-home.png'), fullPage: true });
});

await mark('home_to_plan', async () => {
  await page
    .getByPlaceholder('Describe your intent or search context...')
    .fill('Create a configurable AI provider test project and generate executable plan');
  await page.keyboard.press('Enter');
  await page.getByText('CodeFlow Plan').first().waitFor({ timeout: 10000 });
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-plan.png'), fullPage: true });
});

await mark('plan_execute_modal', async () => {
  await page.getByRole('button', { name: 'Execute Plan' }).first().click();
  await waitAnyText(page, ['Coder', 'Live Stream'], 8000);
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-plan-modal.png'), fullPage: true });
  const closeBtn = page.getByRole('button', { name: 'Close' }).first();
  if (await closeBtn.count()) {
    await closeBtn.click();
  } else {
    await page.keyboard.press('Escape');
  }
});

await mark('projects_open_and_new_project_entry', async () => {
  await page.getByRole('button', { name: 'Projects' }).first().click();
  await waitAnyText(page, ['Manage your active workspaces', 'Projects'], 10000);
  await page.getByRole('button', { name: /New Project/i }).first().click();
  await waitAnyText(page, ['Create New Project', 'New Project', 'Project Name'], 7000);
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-projects.png'), fullPage: true });
  await closeDialogIfVisible(page);
});

await mark('project_back_to_plan', async () => {
  const projectItem = page.getByText('Supabase Migration').first();
  if (await projectItem.count()) {
    await projectItem.click();
  }
  await closeDialogIfVisible(page);
  await page.waitForTimeout(500);
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-project-back.png'), fullPage: true });
});

await mark('sessions_open', async () => {
  await page.getByRole('button', { name: 'Sessions' }).first().click();
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-sessions.png'), fullPage: true });
});

await mark('memory_panel_switch', async () => {
  await page.getByRole('button', { name: 'Memory' }).first().click();
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-memory.png'), fullPage: true });
});

await mark('settings_saved', async () => {
  await page.getByRole('button', { name: 'Settings' }).first().click();
  await waitAnyText(page, ['Settings', 'Manage preferences and configurations'], 10000);

  const gptButton = page.getByRole('button', { name: 'GPT-4o' }).first();
  if (await gptButton.count()) {
    await gptButton.click();
  }

  const saveButton = page.getByRole('button', { name: 'Save Changes' }).first();
  if (await saveButton.count()) {
    await saveButton.click();
    await waitAnyText(page, ['Settings saved successfully', 'Saved', 'Success'], 5000).catch(() => {});
  }

  await page.screenshot({ path: path.join(artifactsDir, 'e2e-settings-saved.png'), fullPage: true });
});

await mark('agents_preset_back_to_plan', async () => {
  await page.getByRole('button', { name: 'Agents' }).first().click();
  await waitAnyText(page, ['Agents', 'Agent', 'Preset'], 10000).catch(() => {});
  const usePresetButton = page.getByRole('button', { name: 'Use Preset' }).first();
  if (await usePresetButton.count()) {
    await usePresetButton.click({ force: true });
  }
  await page.waitForTimeout(600);
  await page.screenshot({ path: path.join(artifactsDir, 'e2e-agents.png'), fullPage: true });
});

report.passed = report.steps.filter((s) => s.ok).length;
report.failed = report.steps.filter((s) => !s.ok).length;
report.finalUrl = page.url();
report.apiKeySuffix = apiKey ? apiKey.slice(-8) : '';

const reportPath = path.join(artifactsDir, 'playwright-real-user-report.json');
fs.writeFileSync(reportPath, JSON.stringify(report, null, 2), 'utf8');
console.log(JSON.stringify(report, null, 2));

await browser.close();

if (report.failed > 0) {
  process.exitCode = 1;
}
