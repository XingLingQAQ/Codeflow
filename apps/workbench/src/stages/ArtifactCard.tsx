import { FileText } from 'lucide-react';
import { Card, Badge } from '../ui';
import type { Artifact } from '../services-bridge/flows';
import { relTime } from '../lib/format';

const STATUS_TONE: Record<string, 'success' | 'warn' | 'neutral' | 'danger'> = {
  approved: 'success',
  draft: 'neutral',
  stale: 'warn',
  rejected: 'danger',
};

const STATUS_LABEL: Record<string, string> = {
  approved: '已批准',
  draft: '草稿',
  stale: '已过期',
  rejected: '已驳回',
};

/** One stage artifact (design.md / plan.md / …) as a compact card row. */
export function ArtifactCard({ artifact }: { artifact: Artifact }) {
  return (
    <Card className="flex items-center gap-3 px-3.5 py-2.5">
      <FileText size={15} className="shrink-0 text-ink-mute" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-mono text-[13px] text-ink">{artifact.type}</span>
          <span className="nums shrink-0 font-mono text-[11px] text-ink-mute">v{artifact.version}</span>
        </div>
        {artifact.content_ref && (
          <p className="mt-0.5 truncate font-mono text-[11px] text-ink-mute" title={artifact.content_ref}>
            {artifact.content_ref}
          </p>
        )}
      </div>
      <div className="flex shrink-0 flex-col items-end gap-1">
        <Badge tone={STATUS_TONE[artifact.status] ?? 'neutral'}>
          {STATUS_LABEL[artifact.status] ?? artifact.status}
        </Badge>
        <span className="text-[10px] text-ink-mute">{relTime(artifact.created_at)}</span>
      </div>
    </Card>
  );
}
