import { useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import {
  ChevronDown,
  Layers,
  MessageSquareOff,
  MessageSquareQuote,
  Sparkles,
  Trash2,
  X,
  FileCode2,
} from 'lucide-react';
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  EmptyState,
  IconButton,
  Tooltip,
} from '../ui';
import { ChatMessages } from './ChatMessages';
import { ChatComposer } from './ChatComposer';
import { useAgents, useChatAvailability } from '../lib/queries';
import { useChatStore, nextMessageId, type ChatMessage } from '../stores/chat';
import { useEditorStore } from '../stores/editor';
import { useActivityStore, nextActivityId } from '../stores/activity';
import { streamChat, ChatUnavailableError, type ChatTurn } from '../services-bridge/chat';
import { roleLabel, type AgentInfo } from '../services-bridge/agents';
import { relTime } from '../lib/format';
import { cn } from '../lib/cn';
import type { StageType } from '../services-bridge/flows';

const HISTORY_WINDOW = 12;

export interface AgentCompanionProps {
  projectId: string;
  stage: StageType;
  stageName?: string;
}

function AgentMenuItem({
  agent,
  selected,
  onSelect,
}: {
  agent: AgentInfo;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <DropdownMenuItem onSelect={onSelect} className={cn(selected && 'bg-tint-active text-ink')}>
      <span className="grid size-6 shrink-0 place-items-center rounded-full bg-tint text-13">
        {agent.avatar ?? '🤖'}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          <span className="truncate text-13 font-medium">{agent.name}</span>
          <Badge tone="neutral" className="shrink-0 px-1.5 py-px text-[11px]">
            {roleLabel(agent.role_base)}
          </Badge>
        </span>
        {agent.description && (
          <span className="mt-0.5 block truncate text-xs text-ink-mute">{agent.description}</span>
        )}
      </span>
    </DropdownMenuItem>
  );
}

