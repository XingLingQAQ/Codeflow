import type { Project } from '../../types';
import type { Flow, Stage, FlowTemplateInfo } from '../services-bridge/flows';
import type { WorkspaceEntry } from '../services-bridge/workspace';
import type { GuardRule, ExemptionRequestRecord } from '../services-bridge/guard';
import type { AgentInfo } from '../services-bridge/agents';

const now = Date.now() / 1000;
const iso = (offset = 0) => new Date(Date.now() + offset * 1000).toISOString();

function stage(
  id: string, type: Stage['type'], name: string, order: number, status: Stage['status'],
  opts: Partial<Stage> = {},
): Stage {
  return { id, type, name, canvas: type, status, optional: false, order, gates: [], ...opts };
}

// ---------- Projects ----------

export const MOCK_PROJECTS: Project[] = [
  {
    id: 'proj-nebula-001',
    title: 'Nebula Console',
    description: '下一代星云管理控制台，支持实时监控与自动扩缩容。',
    status: 'active',
    progress: 65,
    tags: ['frontend', 'react', 'typescript'],
    git_branch: 'feat/nebula-ui',
    created_at: now - 86400 * 12,
    updated_at: now - 3600,
    last_active: now - 1800,
    plan_ids: ['plan-001'],
  },
  {
    id: 'proj-pipeline-002',
    title: '数据管线重构',
    description: '将批量 ETL 管线迁移至流式处理架构，提升实时性与容错能力。',
    status: 'active',
    progress: 30,
    tags: ['backend', 'go', 'streaming'],
    git_branch: 'refactor/pipeline',
    created_at: now - 86400 * 20,
    updated_at: now - 7200,
    last_active: now - 3600,
  },
  {
    id: 'proj-mobile-003',
    title: '移动端伴侣',
    description: '远程审批与 Agent 对话的手机伴侣端，支持配对与推送通知。',
    status: 'planning',
    progress: 0,
    tags: ['mobile', 'react-native'],
    created_at: now - 86400 * 5,
    updated_at: now - 86400 * 2,
    last_active: now - 86400 * 2,
  },
  {
    id: 'proj-legacy-004',
    title: '旧系统归档',
    description: '遗留 PHP 系统迁移完成后的代码归档与文档沉淀。',
    status: 'archived',
    progress: 100,
    tags: ['legacy', 'php'],
    created_at: now - 86400 * 60,
    updated_at: now - 86400 * 30,
    last_active: now - 86400 * 30,
  },
];

// ---------- Flows ----------

