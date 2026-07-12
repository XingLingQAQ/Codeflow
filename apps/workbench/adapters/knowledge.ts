import type { MemoryAgentSource, QueryMemoryNode } from '../types';

export interface PlanKnowledgeContextFallbackInput {
  scenarioLabel: string;
  activeTaskTitle?: string;
  selectedPlanTitle?: string;
  dependencyCount: number;
  latestEvidenceTitle?: string;
}

export type PlanEmbeddedWikiAction =
  | { type: 'open_pack' }
  | { type: 'open_sources' }
  | { type: 'open_graph' }
  | { type: 'select_source'; sourceIndex: number }
  | { type: 'focus_graph_node'; nodeId: string };

export interface PlanEmbeddedWikiEntry {
  id: string;
  label: string;
  content: string;
  actionLabel?: string;
  action?: PlanEmbeddedWikiAction;
}

export function formatKnowledgeNodeTypes(types?: string[]): string {
  return types?.filter(Boolean).join(' · ') || 'Unclassified node';
}

export function formatKnowledgeNodeProperties(properties?: Record<string, unknown>): Array<[string, unknown]> {
  return Object.entries(properties ?? {}).filter(([, value]) => value !== undefined && value !== null);
}

export function buildKnowledgeNodeSummary(node: QueryMemoryNode | null): string {
  if (!node) return 'No node selected';
  return node.description || formatKnowledgeNodeTypes(node['@type']) || node.id;
}

export function buildPlanKnowledgeContextBlock(input: PlanKnowledgeContextFallbackInput): string {
  return [
    `Scenario: ${input.scenarioLabel}`,
    `Focus task: ${input.activeTaskTitle ?? input.selectedPlanTitle ?? 'No active task'}`,
    `Dependencies: ${input.dependencyCount}`,
    `Latest evidence: ${input.latestEvidenceTitle ?? 'No timeline evidence yet'}`,
    'Pack rule: export/import works on SAMG graph; pointer-level details stay on source cards.',
  ].join('\n');
}

function findSourceIndexForWikiLine(line: string, knowledgeSources: MemoryAgentSource[]): number {
  return knowledgeSources.findIndex((source) => {
    const preview = source.content || source.summary || '';
    return preview && line.includes(preview.slice(0, Math.min(preview.length, 24)));
  });
}

export function buildPlanEmbeddedWikiEntries(input: {
  contextBlock: string;
  knowledgeSources?: MemoryAgentSource[];
  knowledgeNodes?: QueryMemoryNode[];
  hasSelectedSource: boolean;
}): PlanEmbeddedWikiEntry[] {
  const knowledgeSources = input.knowledgeSources ?? [];
  const knowledgeNodes = input.knowledgeNodes ?? [];
  const lines = input.contextBlock
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean);

  return lines.map((line, index) => {
    const lower = line.toLowerCase();

    if (lower.startsWith('pack rule:')) {
      return {
        id: `wiki-pack-${index}`,
        label: 'Pack rule',
        content: line.slice('pack rule:'.length).trim(),
        actionLabel: '查看 pack',
        action: { type: 'open_pack' },
      };
    }

    if (lower.startsWith('latest evidence:')) {
      return {
        id: `wiki-evidence-${index}`,
        label: 'Latest evidence',
        content: line.slice('latest evidence:'.length).trim(),
        actionLabel: input.hasSelectedSource ? '打开来源' : '查看来源',
        action: { type: 'open_sources' },
      };
    }

    if (lower.startsWith('dependencies:')) {
      return {
        id: `wiki-graph-${index}`,
        label: 'Dependencies',
        content: line.slice('dependencies:'.length).trim(),
        actionLabel: '查看 graph',
        action: { type: 'open_graph' },
      };
    }

    if (line.startsWith('[atomic|') || line.startsWith('[samg|') || line.startsWith('[raw_archive|')) {
      const sourceIndex = findSourceIndexForWikiLine(line, knowledgeSources);
      return {
        id: `wiki-source-${index}`,
        label: line.startsWith('[samg|') ? 'Graph memory' : line.startsWith('[atomic|') ? 'Atomic memory' : 'Raw archive',
        content: line.replace(/^\[[^\]]+\]\s*/, ''),
        actionLabel: '打开来源',
        action: sourceIndex >= 0 ? { type: 'select_source', sourceIndex } : { type: 'open_sources' },
      };
    }

    const matchedNode = knowledgeNodes.find((node) => line.includes(node.label) || line.includes(node.id));
    if (matchedNode) {
      return {
        id: `wiki-node-${matchedNode.id}-${index}`,
        label: matchedNode.label,
        content: line,
        actionLabel: '跳到 graph',
        action: { type: 'focus_graph_node', nodeId: matchedNode.id },
      };
    }

    return {
      id: `wiki-line-${index}`,
      label: 'Context',
      content: line,
    };
  });
}