export function AgentCompanion({ projectId, stage, stageName }: AgentCompanionProps) {
  const agentsQ = useAgents();
  const agents = useMemo(() => agentsQ.data ?? [], [agentsQ.data]);
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const recommended = useMemo(() => agents.filter((a) => a.stage_tags?.includes(stage)), [agents, stage]);
  const others = useMemo(() => agents.filter((a) => !a.stage_tags?.includes(stage)), [agents, stage]);

  const chosenId = useChatStore((s) => s.agentByStage[stage]);
  const setAgentForStage = useChatStore((s) => s.setAgentForStage);
  const currentAgent = (chosenId && agentById.get(chosenId)) || recommended[0] || agents[0];

  // Stage-following selection: without a remembered choice, the first agent
  // recommended for this stage becomes active (its system prompt follows).
  useEffect(() => {
    if (!chosenId && recommended.length > 0) {
      setAgentForStage(stage, recommended[0].id);
    }
  }, [chosenId, recommended, stage, setAgentForStage]);

  const messages = useChatStore((s) => s.messagesByProject[projectId]) ?? [];
  const streamingId = useChatStore((s) => s.streamingId);
  const quote = useChatStore((s) => s.quote);
  const setQuote = useChatStore((s) => s.setQuote);
  const append = useChatStore((s) => s.append);
  const patchMessage = useChatStore((s) => s.patchMessage);
  const appendDelta = useChatStore((s) => s.appendDelta);
  const clearProject = useChatStore((s) => s.clearProject);
  const setStreamingId = useChatStore((s) => s.setStreamingId);

  const ctxSelection = useEditorStore((s) => s.contextByProject[projectId]) ?? {};
  const toggleContext = useEditorStore((s) => s.toggleContext);
  const ctxFiles = Object.keys(ctxSelection);

  const availQ = useChatAvailability();
  const unavailable = availQ.data === false;

  const latestActivity = useActivityStore((s) => s.entries[0]);

  const [clearOpen, setClearOpen] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const streaming = streamingId != null;

  // Esc anywhere stops the running stream (composer handles its own Esc too).
  useEffect(() => {
    if (!streaming) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        abortRef.current?.abort();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [streaming]);

  const startStream = (prompt: string, files: string[], agentMsgId: string, reuse: boolean) => {
    if (!currentAgent) return;
    const at = new Date().toISOString();
    if (reuse) {
      patchMessage(projectId, agentMsgId, {
        content: '',
        status: 'streaming',
        agentId: currentAgent.id,
        at,
      });
    } else {
      append(projectId, {
        id: agentMsgId,
        role: 'agent',
        agentId: currentAgent.id,
        content: '',
        status: 'streaming',
        at,
        ctx: { stage, files },
      });
    }
    setStreamingId(agentMsgId);
    const ctrl = new AbortController();
    abortRef.current = ctrl;

    const history: ChatTurn[] = (useChatStore.getState().messagesByProject[projectId] ?? [])
      .filter((m) => m.id !== agentMsgId && m.content && m.status !== 'error')
      .slice(-HISTORY_WINDOW)
      .map((m) => ({ role: m.role, content: m.content }));

    streamChat(
      { projectId, stage, stageName, agent: currentAgent, message: prompt, files, history },
      (delta) => appendDelta(projectId, agentMsgId, delta),
      ctrl.signal,
    )
      .then(() => {
        patchMessage(projectId, agentMsgId, { status: 'done' });
        useActivityStore.getState().push({
          id: nextActivityId('chat'),
          kind: 'chat',
          title: 'chat.reply',
          detail: `${currentAgent.name} 已答复`,
          at: new Date().toISOString(),
        });
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          patchMessage(projectId, agentMsgId, { status: 'stopped' });
          return;
        }
        const isUnavailable = err instanceof ChatUnavailableError;
        const detail = isUnavailable
          ? '对话服务未启用：后端尚未提供 Agent 对话端点（实验特性）。'
          : err instanceof Error
            ? err.message
            : String(err);
        const existing = useChatStore.getState().messagesByProject[projectId]?.find((m) => m.id === agentMsgId);
        patchMessage(projectId, agentMsgId, {
          status: 'error',
          content: existing?.content || detail,
        });
        if (!isUnavailable) toast.error(`对话失败：${detail}`);
      })
      .finally(() => {
        setStreamingId(null);
        abortRef.current = null;
      });
  };

  const send = (text: string) => {
    if (!currentAgent || streaming) return;
    const files = [...ctxFiles];
    // A quoted editor selection rides along as a fenced block prefix.
    const content = quote
      ? `【引用 ${quote.path}:L${quote.startLine}-L${quote.endLine}】\n\`\`\`\n${quote.snippet}\n\`\`\`\n\n${text}`
      : text;
    if (quote) setQuote(null);
    append(projectId, {
      id: nextMessageId(),
      role: 'user',
      content,
      status: 'done',
      at: new Date().toISOString(),
      ctx: { stage, files },
    });
    startStream(content, files, nextMessageId(), false);
  };

  const retry = (message: ChatMessage) => {
    if (streaming) return;
    const list = useChatStore.getState().messagesByProject[projectId] ?? [];
    const idx = list.findIndex((m) => m.id === message.id);
    const prevUser = [...list.slice(0, Math.max(idx, 0))].reverse().find((m) => m.role === 'user');
    if (!prevUser) return;
    startStream(prevUser.content, prevUser.ctx?.files ?? [...ctxFiles], message.id, true);
  };

  const stop = () => abortRef.current?.abort();

  const dotTooltip = streaming
    ? `${currentAgent?.name ?? 'Agent'} 正在生成回复…`
    : latestActivity
      ? `最新动态：${latestActivity.title}${latestActivity.detail ? ` — ${latestActivity.detail}` : ''}（${relTime(latestActivity.at)}）`
      : '暂无实时动态';

  return (
    <aside className="flex h-full flex-col border-l border-line bg-panel">
      {/* Header: status dot + agent switcher + clear */}
      <div className="flex h-10 shrink-0 items-center gap-2 border-b border-line px-2.5">
        <Tooltip content={dotTooltip} side="bottom">
          <span className="grid size-5 shrink-0 cursor-default place-items-center" aria-label="Agent 状态">
            <span
              className={cn(
                'inline-block rounded-full',
                streaming
                  ? 'animate-streamdot size-2.5 bg-[image:var(--grad-signature)] shadow-[0_0_6px_oklch(0.55_0.20_285/0.5)]'
                  : 'size-2 bg-ink-mute/60',
              )}
            />
          </span>
        </Tooltip>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className="flex min-w-0 flex-1 items-center gap-2 rounded-lg px-1.5 py-1 text-left transition-colors hover:bg-tint"
              aria-label="切换 Agent"
            >
              <span className="grid size-6 shrink-0 place-items-center rounded-full bg-tint text-13">
                {currentAgent?.avatar ?? '🤖'}
              </span>
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5">
                  <span className="truncate text-13 font-medium text-ink">
                    {currentAgent?.name ?? 'Agent'}
                  </span>
                  {currentAgent && (
                    <Badge tone="neutral" className="shrink-0 px-1.5 py-px text-[11px]">
                      {roleLabel(currentAgent.role_base)}
                    </Badge>
                  )}
                </span>
              </span>
              <ChevronDown size={13} className="shrink-0 text-ink-mute" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-72">
            {recommended.length > 0 && (
              <>
                <DropdownMenuLabel>推荐 · {stageName ?? stage}阶段</DropdownMenuLabel>
                {recommended.map((a) => (
                  <AgentMenuItem
                    key={a.id}
                    agent={a}
                    selected={a.id === currentAgent?.id}
                    onSelect={() => setAgentForStage(stage, a.id)}
                  />
                ))}
              </>
            )}
            {others.length > 0 && (
              <>
                {recommended.length > 0 && <DropdownMenuSeparator />}
                <DropdownMenuLabel>全部</DropdownMenuLabel>
                {others.map((a) => (
                  <AgentMenuItem
                    key={a.id}
                    agent={a}
                    selected={a.id === currentAgent?.id}
                    onSelect={() => setAgentForStage(stage, a.id)}
                  />
                ))}
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        <Tooltip content="清空当前项目对话" side="bottom">
          <IconButton
            size="sm"
            aria-label="清空对话"
            disabled={messages.length === 0}
            onClick={() => setClearOpen(true)}
          >
            <Trash2 size={14} />
          </IconButton>
        </Tooltip>
      </div>

      {/* Context chips: stage + injected files */}
      <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-b border-line px-3 py-1.5">
        <Badge tone="neutral" className="gap-1">
          <Layers size={11} /> {stageName ?? stage}
        </Badge>
        {ctxFiles.length > 0 ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button type="button" aria-label="查看上下文文件">
                <Badge tone="accent" className="cursor-pointer gap-1 transition-colors hover:bg-hover">
                  <FileCode2 size={11} /> {ctxFiles.length} 个文件
                  <ChevronDown size={10} />
                </Badge>
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-64 w-72 overflow-y-auto">
              <DropdownMenuLabel>注入的上下文文件</DropdownMenuLabel>
              {ctxFiles.map((path) => (
                <DropdownMenuItem
                  key={path}
                  onSelect={(e) => {
                    e.preventDefault();
                    toggleContext(projectId, path, 0);
                  }}
                >
                  <span className="min-w-0 flex-1 truncate font-mono text-[12px]">{path}</span>
                  <X size={12} className="shrink-0 text-ink-mute" />
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <span className="text-xs text-ink-dim">未注入文件 — 可在编码画布的文件树勾选</span>
        )}
      </div>

      {/* Body */}
      {unavailable ? (
        <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto">
          <EmptyState
            icon={<MessageSquareOff size={20} />}
            title="对话服务未启用"
            description="后端尚未提供 Agent 对话端点（实验特性）。接入后，这里将提供基于阶段上下文的多轮对话。"
          />
        </div>
      ) : messages.length === 0 ? (
        <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto">
          <EmptyState
            icon={<Sparkles size={20} />}
            title="开始与 Agent 对话"
            description={`当前阶段与勾选的文件会作为上下文注入。${currentAgent ? `由 ${currentAgent.name} 负责${stageName ?? ''}阶段的问题。` : ''}`}
          />
        </div>
      ) : (
        <ChatMessages messages={messages} agentById={agentById} onRetry={retry} />
      )}

      {/* Pending quoted selection from the editor */}
      {quote && (
        <div className="flex shrink-0 items-center gap-2 border-t border-line bg-tint px-3 py-1.5">
          <MessageSquareQuote size={13} className="shrink-0 text-ink-mute" />
          <span className="nums min-w-0 flex-1 truncate font-mono text-xs text-ink-dim" title={quote.snippet}>
            {quote.path}:L{quote.startLine}-L{quote.endLine}
          </span>
          <button
            type="button"
            aria-label="移除引用"
            onClick={() => setQuote(null)}
            className="shrink-0 rounded p-0.5 text-ink-mute transition-colors hover:bg-tint-active hover:text-ink"
          >
            <X size={12} />
          </button>
        </div>
      )}

      <ChatComposer
        disabled={unavailable || !currentAgent}
        streaming={streaming}
        onSend={send}
        onStop={stop}
        placeholder={unavailable ? '对话服务未启用' : undefined}
      />

      {/* Clear-conversation confirm */}
      <Dialog open={clearOpen} onOpenChange={setClearOpen}>
        <DialogContent className="w-[min(92vw,400px)]">
          <DialogTitle>清空对话</DialogTitle>
          <DialogDescription>
            将删除当前项目的全部 {messages.length} 条对话记录，此操作不可撤销。
          </DialogDescription>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setClearOpen(false)}>
              取消
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => {
                abortRef.current?.abort();
                clearProject(projectId);
                setClearOpen(false);
                toast.success('已清空当前项目对话');
              }}
            >
              清空
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </aside>
  );
}