export const MOCK_FLOWS: Flow[] = [
  {
    id: 'flow-nebula-main',
    project_id: 'proj-nebula-001',
    template_id: 'new_project',
    status: 'active',
    stages: [
      stage('s1', 'idea', '意图', 0, 'done', { snapshot_id: 'snap-001' }),
      stage('s2', 'design', '设计', 1, 'done', { snapshot_id: 'snap-002' }),
      stage('s3', 'planning', '规划', 2, 'done', { snapshot_id: 'snap-003' }),
      stage('s4', 'research', '调研', 3, 'skipped', { optional: true }),
      stage('s5', 'coding', '编码', 4, 'active'),
      stage('s6', 'review', '评审', 5, 'pending'),
      stage('s7', 'submit', '提交', 6, 'pending'),
    ],
    loops: [{ from: 'review', to: 'coding' }],
    artifacts: [
      { id: 'art-001', stage_id: 's1', type: 'idea.md', version: 1, status: 'approved', content_ref: 'ideas/nebula.md', created_at: iso(-86400 * 10) },
      { id: 'art-002', stage_id: 's2', type: 'design.md', version: 1, status: 'approved', content_ref: 'designs/nebula-arch.md', created_at: iso(-86400 * 8) },
      { id: 'art-003', stage_id: 's3', type: 'plan.md', version: 1, status: 'approved', content_ref: 'plans/nebula-tasks.md', created_at: iso(-86400 * 6) },
      { id: 'art-004', stage_id: 's5', type: 'App.tsx', version: 1, status: 'draft', content_ref: 'src/console/App.tsx', created_at: iso(-3600) },
    ],
    events: [
      { id: 'ev1', type: 'stage_entered', stage_id: 's5', message: '进入编码阶段', timestamp: iso(-7200) },
    ],
    created_at: iso(-86400 * 12),
    updated_at: iso(-3600),
  },
  {
    id: 'flow-pipeline-gate',
    project_id: 'proj-pipeline-002',
    template_id: 'new_project',
    status: 'active',
    stages: [
      stage('ps1', 'idea', '意图', 0, 'done', { snapshot_id: 'snap-p01' }),
      stage('ps2', 'design', '设计', 1, 'waiting_gate', {
        gates: [{ id: 'g1', phase: 'exit', kind: 'human_approval', on_fail: 'escalate_to_debate', passed: false }],
      }),
      stage('ps3', 'planning', '规划', 2, 'pending'),
      stage('ps4', 'research', '调研', 3, 'pending', { optional: true }),
      stage('ps5', 'coding', '编码', 4, 'pending'),
      stage('ps6', 'review', '评审', 5, 'pending'),
      stage('ps7', 'submit', '提交', 6, 'pending'),
    ],
    loops: [{ from: 'review', to: 'coding' }],
    artifacts: [
      { id: 'art-p01', stage_id: 'ps1', type: 'idea.md', version: 1, status: 'approved', created_at: iso(-86400 * 18) },
    ],
    events: [],
    created_at: iso(-86400 * 20),
    updated_at: iso(-7200),
  },
  {
    id: 'flow-legacy-abort',
    project_id: 'proj-legacy-004',
    template_id: 'import_project',
    status: 'aborted',
    stages: [
      stage('ls1', 'idea', '意图', 0, 'done'),
      stage('ls2', 'coding', '编码', 1, 'done'),
      stage('ls3', 'review', '评审', 2, 'pending'),
      stage('ls4', 'submit', '提交', 3, 'pending'),
    ],
    artifacts: [],
    events: [
      { id: 'ev-l1', type: 'flow_aborted', message: '项目已归档，终止流程', timestamp: iso(-86400 * 30) },
    ],
    created_at: iso(-86400 * 55),
    updated_at: iso(-86400 * 30),
  },
];

// ---------- Templates ----------

export const MOCK_TEMPLATES: FlowTemplateInfo[] = [
  {
    id: 'new_project',
    name: '新建项目',
    description: '标准 7 阶段开发流程：想法 → 设计 → 规划 → 调研 → 编码 → 评审 → 提交。',
    stages: [
      { type: 'idea', name: '意图', optional: false },
      { type: 'design', name: '设计', optional: false },
      { type: 'planning', name: '规划', optional: false },
      { type: 'research', name: '调研', optional: true },
      { type: 'coding', name: '编码', optional: false },
      { type: 'review', name: '评审', optional: false },
      { type: 'submit', name: '提交', optional: false },
    ],
  },
  {
    id: 'import_project',
    name: '导入项目',
    description: '已有代码库导入：理解 → 编码 → 评审 → 提交（跳过设计/规划）。',
    stages: [
      { type: 'idea', name: '意图', optional: false },
      { type: 'comprehension', name: '理解', optional: false },
      { type: 'coding', name: '编码', optional: false },
      { type: 'review', name: '评审', optional: false },
      { type: 'submit', name: '提交', optional: false },
    ],
  },
];

// ---------- Workspace ----------

