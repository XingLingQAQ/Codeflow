import type { Project } from '../../types';
import type { Flow, Stage, FlowTemplateInfo } from '../services-bridge/flows';
import type { WorkspaceEntry } from '../services-bridge/workspace';
import type { GuardRule } from '../services-bridge/guard';

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
