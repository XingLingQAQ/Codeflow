import React, { useState, useEffect, useRef } from 'react';
import { API_BASE } from '../api';

export interface ConnectionStatusProps {
  /** Polling interval in ms (default: 15000) */
  interval?: number;
}

export const ConnectionStatus: React.FC<ConnectionStatusProps> = ({ interval = 15000 }) => {
  const [online, setOnline] = useState<boolean | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval>>();

  useEffect(() => {
    let mounted = true;

    const check = async () => {
      try {
        const res = await fetch(`${API_BASE}/health`, { method: 'GET', signal: AbortSignal.timeout(5000) });
        if (mounted) setOnline(res.ok);
      } catch {
        if (mounted) setOnline(false);
      }
    };

    check();
    timerRef.current = setInterval(check, interval);

    return () => {
      mounted = false;
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [interval]);

  if (online === null) {
    return (
      <div className="bg-white/80 backdrop-blur-sm px-4 py-1.5 rounded-full shadow-lg shadow-slate-500/5 border border-slate-100 flex items-center gap-2.5">
        <span className="relative flex h-2 w-2">
          <span className="animate-pulse relative inline-flex rounded-full h-2 w-2 bg-slate-300"></span>
        </span>
        <span className="text-xs font-semibold text-slate-400 tracking-wide">CONNECTING...</span>
      </div>
    );
  }

  return (
    <div className={`bg-white/80 backdrop-blur-sm px-4 py-1.5 rounded-full shadow-lg border flex items-center gap-2.5 ${
      online ? 'shadow-blue-500/5 border-blue-100' : 'shadow-red-500/5 border-red-100'
    }`}>
      <span className="relative flex h-2 w-2">
        {online ? (
          <>
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
            <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
          </>
        ) : (
          <span className="relative inline-flex rounded-full h-2 w-2 bg-red-400"></span>
        )}
      </span>
      <span className={`text-xs font-semibold tracking-wide ${online ? 'text-slate-600' : 'text-red-500'}`}>
        {online ? 'SYSTEM ONLINE' : 'SYSTEM OFFLINE'}
      </span>
    </div>
  );
};
