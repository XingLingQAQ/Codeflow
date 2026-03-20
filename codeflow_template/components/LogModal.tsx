import React from 'react';
import { X, Copy, Maximize2, Minus, Zap, Terminal, Activity, Code, Eye, RefreshCw } from 'lucide-react';
import type { WorkflowReplayData } from '../types';

interface LogModalProps {
  isOpen: boolean;
  onClose: () => void;
  replay?: WorkflowReplayData | null;
}

export const LogModal: React.FC<LogModalProps> = ({ isOpen, onClose }) => {
  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center p-4">
      {/* Backdrop */}
      <div 
        className="absolute inset-0 bg-white/20 backdrop-blur-sm transition-opacity" 
        onClick={onClose}
      />

      {/* Modal Window */}
      <div className="w-full max-w-4xl bg-white/80 backdrop-blur-xl rounded-3xl shadow-2xl border border-white/50 flex flex-col max-h-[85vh] animate-in fade-in zoom-in-95 duration-200 overflow-hidden relative z-10 m-2">
        
        {/* Header */}
        <div className="px-4 md:px-6 py-4 border-b border-slate-200/60 bg-white/40 flex items-center justify-between shrink-0">
          <div className="flex items-center gap-3 md:gap-4">
            <div className="relative group">
              <div className="size-10 md:size-12 rounded-2xl bg-gradient-to-br from-blue-50 to-indigo-50 border border-white/60 shadow-sm flex items-center justify-center text-blue-600">
                <Code className="size-5 md:size-6" />
              </div>
              <div className="absolute -top-1 -right-1 flex h-3 w-3">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-3 w-3 bg-green-500 border-2 border-white"></span>
              </div>
            </div>
            <div className="flex flex-col">
              <h3 className="font-bold text-slate-800 text-base md:text-lg tracking-tight flex items-center gap-2">
                Coder
                <span className="px-2.5 py-0.5 bg-blue-100/50 backdrop-blur-sm text-blue-600 text-[10px] rounded-full font-bold uppercase tracking-wider border border-blue-200/50">Active</span>
              </h3>
              <span className="text-[10px] md:text-xs text-slate-500 font-mono bg-white/50 px-1.5 rounded truncate max-w-[150px] md:max-w-none">Task: Create types/checkout.ts</span>
            </div>
          </div>
          <div className="flex items-center gap-1 md:gap-2">
            <button className="size-8 hidden md:flex items-center justify-center rounded-full hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-all">
              <Minus className="size-4" />
            </button>
            <button className="size-8 hidden md:flex items-center justify-center rounded-full hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-all">
              <Maximize2 className="size-4" />
            </button>
            <button onClick={onClose} className="size-8 flex items-center justify-center rounded-full hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-all">
              <X className="size-4" />
            </button>
          </div>
        </div>

        {/* Content - Log Stream */}
        <div className="flex-1 overflow-y-auto p-4 md:p-8 bg-slate-50/30 font-mono text-xs md:text-[13px] leading-relaxed space-y-6">
          
          {/* Item 1 */}
          <div className="flex flex-col gap-1.5 opacity-80 hover:opacity-100 transition-opacity">
            <div className="flex items-center gap-3">
              <span className="text-[10px] font-bold text-slate-400 bg-white px-1.5 py-0.5 rounded border border-slate-200">10:42:05</span>
              <span className="text-emerald-600 font-bold tracking-wide">[PLANNER]</span>
              <span className="text-slate-600">Receiving context from Director...</span>
            </div>
          </div>

          {/* Item 2 */}
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-3">
              <span className="text-[10px] font-bold text-slate-400 bg-white px-1.5 py-0.5 rounded border border-slate-200">10:42:08</span>
              <span className="text-blue-600 font-bold tracking-wide">[THOUGHT]</span>
              <span className="text-slate-800 font-medium">Analyzing requirements for Stripe integration.</span>
            </div>
            <div className="pl-4 ml-[70px] md:ml-[85px] text-slate-500 border-l-2 border-indigo-100 py-1 space-y-1">
              <p>Need to define TypeScript interfaces for Checkout Session object.</p>
              <ul className="list-disc list-inside opacity-90 pl-1 space-y-0.5">
                <li>Fields required: <span className="text-slate-700 font-semibold bg-slate-100 px-1 rounded">customer_email</span></li>
                <li>Fields required: <span className="text-slate-700 font-semibold bg-slate-100 px-1 rounded">line_items</span></li>
                <li>Fields required: <span className="text-slate-700 font-semibold bg-slate-100 px-1 rounded">success_url</span></li>
              </ul>
            </div>
          </div>

          {/* Item 3 */}
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-3">
              <span className="text-[10px] font-bold text-slate-400 bg-white px-1.5 py-0.5 rounded border border-slate-200">10:42:15</span>
              <span className="text-purple-600 font-bold tracking-wide">[TOOL]</span>
              <span className="text-slate-800 break-all">Executing: <code className="bg-white border border-slate-200 px-1.5 py-0.5 rounded text-blue-500 font-semibold">fs.readFile('./old/CheckoutController.php')</code></span>
            </div>
          </div>

          {/* Item 4 - Active Generation */}
          <div className="flex flex-col gap-2 bg-white border border-slate-200 rounded-xl p-3 md:p-5 shadow-sm group hover:shadow-md transition-all duration-300">
            <div className="flex items-center justify-between gap-2 text-slate-800 mb-1">
              <div className="flex items-center gap-3">
                <span className="text-[10px] font-bold text-slate-400 bg-slate-50 px-1.5 py-0.5 rounded border border-slate-200">10:42:18</span>
                <span className="text-indigo-600 font-bold tracking-wide">[OUTPUT]</span>
                <span className="font-medium">Generating Interface...</span>
              </div>
              <RefreshCw className="size-4 text-blue-500 animate-spin" />
            </div>
            <div className="relative mt-2 bg-slate-50 rounded-lg p-3 border border-slate-100">
              <pre className="overflow-x-auto text-xs md:text-sm font-medium">
                <code className="text-slate-600">
                  <span className="text-purple-600">export interface</span> <span className="text-slate-900">CheckoutSession</span> {'{'}{'\n'}
                  {'  '}<span className="text-blue-500">id</span>: <span className="text-purple-600">string</span>;{'\n'}
                  {'  '}<span className="text-blue-500">object</span>: <span className="text-green-600">'checkout.session'</span>;{'\n'}
                  {'  '}<span className="text-blue-500">amount_total</span>: <span className="text-purple-600">number</span>;{'\n'}
                  {'  '}<span className="text-blue-500">currency</span>: <span className="text-purple-600">string</span>;{'\n'}
                  {'  '}<span className="text-blue-500">payment_status</span>: <span className="text-green-600">'paid'</span> | <span className="text-green-600">'unpaid'</span>;{'\n'}
                  {'  '}<span className="text-slate-400 italic">// ... determining metadata fields</span>{'\n'}
                  {'}'}
                </code>
              </pre>
            </div>
          </div>

          {/* Item 5 */}
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-3">
              <span className="text-[10px] font-bold text-slate-400 bg-white px-1.5 py-0.5 rounded border border-slate-200">10:42:22</span>
              <span className="text-blue-600 font-bold animate-pulse tracking-wide">[THOUGHT]</span>
              <span className="text-slate-800">Validating against existing user data structure...</span>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="px-4 md:px-8 py-4 md:py-5 border-t border-slate-200 bg-white/50 backdrop-blur-md flex justify-between items-center shrink-0">
          <div className="flex items-center gap-2.5 text-xs font-medium text-slate-500 bg-white px-3 py-1.5 rounded-full border border-slate-200 shadow-sm">
            <span className="size-2 rounded-full bg-green-500 animate-pulse shadow-[0_0_8px_rgba(34,197,94,0.6)]"></span>
            <span>Live Stream</span>
          </div>
          <div className="flex items-center gap-3">
            <button className="hidden md:flex px-5 py-2.5 rounded-full bg-white border border-slate-200 text-slate-600 text-sm font-medium hover:bg-slate-50 hover:text-blue-600 hover:border-blue-200 transition-all items-center gap-2">
              <Copy className="size-4" />
              Copy Log
            </button>
            <button onClick={onClose} className="px-6 md:px-8 py-2 md:py-2.5 rounded-full bg-gradient-to-r from-blue-600 to-indigo-600 text-white text-sm font-medium hover:shadow-lg hover:shadow-blue-500/20 hover:-translate-y-0.5 transition-all">
              Close
            </button>
          </div>
        </div>

      </div>
    </div>
  );
};