export const MOCK_WORKSPACE: WorkspaceEntry[] = [
  { name: 'src', path: 'src', is_dir: true, mod_time: iso(-3600) },
  { name: 'components', path: 'src/components', is_dir: true, mod_time: iso(-3600) },
  { name: 'Dashboard.tsx', path: 'src/components/Dashboard.tsx', is_dir: false, size: 4280, mod_time: iso(-3800) },
  { name: 'Sidebar.tsx', path: 'src/components/Sidebar.tsx', is_dir: false, size: 2150, mod_time: iso(-5400) },
  { name: 'Header.tsx', path: 'src/components/Header.tsx', is_dir: false, size: 1820, mod_time: iso(-7200) },
  { name: 'StatusBar.tsx', path: 'src/components/StatusBar.tsx', is_dir: false, size: 980, mod_time: iso(-14400) },
  { name: 'utils', path: 'src/utils', is_dir: true, mod_time: iso(-7200) },
  { name: 'api.ts', path: 'src/utils/api.ts', is_dir: false, size: 3640, mod_time: iso(-7200) },
  { name: 'format.ts', path: 'src/utils/format.ts', is_dir: false, size: 1200, mod_time: iso(-14400) },
  { name: 'hooks.ts', path: 'src/utils/hooks.ts', is_dir: false, size: 2800, mod_time: iso(-10800) },
  { name: 'types', path: 'src/types', is_dir: true, mod_time: iso(-86400) },
  { name: 'index.ts', path: 'src/types/index.ts', is_dir: false, size: 450, mod_time: iso(-86400) },
  { name: 'models.ts', path: 'src/types/models.ts', is_dir: false, size: 1560, mod_time: iso(-86400) },
  { name: 'main.tsx', path: 'src/main.tsx', is_dir: false, size: 520, mod_time: iso(-3600) },
  { name: 'App.tsx', path: 'src/App.tsx', is_dir: false, size: 6800, mod_time: iso(-1800) },
  { name: 'index.css', path: 'src/index.css', is_dir: false, size: 340, mod_time: iso(-86400 * 3) },
  { name: 'public', path: 'public', is_dir: true, mod_time: iso(-86400 * 5) },
  { name: 'favicon.ico', path: 'public/favicon.ico', is_dir: false, size: 15086, mod_time: iso(-86400 * 5) },
  { name: 'package.json', path: 'package.json', is_dir: false, size: 1240, mod_time: iso(-86400) },
  { name: 'tsconfig.json', path: 'tsconfig.json', is_dir: false, size: 680, mod_time: iso(-86400 * 3) },
  { name: 'vite.config.ts', path: 'vite.config.ts', is_dir: false, size: 920, mod_time: iso(-86400 * 2) },
  { name: 'README.md', path: 'README.md', is_dir: false, size: 2400, mod_time: iso(-86400 * 10) },
];

// ---------- Workspace file contents ----------

/**
 * Baseline (working-tree) contents keyed by relative path. Mutable: direct
 * writes and promotes update entries in-place for the page session.
 */
