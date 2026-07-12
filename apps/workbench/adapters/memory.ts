import type { LucideIcon } from 'lucide-react';
import { Activity, Archive, Layers } from 'lucide-react';
import type { MemoryAgentSource, MemorySourceKind, QueryMemoryNode } from '../types';

const MEMORY_SOURCE_PREVIEW_LIMIT = 120;

export interface MemorySourceKindMeta {
  label: string;
  badge: string;
  icon: LucideIcon;
}

export function buildMemorySourceKey(source: MemoryAgentSource): string {
  return [
    source.kind,
    source.id,
    source.node_id ?? '',
    source.source_id ?? '',
  ].join(':');
}

export function mergeRecordsById<T extends { id: string }>(primary: T[] = [], secondary: T[] = []): T[] {
  const merged = [...primary];
  const seen = new Set(primary.map((item) => item.id));

  for (const item of secondary) {
    if (seen.has(item.id)) continue;
    merged.push(item);
    seen.add(item.id);
  }

  return merged;
}

export function mergeMemorySources(primary: MemoryAgentSource[] = [], secondary: MemoryAgentSource[] = []): MemoryAgentSource[] {
  const merged = [...primary];
  const seen = new Set(primary.map((source) => buildMemorySourceKey(source)));

  for (const source of secondary) {
    const key = buildMemorySourceKey(source);
    if (seen.has(key)) continue;
    merged.push(source);
    seen.add(key);
  }

  return merged;
}

export function getMemorySourceKindMeta(kind: MemorySourceKind): MemorySourceKindMeta {
  switch (kind) {
    case 'atomic_memory':
      return {
        label: 'Atomic Memory',
        icon: Layers,
        badge: 'bg-red-50 text-red-600 border-red-200',
      };
    case 'samg_pointer':
      return {
        label: 'SAMG Pointer',
        icon: Activity,
        badge: 'bg-violet-50 text-violet-600 border-violet-200',
      };
    default:
      return {
        label: 'Raw Archive',
        icon: Archive,
        badge: 'bg-slate-100 text-slate-600 border-slate-200',
      };
  }
}

export function getMemorySourceTitle(source: MemoryAgentSource): string {
  return source.title || source.node_label || source.summary || source.source_id || source.id;
}

export function getMemorySourcePreview(source: MemoryAgentSource): string {
  const preview = source.summary || source.content || source.source_id || source.id;
  return preview.length > MEMORY_SOURCE_PREVIEW_LIMIT
    ? `${preview.slice(0, MEMORY_SOURCE_PREVIEW_LIMIT)}...`
    : preview;
}

export function mergeQueryMemoryNodes(primary: QueryMemoryNode[] = [], secondary: QueryMemoryNode[] = []): QueryMemoryNode[] {
  return mergeRecordsById(primary, secondary);
}
