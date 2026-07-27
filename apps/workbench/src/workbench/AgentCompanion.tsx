import { Bot, Send, Sparkles } from 'lucide-react';
import { Badge, Textarea, Button, EmptyState, Skeleton } from '../ui';
import { cn } from '../lib/cn';

interface AgentCompanionProps {
  stageName?: string;
  collapsed?: boolean;
}

export function AgentCompanion({ stageName, collapsed }: AgentCompanionProps) {
  if (collapsed) return null;

  return (
    <aside className="flex h-full flex-col border-l border-line bg-panel">
      <div className="flex h-10 shrink-0 items-center gap-2 border-b border-line px-3">
        <Bot size={15} className="text-ink-dim" />
        <span className="text-[13px] font-medium text-ink">Agent 伴侣</span>
        {stageName && <Badge tone="neutral">{stageName}</Badge>}
      </div>

      <div className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 overflow-y-auto px-3 py-4">
          <EmptyState
            icon={<Sparkles size={20} />}
            title="就绪"
            description="Agent 将在此阶段为你提供上下文感知对话。输入问题或指令开始交互。"
          />

          <div className="mt-6 space-y-3 px-2">
            <div className="flex gap-2">
              <div className="size-6 shrink-0 rounded-full bg-ink/15" />
              <div className="flex-1 space-y-1.5 rounded-xl rounded-tl-sm bg-raised p-3">
                <Skeleton className="h-3 w-full" />
                <Skeleton className="h-3 w-4/5" />
              </div>
            </div>
            <div className="flex items-end gap-2">
              <div className="size-6 shrink-0 rounded-full bg-ink/15" />
              <div className="rounded-xl rounded-tl-sm bg-raised p-3">
                <div className="flex gap-0.5">
                  <span className="inline-block size-1.5 animate-pulse-dot rounded-full bg-ink-mute" />
                  <span className="inline-block size-1.5 animate-pulse-dot rounded-full bg-ink-mute delay-75" />
                  <span className="inline-block size-1.5 animate-pulse-dot rounded-full bg-ink-mute delay-150" />
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="shrink-0 border-t border-line p-3">
          <div className="mb-2 flex items-center gap-1.5">
            <Badge tone="neutral">模型 —</Badge>
            <span className="text-[11px] text-ink-mute">阶段上下文已注入</span>
          </div>
          <div className="flex items-end gap-2">
            <Textarea rows={2} placeholder="输入消息…" className="flex-1 text-[13px]" />
            <Button variant="primary" size="sm" className="shrink-0">
              <Send size={14} />
            </Button>
          </div>
        </div>
      </div>
    </aside>
  );
}
