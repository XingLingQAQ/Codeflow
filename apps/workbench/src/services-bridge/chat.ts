// Chat bridge. The backend has no chat HTTP endpoint yet (the conversations
// stream is a hub entry only), so this module is mock-first: in DEV+mock a
// local async generator produces role-aware streamed replies; in production it
// probes POST /api/v1/agents/chat once and reports the service unavailable
// (experimental) when the endpoint is missing.
import { getApiBase } from '../../api';
import { authHeadersFor, handleAuthHttpStatus } from './authProvider';
import { registerIdentityScopedCache } from './identityCaches';
import type { AgentInfo } from './agents';

export interface ChatTurn {
  role: 'user' | 'agent';
  content: string;
}

export interface ChatStreamRequest {
  projectId: string;
  stage: string;
  /** Chinese stage label for prose replies (falls back to the slug). */
  stageName?: string;
  agent: AgentInfo;
  message: string;
  files: string[];
  history: ChatTurn[];
}

export class ChatUnavailableError extends Error {
  constructor() {
    super('chat_unavailable');
    this.name = 'ChatUnavailableError';
  }
}

type Availability = 'unknown' | 'available' | 'unavailable';
let availability: Availability = 'unknown';

// The probe result belongs to one backend pairing (a different sidecar may
// serve a different endpoint surface), so it is identity-scoped: a re-pair
// resets it to 'unknown' and the next call probes again.
registerIdentityScopedCache('services-bridge/chat.availability', () => {
  availability = 'unknown';
});

function chatEndpoint(): string {
  return `${getApiBase()}/api/v1/agents/chat`;
}

/**
 * Lazily probe the experimental chat endpoint once. 404/503 marks it
 * unavailable for the session; network errors stay unknown so a later backend
 * start can still succeed.
 */
export async function probeChatEndpoint(signal?: AbortSignal): Promise<boolean> {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) return true;
  }
  if (availability !== 'unknown') return availability === 'available';
  try {
    const url = chatEndpoint();
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeadersFor(url) },
      body: JSON.stringify({ probe: true }),
      signal,
    });
    if (resp.status === 401 || resp.status === 403) {
      // Auth failure: rebind (401) / latch (403) and stay 'unknown' so the
      // next attempt after re-pair probes again instead of caching a denial.
      await handleAuthHttpStatus(url, resp.status);
      return false;
    }
    if (resp.status === 404 || resp.status === 503) {
      availability = 'unavailable';
      return false;
    }
    availability = 'available';
    return true;
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
    return false;
  }
}

function abortError(): DOMException {
  return new DOMException('Aborted', 'AbortError');
}

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(abortError());
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(abortError());
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

function fileSentence(files: string[]): string {
  if (files.length === 0) return '当前没有注入上下文文件，如需针对具体代码讨论，可在文件树勾选后再发送。';
  const shown = files.slice(0, 3).map((f) => `\`${f}\``).join('、');
  const suffix = files.length > 3 ? ` 等 ${files.length} 个文件` : '';
  return `我已读取注入的 ${files.length} 个上下文文件：${shown}${suffix}。`;
}

