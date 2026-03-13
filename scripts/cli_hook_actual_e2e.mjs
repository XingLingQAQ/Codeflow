import fs from 'fs';
import path from 'path';
import { ensureArtifactsDir, importCoreDist, repoRoot } from './_shared/runtime.mjs';

const [{ HookManager }, { HookEvent }, { MemoryInjectionHook }, { ProfileInjectionHook }, { MemoryShadowHooks }] =
  await Promise.all([
    importCoreDist('hooks/HookManager.js'),
    importCoreDist('hooks/types.js'),
    importCoreDist('hooks/MemoryInjectionHook.js'),
    importCoreDist('hooks/ProfileInjectionHook.js'),
    importCoreDist('hooks/MemoryShadowHooks.js'),
  ]);

const report = {
  timestamp: new Date().toISOString(),
  checks: [],
  passed: 0,
  failed: 0,
};

const addCheck = (name, ok, detail = {}) => {
  report.checks.push({ name, ok, ...detail });
  if (ok) report.passed += 1;
  else report.failed += 1;
};

async function main() {
  const hookManager = new HookManager();

  hookManager.register(HookEvent.BEFORE_SEND, async (payload) => ({
    ...payload,
    model: 'mutated-by-cli-hook',
  }));

  const mockProfileService = {
    async getProfile() {
      return {
        userId: 'u1',
        sections: {
          preferences: '中文输出',
          background: 'backend engineer',
          expertise: ['go', 'typescript'],
          communicationStyle: 'concise',
          goals: ['ship stable features'],
        },
      };
    },
  };

  const profileHook = new ProfileInjectionHook(hookManager, mockProfileService, {
    enabled: true,
    userId: 'u1',
    position: 'prepend',
  });
  profileHook.register();

  const ragService = {
    async retrieve() {
      throw new Error('local retrieve should not be used in agent mode');
    },
    formatForInjection() {
      return 'Local fallback should not be used';
    },
  };

  const shadowState = {
    extracted: 0,
    profileUpdated: 0,
    ingested: 0,
    lastIngestContent: '',
  };

  const mockAgentClient = {
    async assembleContext(payload) {
      return {
        context_block: `MemoryAgent Context: ${payload?.query || 'Memory context content'}`,
        source_count: 1,
        atomic_memories: [],
        samg_nodes: [],
        sources: [],
      };
    },
    async ingest(payload) {
      shadowState.ingested += 1;
      shadowState.lastIngestContent = payload?.content || '';
      return { ok: true };
    },
  };

  const memoryHook = new MemoryInjectionHook(hookManager, ragService, {
    enabled: true,
    sessionId: 's1',
    position: 'prepend',
    mode: 'agent',
  }, mockAgentClient);
  memoryHook.register();

  const beforeSendOutput = await hookManager.hook_before_send({
    messages: [{ role: 'user', content: 'Please continue fixing the issue' }],
  });

  addCheck('before_send_model_mutation', beforeSendOutput.model === 'mutated-by-cli-hook', {
    model: beforeSendOutput.model,
  });
  addCheck(
    'before_send_profile_injection',
    Boolean(
      beforeSendOutput.messages?.some(
        (m) => m.role === 'system' && String(m.content || '').includes('[用户画像]')
      )
    )
  );
  addCheck(
    'before_send_memory_injection',
    Boolean(
      beforeSendOutput.messages?.some(
        (m) => m.role === 'system' && String(m.content || '').includes('MemoryAgent Context')
      )
    )
  );

  const mockMemoryExtractor = {
    extractFromConversation(user, assistant, sessionId) {
      if (user && assistant && sessionId) shadowState.extracted += 1;
    },
  };

  const mockProfileUpdateService = {
    async update(userId, sessionId) {
      if (userId && sessionId) shadowState.profileUpdated += 1;
    },
  };

  const mockShadowScaffold = {
    async initialize() {},
  };
  const memoryShadowHooks = new MemoryShadowHooks(
    hookManager,
    {
      userId: 'u1',
      sessionId: 's1',
      projectRoot: repoRoot,
      enableMemoryExtraction: true,
      enableProfileUpdate: true,
      profileUpdateInterval: 2,
      enableAgentIngest: true,
    },
    mockMemoryExtractor,
    mockProfileUpdateService,
    mockShadowScaffold,
    mockAgentClient
  );
  memoryShadowHooks.register();

  await hookManager.hook_on_message_complete({ role: 'user', content: 'User message 1' });
  await hookManager.hook_post_response({ model: 'test-model', content: 'Assistant response 1' });
  await hookManager.hook_on_message_complete({ role: 'assistant', content: 'Assistant message done' });
  await new Promise((resolve) => setTimeout(resolve, 50));

  addCheck('post_response_memory_extract_called', shadowState.extracted === 1, {
    extracted: shadowState.extracted,
  });
  addCheck('post_response_agent_ingest_called', shadowState.ingested === 1, {
    ingested: shadowState.ingested,
    content_preview: shadowState.lastIngestContent.slice(0, 80),
  });
  addCheck('message_complete_profile_update_interval', shadowState.profileUpdated === 1, {
    profileUpdated: shadowState.profileUpdated,
  });

  memoryHook.disable();
  const afterDisable = await hookManager.hook_before_send({
    messages: [{ role: 'user', content: 'Second request' }],
  });
  addCheck(
    'memory_hook_disable_effective',
    !afterDisable.messages?.some(
      (m) => m.role === 'system' && String(m.content || '').includes('MemoryAgent Context')
    )
  );
}

try {
  await main();
} catch (error) {
  addCheck('script_runtime_error', false, { error: String(error) });
}

const outPath = path.join(ensureArtifactsDir(), 'cli-hook-actual-e2e-latest.json');
fs.writeFileSync(outPath, JSON.stringify(report, null, 2), 'utf8');
console.log(JSON.stringify(report, null, 2));

if (report.failed > 0) {
  process.exitCode = 1;
}
