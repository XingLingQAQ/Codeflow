import { useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { ArrowDown, RotateCcw, CircleAlert } from 'lucide-react';
import type { ChatMessage } from '../stores/chat';
import type { AgentInfo } from '../services-bridge/agents';
import { Button } from '../ui';
import { relTime } from '../lib/format';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

interface ContentSegment {
  type: 'text' | 'code';
  lang?: string;
  text: string;
}

/**
 * Tiny fenced-code splitter (no markdown library): ``` toggles code segments;
 * an unterminated fence during streaming renders as code.
 */
function parseFences(content: string): ContentSegment[] {
  const segments: ContentSegment[] = [];
  const lines = content.split('\n');
  let buffer: string[] = [];
  let inCode = false;
  let lang: string | undefined;

  const flush = () => {
    if (buffer.length === 0) return;
    segments.push({ type: inCode ? 'code' : 'text', lang: inCode ? lang : undefined, text: buffer.join('\n') });
    buffer = [];
  };

  for (const line of lines) {
    const fence = line.match(/^```([\w-]*)\s*$/);
    if (fence) {
      flush();
      if (!inCode) lang = fence[1] || undefined;
      inCode = !inCode;
      continue;
    }
    buffer.push(line);
  }
  flush();
  return segments;
}

function StreamingCaret() {
  return <span className="caret-streaming ml-0.5 inline-block h-3.5 w-[2.5px] rounded-full align-middle" />;
}

function MessageBody({ content, streaming }: { content: string; streaming: boolean }) {
  const segments = parseFences(content);
  if (segments.length === 0 && streaming) return <StreamingCaret />;
  return (
    <>
      {segments.map((seg, i) =>
        seg.type === 'code' ? (
          <pre
            key={i}
            className="my-1.5 overflow-x-auto rounded-lg border border-line bg-hover px-2.5 py-2 font-mono text-[12px] leading-relaxed text-ink-dim"
          >
            {seg.text}
          </pre>
        ) : (
          <span key={i} className="whitespace-pre-wrap">
            {seg.text}
          </span>
        ),
      )}
      {streaming && <StreamingCaret />}
    </>
  );
}

export interface ChatMessagesProps {
  messages: ChatMessage[];
  agentById: Map<string, AgentInfo>;
  onRetry: (message: ChatMessage) => void;
}

export function ChatMessages({ messages, agentById, onRetry }: ChatMessagesProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  // Follow the stream unless the user scrolled up; then offer a jump-back pill.
  const [stick, setStick] = useState(true);
  const last = messages[messages.length - 1];
  const lastLen = last ? last.content.length + last.status : '';

  useEffect(() => {
    const el = containerRef.current;
    if (el && stick) el.scrollTop = el.scrollHeight;
  }, [messages.length, lastLen, stick]);

  const onScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    setStick(el.scrollHeight - el.scrollTop - el.clientHeight < 48);
  };

  return (
    <div className="relative min-h-0 flex-1">
      <div ref={containerRef} onScroll={onScroll} className="h-full space-y-3 overflow-y-auto px-3 py-3">
        {messages.map((m) => {
          if (m.role === 'user') {
            return (
              <div key={m.id} className="flex justify-end">
                <div className="max-w-[85%] rounded-xl rounded-br-sm bg-tint-active px-3 py-2 text-[13px] leading-relaxed text-ink">
                  <MessageBody content={m.content} streaming={false} />
                  {m.ctx && m.ctx.files.length > 0 && (
                    <div className="mt-1 text-right text-2xs text-ink-mute">
                      已附带 {m.ctx.files.length} 个文件上下文
                    </div>
                  )}
                </div>
              </div>
            );
          }
          const agent = m.agentId ? agentById.get(m.agentId) : undefined;
          const streaming = m.status === 'streaming';
          return (
            <div key={m.id} className="flex gap-2">
              <span className="grid size-6 shrink-0 place-items-center rounded-full bg-tint text-[13px]" aria-hidden>
                {agent?.avatar ?? '🤖'}
              </span>
              <div className="min-w-0 flex-1">
                <div className="mb-1 flex items-baseline gap-2 text-xs text-ink-mute">
                  <span className="font-medium text-ink-dim">{agent?.name ?? 'Agent'}</span>
                  <span className="nums">{relTime(m.at)}</span>
                  {m.status === 'stopped' && <span>已停止</span>}
                </div>
                <div
                  className={cn(
                    'rounded-xl rounded-tl-sm bg-raised px-3 py-2 text-[13px] leading-relaxed text-ink-dim',
                    m.status === 'error' && 'border border-danger/30 bg-danger/5',
                  )}
                >
                  <MessageBody content={m.content} streaming={streaming} />
                  {m.status === 'error' && (
                    <div className="mt-2 flex items-center justify-between gap-2 border-t border-danger/20 pt-2">
                      <span className="flex items-center gap-1.5 text-[12px] text-danger">
                        <CircleAlert size={13} /> 回复失败
                      </span>
                      <Button variant="ghost" size="sm" className="h-6 px-2 text-[12px]" onClick={() => onRetry(m)}>
                        <RotateCcw size={12} /> 重试
                      </Button>
                    </div>
                  )}
                </div>
              </div>
            </div>
          );
        })}
      </div>

      <AnimatePresence>
        {!stick && (
          <motion.button
            type="button"
            key="back-to-bottom"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 6 }}
            transition={{ duration: 0.15, ease: EASE_FLOW }}
            onClick={() => {
              const el = containerRef.current;
              if (el) el.scrollTop = el.scrollHeight;
              setStick(true);
            }}
            className="glass absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full px-3 py-1.5 text-[12px] text-ink-dim shadow-lg transition-colors hover:text-ink"
          >
            <ArrowDown size={13} /> 回到底部
          </motion.button>
        )}
      </AnimatePresence>
    </div>
  );
}
