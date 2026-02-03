/**
 * Plan Mode - 类型定义
 * 整合 OpenSpec 约束驱动和 Aha-Loop 愿景构建
 */

/**
 * Plan 模式阶段
 */
export type PlanPhase =
  | 'vision'      // 愿景构建
  | 'constraints' // 约束提取
  | 'proposal'    // 提案生成
  | 'specs'       // 规格生成
  | 'design'      // 设计生成
  | 'tasks'       // 任务生成
  | 'execute'     // 执行阶段
  | 'completed';  // 完成

/**
 * Plan 模式状态
 */
export type PlanStatus = 'idle' | 'running' | 'paused' | 'completed' | 'failed';

/**
 * 愿景问题类型
 */
export interface VisionQuestion {
  id: string;
  category: 'goal' | 'scope' | 'constraint' | 'priority' | 'risk';
  question: string;
  required: boolean;
  followUp?: string[];
}

/**
 * 愿景回答
 */
export interface VisionAnswer {
  questionId: string;
  answer: string;
  timestamp: number;
}

/**
 * 愿景文档
 */
export interface VisionDocument {
  id: string;
  title: string;
  summary: string;
  goals: string[];
  scope: {
    included: string[];
    excluded: string[];
  };
  constraints: string[];
  priorities: string[];
  risks: string[];
  answers: VisionAnswer[];
  createdAt: number;
  updatedAt: number;
}

/**
 * 约束类型
 */
export type ConstraintType =
  | 'functional'    // 功能约束
  | 'technical'     // 技术约束
  | 'performance'   // 性能约束
  | 'security'      // 安全约束
  | 'compatibility' // 兼容性约束
  | 'resource';     // 资源约束

/**
 * 约束优先级
 */
export type ConstraintPriority = 'must' | 'should' | 'could' | 'wont';

/**
 * 约束定义
 */
export interface Constraint {
  id: string;
  type: ConstraintType;
  priority: ConstraintPriority;
  description: string;
  rationale?: string;
  source?: string;
  verifiable: boolean;
  verificationCriteria?: string;
}

/**
 * 约束集
 */
export interface ConstraintSet {
  id: string;
  visionId: string;
  constraints: Constraint[];
  createdAt: number;
  updatedAt: number;
}

/**
 * 工件类型
 */
export type ArtifactType = 'proposal' | 'spec' | 'design' | 'tasks' | 'architecture' | 'roadmap';

/**
 * 工件状态
 */
export type ArtifactStatus = 'draft' | 'review' | 'approved' | 'rejected';

/**
 * 工件元数据
 */
export interface ArtifactMetadata {
  id: string;
  type: ArtifactType;
  title: string;
  version: number;
  status: ArtifactStatus;
  path: string;
  createdAt: number;
  updatedAt: number;
  author?: string;
  reviewers?: string[];
}

/**
 * Proposal 工件
 */
export interface ProposalArtifact extends ArtifactMetadata {
  type: 'proposal';
  content: {
    why: string;
    what: string;
    how: string;
    impact: string;
    risks: string[];
    alternatives: string[];
  };
}

/**
 * Spec 工件
 */
export interface SpecArtifact extends ArtifactMetadata {
  type: 'spec';
  content: {
    requirement: string;
    scenarios: SpecScenario[];
    acceptanceCriteria: string[];
  };
}

/**
 * Spec 场景
 */
export interface SpecScenario {
  id: string;
  name: string;
  given: string;
  when: string;
  then: string;
}

/**
 * Design 工件
 */
export interface DesignArtifact extends ArtifactMetadata {
  type: 'design';
  content: {
    overview: string;
    components: DesignComponent[];
    interfaces: DesignInterface[];
    dataFlow: string;
    dependencies: string[];
  };
}

/**
 * 设计组件
 */
export interface DesignComponent {
  name: string;
  responsibility: string;
  dependencies: string[];
}

/**
 * 设计接口
 */
export interface DesignInterface {
  name: string;
  methods: string[];
  description: string;
}

/**
 * Tasks 工件
 */
export interface TasksArtifact extends ArtifactMetadata {
  type: 'tasks';
  content: {
    tasks: PlanTask[];
    dependencies: TaskDependency[];
    estimatedDuration: number;
  };
}

/**
 * 计划任务
 */
export interface PlanTask {
  id: string;
  title: string;
  description: string;
  priority: 'high' | 'medium' | 'low';
  status: 'pending' | 'in_progress' | 'completed' | 'blocked';
  assignee?: string;
  estimatedHours?: number;
  files?: string[];
}

/**
 * 任务依赖
 */
export interface TaskDependency {
  taskId: string;
  dependsOn: string[];
}

/**
 * Plan 会话
 */
export interface PlanSession {
  id: string;
  name: string;
  status: PlanStatus;
  currentPhase: PlanPhase;
  vision?: VisionDocument;
  constraints?: ConstraintSet;
  artifacts: ArtifactMetadata[];
  config: PlanConfig;
  createdAt: number;
  updatedAt: number;
  completedAt?: number;
}

/**
 * Plan 配置
 */
export interface PlanConfig {
  outputDir: string;
  visionModel?: string;
  constraintModel?: string;
  artifactModel?: string;
  autoAdvance: boolean;
  requireApproval: boolean;
  language: 'en' | 'zh';
}

/**
 * Architecture 工件
 */
