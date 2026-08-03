import { useEffect, useRef, useState } from 'react';
import { Send, Square } from 'lucide-react';
import { Button } from '../ui';
import { cn } from '../lib/cn';

const MAX_ROWS = 6;
const LINE_HEIGHT = 20;

export interface ChatComposerProps {
  disabled?: boolean;
  streaming: boolean;
  placeholder?: string;
  onSend: (text: string) => void;
  onStop: () => void;
}

/** Auto-sizing (1-6 rows) composer: Enter sends, Shift+Enter breaks, Esc stops. */
export function ChatComposer({ disabled, streaming, placeholder, onSend, onStop }: ChatComposerProps) {
  const [value, setValue] = useState('');
  const taRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = 'auto';
    ta.style.height = `${Math.min(ta.scrollHeight, MAX_ROWS * LINE_HEIGHT + 18)}px`;
  }, [value]);

  const send = () => {
    const text = value.trim();
    if (!text || disabled || streaming) return;
    onSend(text);
    setValue('');
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Escape' && streaming) {
      e.preventDefault();
      onStop();
      return;
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send();
    }
  };

  return (
    <div className="shrink-0 border-t border-line p-2.5">
      <div className="flex items-end gap-2">
        <textarea
          ref={taRef}
          rows={1}
          value={value}
          disabled={disabled}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={placeholder ?? '向 Agent 提问或下达指令…'}
          aria-label="对话输入框"
          className={cn(
            'min-h-9 flex-1 resize-none overflow-y-auto rounded-lg border border-line bg-raised px-3 py-2',
            'text-[13px] leading-5 text-ink placeholder:text-ink-mute',
            'transition-[border-color] duration-150 hover:border-line-strong focus:border-ink/50 focus:outline-none',
            'disabled:opacity-50',
          )}
        />
        {streaming ? (
          <Button
            variant="secondary"
            size="sm"
            className="h-9 shrink-0"
            onClick={onStop}
            aria-label="停止生成"
            title="停止生成（Esc）"
          >
            <Square size={13} className="fill-current" /> 停止
          </Button>
        ) : (
          <Button
            variant="primary"
            size="sm"
            className="h-9 shrink-0"
            disabled={disabled || !value.trim()}
            onClick={send}
            aria-label="发送"
          >
            <Send size={14} />
          </Button>
        )}
      </div>
      <p className="mt-1.5 text-[11px] leading-relaxed text-ink-mute">
        Enter 发送 · Shift+Enter 换行{streaming ? ' · Esc 停止' : ''}
      </p>
    </div>
  );
}
