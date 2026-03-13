import fs from 'fs';
import path from 'path';
import { ensureArtifactsDir, importCoreDist } from './_shared/runtime.mjs';

const coreMod = await importCoreDist('index.js');
const memoryHookMod = await importCoreDist('hooks/MemoryInjectionHook.js');

const { HookManager, HookEvent } = coreMod;
const { MemoryInjectionHook } = memoryHookMod;

const report = {
  beforeSendMutation: false,
  postResponseTriggered: false,
  messageCompleteTriggered: false,
  memoryInjected: false,
  memoryDisableWorks: false,
  errors: [],
};

try {
  const hookManager = new HookManager();

  hookManager.register(HookEvent.BEFORE_SEND, async (payload) => ({
    ...payload,
    model: 'hook-mutated-model',
  }));

  const out1 = await hookManager.hook_before_send({
    messages: [{ role: 'user', content: 'hello hook' }],
  });
  report.beforeSendMutation = out1.model === 'hook-mutated-model';

  let postCalled = false;
  let messageCalled = false;
  hookManager.register(HookEvent.POST_RESPONSE, async () => {
    postCalled = true;
  });
  hookManager.register(HookEvent.MESSAGE_COMPLETE, async () => {
    messageCalled = true;
  });

  await hookManager.hook_post_response({ content: 'ok', model: 'test-model' });
  await hookManager.hook_on_message_complete({ role: 'user', content: 'done' });
  report.postResponseTriggered = postCalled;
  report.messageCompleteTriggered = messageCalled;

  const ragService = {
    async retrieve() {
      throw new Error('local retrieve should not be used in agent mode');
    },
    formatForInjection() {
      return 'Local fallback should not be used';
    },
  };

  const agentClient = {
    async assembleContext(payload) {
      return {
        context_block: `MemoryAgent Context: ${payload?.query || 'Remember provider URL'}`,
        source_count: 1,
        atomic_memories: [],
        samg_nodes: [],
        sources: [],
      };
    },
  };

  const memoryHook = new MemoryInjectionHook(hookManager, ragService, {
    enabled: true,
    sessionId: 'session-cli-test',
    position: 'prepend',
    maxInjectionLength: 2000,
    mode: 'agent',
  }, agentClient);
  memoryHook.register();

  const out2 = await hookManager.hook_before_send({
    messages: [{ role: 'user', content: 'What is my provider?' }],
  });

  report.memoryInjected =
    Array.isArray(out2.messages) &&
    out2.messages.length > 0 &&
    out2.messages[0].role === 'system' &&
    String(out2.messages[0].content || '').includes('MemoryAgent Context');

  memoryHook.disable();
  const out3 = await hookManager.hook_before_send({
    messages: [{ role: 'user', content: 'No injection expected' }],
  });
  report.memoryDisableWorks =
    Array.isArray(out3.messages) &&
    out3.messages.length === 1 &&
    out3.messages[0].role === 'user';
} catch (error) {
  report.errors.push(String(error));
}

const outPath = path.join(ensureArtifactsDir(), 'cli-hook-smoke-latest.json');
fs.writeFileSync(outPath, JSON.stringify(report, null, 2), 'utf8');
console.log(JSON.stringify(report, null, 2));