export const MOCK_FILE_CONTENTS: Record<string, string> = {
  'src/components/Dashboard.tsx': `import { useMemo } from 'react';
import { StatCard } from './StatCard';
import { useClusters } from '../utils/hooks';
import { formatBytes } from '../utils/format';

export function Dashboard() {
  const { clusters, loading } = useClusters();
  const total = useMemo(
    () => clusters.reduce((sum, c) => sum + c.nodeCount, 0),
    [clusters],
  );

  if (loading) return <p>加载中…</p>;

  return (
    <section className="dashboard">
      <h1>星云集群总览</h1>
      <StatCard label="节点总数" value={total} />
      <StatCard label="集群数量" value={clusters.length} />
      <ul>
        {clusters.map((c) => (
          <li key={c.id}>
            {c.name} — {formatBytes(c.memoryBytes)}
          </li>
        ))}
      </ul>
    </section>
  );
}
`,
  'src/components/Sidebar.tsx': `import { NavLink } from 'react-router-dom';

const links = [
  { to: '/', label: '总览' },
  { to: '/clusters', label: '集群' },
  { to: '/scaling', label: '扩缩容' },
  { to: '/settings', label: '设置' },
];

export function Sidebar() {
  return (
    <nav className="sidebar">
      {links.map((l) => (
        <NavLink key={l.to} to={l.to}>
          {l.label}
        </NavLink>
      ))}
    </nav>
  );
}
`,
  'src/components/Header.tsx': `interface HeaderProps {
  title: string;
  onRefresh?: () => void;
}

export function Header({ title, onRefresh }: HeaderProps) {
  return (
    <header className="app-header">
      <h2>{title}</h2>
      {onRefresh && (
        <button type="button" onClick={onRefresh}>
          刷新
        </button>
      )}
    </header>
  );
}
`,
  'src/components/StatusBar.tsx': `export function StatusBar({ connected }: { connected: boolean }) {
  return (
    <footer className="status-bar">
      <span className={connected ? 'dot ok' : 'dot bad'} />
      {connected ? '已连接' : '离线'}
    </footer>
  );
}
`,
  'src/utils/api.ts': `const BASE = import.meta.env.VITE_API_BASE ?? 'http://localhost:9000';

export async function apiGet<T>(path: string): Promise<T> {
  const resp = await fetch(BASE + path);
  if (!resp.ok) throw new Error('HTTP ' + resp.status);
  return resp.json() as Promise<T>;
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const resp = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!resp.ok) throw new Error('HTTP ' + resp.status);
  return resp.json() as Promise<T>;
}
`,
  'src/utils/format.ts': `export function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let i = -1;
  do {
    value /= 1024;
    i++;
  } while (value >= 1024 && i < units.length - 1);
  return value.toFixed(1) + ' ' + units[i];
}

export function formatPercent(ratio: number): string {
  return (ratio * 100).toFixed(1) + '%';
}
`,
  'src/utils/hooks.ts': `import { useEffect, useState } from 'react';
import { apiGet } from './api';
import type { Cluster } from '../types/models';

export function useClusters() {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    apiGet<Cluster[]>('/api/clusters')
      .then((data) => {
        if (!cancelled) setClusters(data);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return { clusters, loading };
}
`,
  'src/types/index.ts': `export * from './models';
`,
  'src/types/models.ts': `export interface Cluster {
  id: string;
  name: string;
  nodeCount: number;
  memoryBytes: number;
  status: 'healthy' | 'degraded' | 'offline';
}

export interface ScalingPolicy {
  clusterId: string;
  minNodes: number;
  maxNodes: number;
  targetCpu: number;
}
`,
  'src/main.tsx': `import { createRoot } from 'react-dom/client';
import { App } from './App';
import './index.css';

createRoot(document.getElementById('root')!).render(<App />);
`,
  'src/App.tsx': `import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { Dashboard } from './components/Dashboard';
import { Sidebar } from './components/Sidebar';
import { Header } from './components/Header';
import { StatusBar } from './components/StatusBar';

export function App() {
  return (
    <BrowserRouter>
      <div className="layout">
        <Sidebar />
        <main>
          <Header title="Nebula Console" />
          <Routes>
            <Route path="/" element={<Dashboard />} />
          </Routes>
          <StatusBar connected />
        </main>
      </div>
    </BrowserRouter>
  );
}
`,
  'src/index.css': `:root {
  color-scheme: light dark;
  font-family: system-ui, sans-serif;
}

body {
  margin: 0;
}
`,
  'package.json': `{
  "name": "nebula-console",
  "private": true,
  "version": "0.3.0",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "test": "vitest run",
    "lint": "eslint src"
  }
}
`,
  'tsconfig.json': `{
  "compilerOptions": {
    "target": "ES2022",
    "jsx": "react-jsx",
    "strict": true,
    "moduleResolution": "bundler"
  },
  "include": ["src"]
}
`,
  'vite.config.ts': `import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: { port: 5180 },
});
`,
  'README.md': `# Nebula Console

下一代星云管理控制台。

## 开发

\`\`\`bash
pnpm install
pnpm dev
\`\`\`
`,
};

/**
 * Shadow staging area (.codeflow/staging) — mutable map path → staged copy.
 * Pre-seeded with two edits so the 暂存区 / diff 视图 has data out of the box.
 */