/** Compose a plausible role-aware mock reply (2-4 paragraphs, Chinese). */
function buildMockReply(req: ChatStreamRequest): string {
  const stage = req.stageName ?? req.stage;
  const files = req.files;
  const firstFile = files[0] ?? 'src/App.tsx';
  const ask = req.message.length > 42 ? `${req.message.slice(0, 42)}…` : req.message;
  const ctx = fileSentence(files);

  switch (req.agent.role_base) {
    case 'coder':
      return [
        `收到，围绕「${ask}」我先从${stage}阶段的现有实现入手。${ctx}`,
        `建议的最小改动集中在 \`${firstFile}\`：保持现有命名与结构，只调整必要的分支逻辑，避免顺手重构。示意如下：`,
        '```tsx\n// 仅示意：按守卫规则走暂存写入\nexport function apply(next: State) {\n  if (!validate(next)) return reject("guard");\n  return stage(next);\n}\n```',
        `写入会先落到暂存区，经守卫检查后再应用到工作树；如果被 \`no_console_log\` 之类的规则拦截，我会按拦截原因修正而不是绕过。你确认思路后我就开始起草补丁。`,
      ].join('\n\n');
    case 'critic':
      return [
        `针对「${ask}」，我按正确性 → 安全 → 性能的顺序过了一遍${stage}阶段的变更。${ctx}`,
        `主要疑点有两处：其一，\`${firstFile}\` 中的错误分支疑似不可达，建议补一个能触发它的用例；其二，输入未做长度校验，存在越界读取的风险，属于 major 级别。`,
        `以上均为「疑似风险」而非「已证实缺陷」——如果你能给出反例或规格引用，我会撤回对应结论；否则建议先补测试再合入。`,
      ].join('\n\n');
    case 'researcher':
      return [
        `关于「${ask}」，我把问题拆成了两个子问题分别检索：现状实现与可选方案。${ctx}`,
        `初步结论（已核实）：现有实现集中在 \`${firstFile}\`，未发现与提案冲突的约束；（推断）迁移成本主要在调用方适配，预计影响范围有限；（待证）外部依赖的版本兼容性还需要一次针对性验证。`,
        `证据均已按子问题归入证据篮，冲突项为零。如需继续深入，我建议下一步优先验证「待证」项，再进入${stage}阶段的方案定稿。`,
      ].join('\n\n');
    case 'sub':
      return [
        `检索目标「${ask}」已完成一轮扫描。${ctx}`,
        `发现 3 处相关位置：\`${firstFile}\` 的入口逻辑、其相邻的工具函数、以及一处仅在测试中引用的旧实现。全部为只读结论，未改动任何文件。`,
        `建议：入口逻辑与你的目标最相关，可以从它开始；旧实现疑似废弃，确认后可安排清理任务。需要我把逐条 file:line 引用整理出来吗？`,
      ].join('\n\n');
    default:
      return [
        `好的，我来统筹「${ask}」。当前处于「${stage}」阶段，${ctx}`,
        `编排安排：先由 Scout 完成相关上下文检索，再交给 Code Artisan 起草最小改动；若评审阶段出现分歧，会按 Gate 策略升级为多方辩论而不是我单方面裁决。`,
        `阶段推进前我会检查出口 Gate 的通过条件；一切就绪后你可以在阶段卡片上点击「完成并推进」。有需要随时打断我。`,
      ].join('\n\n');
  }
}

async function mockStream(
  req: ChatStreamRequest,
  onDelta: (delta: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  const text = buildMockReply(req);
  // Thinking latency before the first token.
  await delay(300 + Math.random() * 400, signal);
  let i = 0;
  while (i < text.length) {
    if (signal?.aborted) throw abortError();
    const n = 3 + Math.floor(Math.random() * 9);
    onDelta(text.slice(i, i + n));
    i += n;
    await delay(20 + Math.random() * 20, signal);
  }
}

/**
 * Stream one chat exchange. Resolves when the reply is complete; rejects with
 * AbortError on stop, ChatUnavailableError when no backend endpoint exists.
 */
export async function streamChat(
  req: ChatStreamRequest,
  onDelta: (delta: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      await mockStream(req, onDelta, signal);
      return;
    }
  }

  const available = await probeChatEndpoint(signal);
  if (!available) throw new ChatUnavailableError();

  // Experimental endpoint present: no streaming contract is defined yet, so
  // degrade to a single-shot exchange and emit the reply as one delta.
  const url = chatEndpoint();
  const resp = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeadersFor(url) },
    body: JSON.stringify({
      agent_id: req.agent.id,
      message: req.message,
      history: req.history,
      context: { project_id: req.projectId, stage: req.stage, files: req.files },
    }),
    signal,
  });
  if (resp.status === 401 || resp.status === 403) {
    await handleAuthHttpStatus(url, resp.status);
  }
  const body = (await resp.json().catch(() => null)) as
    | { success?: boolean; data?: { content?: string; message?: string }; error?: string }
    | null;
  if (!resp.ok || body?.success === false) {
    throw new Error(body?.error ?? `HTTP ${resp.status}`);
  }
  const content = body?.data?.content ?? body?.data?.message ?? '';
  if (content) onDelta(String(content));
}
