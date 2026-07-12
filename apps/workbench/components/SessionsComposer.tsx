import React from 'react';
import { Paperclip, Terminal, ArrowUp } from 'lucide-react';
import { IconButton } from './IconButton';

export const SessionsComposer: React.FC = () => (
  <div className="relative bg-slate-50 border border-slate-200 rounded-2xl p-2 focus-within:ring-2 focus-within:ring-blue-100 transition-all">
    <textarea className="w-full bg-transparent border-none text-sm resize-none focus:ring-0 px-3 py-2 h-12" placeholder="Reply to CodeFlow..."></textarea>
    <div className="flex justify-between items-center px-2 pb-1">
      <div className="flex gap-2 text-slate-400">
        <IconButton icon={<Paperclip size={18} />} tone="subtle" size="sm" className="hover:text-blue-500 transition-colors" />
        <IconButton icon={<Terminal size={18} />} tone="subtle" size="sm" className="hover:text-blue-500 transition-colors" />
      </div>
      <IconButton icon={<ArrowUp size={16} />} tone="primary" />
    </div>
  </div>
);