export const MOCK_STAGED: Record<string, { content: string; mod_time: string }> = {
  'src/components/Dashboard.tsx': {
    content: `import { useMemo } from 'react';
import { StatCard } from './StatCard';
import { useClusters } from '../utils/hooks';
import { formatBytes, formatPercent } from '../utils/format';

export function Dashboard() {
  const { clusters, loading, error } = useClusters();
  const total = useMemo(
    () => clusters.reduce((sum, c) => sum + c.nodeCount, 0),
    [clusters],
  );
  const healthy = clusters.filter((c) => c.status === 'healthy').length;

  if (loading) return <p>加载中…</p>;
  if (error) return <p className="error">加载失败：{error.message}</p>;

  return (
    <section className="dashboard">
      <h1>星云集群总览</h1>
      <StatCard label="节点总数" value={total} />
      <StatCard label="集群数量" value={clusters.length} />
      <StatCard label="健康比例" value={formatPercent(healthy / clusters.length)} />
      <ul>
        {clusters.map((c) => (
          <li key={c.id} data-status={c.status}>
            {c.name} — {formatBytes(c.memoryBytes)}
          </li>
        ))}
      </ul>
    </section>
  );
}
`,
    mod_time: iso(-600),
  },
  'src/utils/format.ts': {
    content: `export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes < 1024) return bytes + ' B';
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let i = -1;
  do {
    value /= 1024;
    i++;
  } while (value >= 1024 && i < units.length - 1);
  return value.toFixed(1) + ' ' + units[i];
}

export function formatPercent(ratio: number): string {
  if (!Number.isFinite(ratio)) return '—';
  return (ratio * 100).toFixed(1) + '%';
}
`,
    mod_time: iso(-1200),
  },
};

// ---------- Workspace dev-server scripts ----------

export const MOCK_SCRIPTS: { name: string; command: string }[] = [
  { name: 'dev', command: 'vite' },
  { name: 'build', command: 'tsc -b && vite build' },
  { name: 'test', command: 'vitest run' },
  { name: 'lint', command: 'eslint src' },
];

// ---------- Guard ----------

export const MOCK_GUARD_RULES: GuardRule[] = [
  { id: 'deprecated_path', severity: 'error' },
  { id: 'max_file_size', severity: 'error' },
  { id: 'no_test_skip', severity: 'warn' },
  { id: 'no_console_log', severity: 'warn' },
  { id: 'require_type_annotation', severity: 'warn' },
];

export const MOCK_GUARD_META = {
  denied_path_globs: ['node_modules/**', '.env*', '**/*.secret'],
  max_file_bytes: 524288,
};

/** Pending guard exemption requests for the approval flow demo (mutable). */
export const MOCK_EXEMPTION_REQUESTS: ExemptionRequestRecord[] = [
  {
    id: 'exreq-001',
    path: 'src/utils/api.ts',
    rule_id: 'no_console_log',
    reason: '需要临时保留调试日志以定位扩缩容抖动问题，预计两天内移除。',
    requester: 'builtin-code-artisan',
    status: 'pending',
    created_at: iso(-5400),
  },
  {
    id: 'exreq-002',
    path: 'src/components/Dashboard.tsx',
    rule_id: 'max_file_size',
    reason: '仪表盘聚合视图重构中间态超出单文件上限，拆分将在下一任务完成。',
    requester: 'builtin-flow-conductor',
    status: 'pending',
    created_at: iso(-12600),
  },
];

// ---------- Agents ----------

/** Registry builtin agents (mirrors backend/internal/agent/builtins.go). */
export const MOCK_AGENTS: AgentInfo[] = [
  {
    id: 'builtin-flow-conductor',
    name: 'Flow Conductor',
    avatar: '🎯',
    description: '统筹阶段生命周期：Gate 评估、专家协作与辩论升级。',
    role_base: 'main',
    stage_tags: ['planning', 'coding', 'review', 'submit'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-code-artisan',
    name: 'Code Artisan',
    avatar: '🛠️',
    description: '在守卫约束下编写最小聚焦 Diff 的实现代码。',
    role_base: 'coder',
    stage_tags: ['coding'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-scout',
    name: 'Scout',
    avatar: '🔍',
    description: '快速检索与探索，只读不改，返回结构化发现。',
    role_base: 'sub',
    stage_tags: ['research', 'comprehension', 'planning'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-red-critic',
    name: 'Red Critic',
    avatar: '🛡️',
    description: '对抗式评审：正确性、安全与性能问题猎手。',
    role_base: 'critic',
    stage_tags: ['review'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-deep-researcher',
    name: 'Deep Researcher',
    avatar: '📚',
    description: '多源调研综合，以证据与引用为先。',
    role_base: 'researcher',
    stage_tags: ['research'],
    source: 'builtin',
    enabled: true,
  },
];

// ---------- Global config ----------

export const MOCK_GLOBAL_CONFIG = {
  default_model: 'claude-sonnet-5',
  api_pool: [],
  public_mcp: [],
};