export interface ArchitectureArtifact extends ArtifactMetadata {
  type: 'architecture';
  content: {
    overview: string;
    techStack: TechStackDecision[];
    systemComponents: SystemComponent[];
    dataArchitecture: DataArchitecture;
    integrations: Integration[];
    securityArchitecture: SecurityArchitecture;
    scalabilityPlan: ScalabilityPlan;
  };
}

/**
 * 技术栈决策
 */
export interface TechStackDecision {
  category: 'language' | 'framework' | 'database' | 'infrastructure' | 'tool';
  name: string;
  version?: string;
  rationale: string;
  alternatives: string[];
  tradeoffs: string[];
}

/**
 * 系统组件
 */
export interface SystemComponent {
  name: string;
  type: 'service' | 'library' | 'module' | 'external';
  responsibility: string;
  interfaces: string[];
  dependencies: string[];
  techStack: string[];
}

/**
 * 数据架构
 */
export interface DataArchitecture {
  dataStores: DataStore[];
  dataFlows: DataFlow[];
  caching: CachingStrategy;
}

/**
 * 数据存储
 */
export interface DataStore {
  name: string;
  type: 'relational' | 'document' | 'key-value' | 'graph' | 'file';
  technology: string;
  purpose: string;
}

/**
 * 数据流
 */
export interface DataFlow {
  from: string;
  to: string;
  dataType: string;
  protocol: string;
}

/**
 * 缓存策略
 */
export interface CachingStrategy {
  enabled: boolean;
  layers: string[];
  invalidation: string;
}

/**
 * 集成
 */
export interface Integration {
  name: string;
  type: 'api' | 'webhook' | 'message-queue' | 'file';
  direction: 'inbound' | 'outbound' | 'bidirectional';
  protocol: string;
}

/**
 * 安全架构
 */
export interface SecurityArchitecture {
  authentication: string;
  authorization: string;
  encryption: string[];
  compliance: string[];
}

/**
 * 可扩展性计划
 */
export interface ScalabilityPlan {
  horizontalScaling: boolean;
  verticalScaling: boolean;
  loadBalancing: string;
  bottlenecks: string[];
  mitigations: string[];
}

/**
 * Roadmap 工件
 */
export interface RoadmapArtifact extends ArtifactMetadata {
  type: 'roadmap';
  content: {
    milestones: Milestone[];
    prdQueue: PRDItem[];
    timeline: TimelineEntry[];
    dependencies: RoadmapDependency[];
  };
}

/**
 * 里程碑
 */
export interface Milestone {
  id: string;
  name: string;
  description: string;
  targetDate?: string;
  deliverables: string[];
  status: 'planned' | 'in_progress' | 'completed' | 'delayed';
  priority: 'critical' | 'high' | 'medium' | 'low';
}

/**
 * PRD 项
 */
export interface PRDItem {
  id: string;
  title: string;
  description: string;
  milestoneId: string;
  priority: number;
  estimatedEffort: 'small' | 'medium' | 'large' | 'xlarge';
  status: 'backlog' | 'ready' | 'in_progress' | 'done';
  acceptanceCriteria: string[];
}

/**
 * 时间线条目
 */
export interface TimelineEntry {
  milestoneId: string;
  startDate: string;
  endDate: string;
  phase: string;
}

/**
 * Roadmap 依赖
 */
export interface RoadmapDependency {
  itemId: string;
  dependsOn: string[];
  type: 'blocks' | 'requires' | 'enhances';
}

/**
 * 技术研究结果
 */
export interface TechResearchResult {
  id: string;
  topic: string;
  query: string;
  findings: ResearchFinding[];
  recommendations: string[];
  sources: string[];
  timestamp: number;
}

/**
 * 研究发现
 */
export interface ResearchFinding {
  title: string;
  summary: string;
  pros: string[];
  cons: string[];
  relevance: 'high' | 'medium' | 'low';
}

/**
 * Plan 事件
 */
export type PlanEvent =
  | { type: 'phase:start'; phase: PlanPhase; sessionId: string }
  | { type: 'phase:complete'; phase: PlanPhase; sessionId: string }
  | { type: 'vision:question'; question: VisionQuestion }
  | { type: 'vision:answer'; answer: VisionAnswer }
  | { type: 'vision:complete'; vision: VisionDocument }
  | { type: 'constraints:extracted'; constraints: ConstraintSet }
  | { type: 'artifact:created'; artifact: ArtifactMetadata }
  | { type: 'artifact:updated'; artifact: ArtifactMetadata }
  | { type: 'architecture:created'; architecture: ArchitectureArtifact }
  | { type: 'roadmap:created'; roadmap: RoadmapArtifact }
  | { type: 'research:complete'; result: TechResearchResult }
  | { type: 'session:complete'; session: PlanSession }
  | { type: 'session:error'; sessionId: string; error: string };

/**
 * Plan 事件监听器
 */
export type PlanEventListener = (event: PlanEvent) => void;

/**
 * VisionBuilder 配置
 */
export interface VisionBuilderConfig {
  maxQuestions: number;
  requireAllCategories: boolean;
  language: 'en' | 'zh';
}

/**
 * ConstraintExtractor 配置
 */
export interface ConstraintExtractorConfig {
  minConstraints: number;
  maxConstraints: number;
  requireVerifiable: boolean;
}

/**
 * ArtifactManager 配置
 */
export interface ArtifactManagerConfig {
  outputDir: string;
  versionControl: boolean;
  autoBackup: boolean;
}
