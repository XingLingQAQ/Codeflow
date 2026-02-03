/**
 * ArchitectPhase - 架构设计阶段
 * 实现 Aha-Loop 的 Architect 阶段，支持技术选型和架构设计
 */

import { EventEmitter } from 'events';
import * as path from 'path';
import * as fs from 'fs';
import {
  VisionDocument,
  ConstraintSet,
  ArchitectureArtifact,
  RoadmapArtifact,
  TechStackDecision,
  SystemComponent,
  DataArchitecture,
  Integration,
  SecurityArchitecture,
  ScalabilityPlan,
  Milestone,
  PRDItem,
  TimelineEntry,
  RoadmapDependency,
  TechResearchResult,
  ResearchFinding,
} from './types.js';

/**
 * AI 回调类型 - 技术研究
 */
export type TechResearchCallback = (
  topic: string,
  context: string
) => Promise<TechResearchResult>;

/**
 * AI 回调类型 - 架构设计
 */
export type ArchitectureDesignCallback = (
  vision: VisionDocument,
  constraints: ConstraintSet,
  research: TechResearchResult[]
) => Promise<ArchitectureArtifact['content']>;

/**
 * AI 回调类型 - Roadmap 生成
 */
export type RoadmapGenerateCallback = (
  vision: VisionDocument,
  architecture: ArchitectureArtifact
) => Promise<RoadmapArtifact['content']>;

/**
 * TechResearcher 配置
 */
export interface TechResearcherConfig {
  maxTopics: number;
  cacheResults: boolean;
  modelId?: string;
}

/**
 * ArchitectureDesigner 配置
 */
export interface ArchitectureDesignerConfig {
  includeSecurityAnalysis: boolean;
  includeScalabilityPlan: boolean;
  modelId?: string;
}

/**
 * RoadmapGenerator 配置
 */
export interface RoadmapGeneratorConfig {
  defaultMilestoneCount: number;
  estimateTimeline: boolean;
  modelId?: string;
}

/**
 * ArchitectPhase 配置
 */
export interface ArchitectPhaseConfig {
  outputDir: string;
  researcher: TechResearcherConfig;
  designer: ArchitectureDesignerConfig;
  roadmap: RoadmapGeneratorConfig;
}

const DEFAULT_CONFIG: ArchitectPhaseConfig = {
  outputDir: '.codeflow/changes',
  researcher: {
    maxTopics: 5,
    cacheResults: true,
  },
  designer: {
    includeSecurityAnalysis: true,
    includeScalabilityPlan: true,
  },
  roadmap: {
    defaultMilestoneCount: 3,
    estimateTimeline: true,
  },
};

/**
 * TechResearcher - 技术研究器
 */
export class TechResearcher extends EventEmitter {
  private config: TechResearcherConfig;
  private cache: Map<string, TechResearchResult> = new Map();
  private aiCallback?: TechResearchCallback;

  constructor(config: Partial<TechResearcherConfig> = {}, aiCallback?: TechResearchCallback) {
    super();
    this.config = { ...DEFAULT_CONFIG.researcher, ...config };
    this.aiCallback = aiCallback;
  }

  /**
   * 研究技术主题
   */
  async research(topic: string, context: string): Promise<TechResearchResult> {
    // 检查缓存
    const cacheKey = `${topic}:${context}`;
    if (this.config.cacheResults && this.cache.has(cacheKey)) {
      const cached = this.cache.get(cacheKey)!;
      this.emit('research:cached', cached);
      return cached;
    }

    this.emit('research:start', { topic, context });

    let result: TechResearchResult;

    if (this.aiCallback) {
      result = await this.aiCallback(topic, context);
    } else {
      result = this.generateStaticResearch(topic, context);
    }

    // 缓存结果
    if (this.config.cacheResults) {
      this.cache.set(cacheKey, result);
    }

    this.emit('research:complete', result);
    return result;
  }

  /**
   * 批量研究
   */
  async researchBatch(topics: Array<{ topic: string; context: string }>): Promise<TechResearchResult[]> {
    const limitedTopics = topics.slice(0, this.config.maxTopics);
    const results: TechResearchResult[] = [];

    for (const { topic, context } of limitedTopics) {
      const result = await this.research(topic, context);
      results.push(result);
    }

    return results;
  }

  /**
   * 从愿景提取研究主题
   */
  extractTopicsFromVision(vision: VisionDocument): Array<{ topic: string; context: string }> {
    const topics: Array<{ topic: string; context: string }> = [];

    // 从目标提取
    for (const goal of vision.goals) {
      if (this.containsTechKeyword(goal)) {
        topics.push({
          topic: this.extractTechTopic(goal),
          context: `Goal: ${goal}`,
        });
      }
    }

    // 从约束提取
    for (const constraint of vision.constraints) {
      if (this.containsTechKeyword(constraint)) {
        topics.push({
          topic: this.extractTechTopic(constraint),
          context: `Constraint: ${constraint}`,
        });
      }
    }

    return topics.slice(0, this.config.maxTopics);
  }

  private containsTechKeyword(text: string): boolean {
    const keywords = [
      'database', 'api', 'framework', 'library', 'cloud', 'server',
      'frontend', 'backend', 'authentication', 'cache', 'queue',
      'storage', 'deployment', 'container', 'microservice',
    ];
    const lower = text.toLowerCase();
    return keywords.some(k => lower.includes(k));
  }

  private extractTechTopic(text: string): string {
    const techPatterns = [
      /(?:use|implement|integrate|build)\s+(\w+(?:\s+\w+)?)/i,
      /(\w+)\s+(?:database|api|framework|service)/i,
      /(?:database|api|framework|service)\s+(\w+)/i,
    ];

    for (const pattern of techPatterns) {
      const match = text.match(pattern);
      if (match) return match[1];
    }

    return text.split(' ').slice(0, 3).join(' ');
  }

  private generateStaticResearch(topic: string, context: string): TechResearchResult {
    return {
      id: `research-${Date.now()}`,
      topic,
      query: context,
      findings: [
        {
          title: `${topic} Overview`,
          summary: `Research findings for ${topic} based on context: ${context}`,
          pros: ['Well-documented', 'Active community', 'Production-ready'],
          cons: ['Learning curve', 'Configuration complexity'],
          relevance: 'high',
        },
      ],
      recommendations: [
        `Consider ${topic} for the implementation`,
        'Evaluate alternatives before final decision',
      ],
      sources: ['Documentation', 'Community forums', 'Best practices guides'],
      timestamp: Date.now(),
    };
  }

  /**
   * 清除缓存
   */
  clearCache(): void {
    this.cache.clear();
    this.emit('cache:cleared');
  }

  /**
   * 获取缓存的研究结果
   */
  getCachedResults(): TechResearchResult[] {
    return Array.from(this.cache.values());
  }
}

/**
 * ArchitectureDesigner - 架构设计器
 */
export class ArchitectureDesigner extends EventEmitter {
  private config: ArchitectureDesignerConfig;
  private aiCallback?: ArchitectureDesignCallback;

  constructor(config: Partial<ArchitectureDesignerConfig> = {}, aiCallback?: ArchitectureDesignCallback) {
    super();
    this.config = { ...DEFAULT_CONFIG.designer, ...config };
    this.aiCallback = aiCallback;
  }

  /**
   * 设计架构
   */
  async design(
    vision: VisionDocument,
    constraints: ConstraintSet,
    research: TechResearchResult[]
  ): Promise<ArchitectureArtifact> {
    this.emit('design:start', { visionId: vision.id });

    let content: ArchitectureArtifact['content'];

    if (this.aiCallback) {
      content = await this.aiCallback(vision, constraints, research);
    } else {
      content = this.generateStaticArchitecture(vision, constraints, research);
    }

    const artifact: ArchitectureArtifact = {
      id: `arch-${Date.now()}`,
      type: 'architecture',
      title: `Architecture for ${vision.title}`,
      version: 1,
      status: 'draft',
      path: '',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      content,
    };

    this.emit('design:complete', artifact);
    return artifact;
  }

  private generateStaticArchitecture(
    vision: VisionDocument,
    constraints: ConstraintSet,
    research: TechResearchResult[]
  ): ArchitectureArtifact['content'] {
    // 从研究结果提取技术栈
    const techStack: TechStackDecision[] = research.map(r => ({
      category: 'framework' as const,
      name: r.topic,
      rationale: r.recommendations[0] || 'Based on research findings',
      alternatives: r.findings.map(f => f.title),
      tradeoffs: r.findings.flatMap(f => f.cons),
    }));

    // 从愿景生成组件
    const systemComponents: SystemComponent[] = vision.goals.map((goal, i) => ({
      name: `Component${i + 1}`,
      type: 'module' as const,
      responsibility: goal,
      interfaces: [],
      dependencies: [],
      techStack: techStack.map(t => t.name),
    }));

    // 数据架构
    const dataArchitecture: DataArchitecture = {
      dataStores: [
        {
          name: 'PrimaryDB',
          type: 'relational',
          technology: 'PostgreSQL',
          purpose: 'Main data storage',
        },
      ],
      dataFlows: [],
      caching: {
        enabled: true,
        layers: ['Application', 'Database'],
        invalidation: 'TTL-based',
      },
    };

    // 集成
    const integrations: Integration[] = [];

    // 安全架构
    const securityArchitecture: SecurityArchitecture = this.config.includeSecurityAnalysis
      ? {
          authentication: 'JWT-based',
          authorization: 'RBAC',
          encryption: ['TLS 1.3', 'AES-256'],
          compliance: [],
        }
      : {
          authentication: 'TBD',
          authorization: 'TBD',
          encryption: [],
          compliance: [],
        };

    // 可扩展性计划
    const scalabilityPlan: ScalabilityPlan = this.config.includeScalabilityPlan
      ? {
          horizontalScaling: true,
          verticalScaling: true,
          loadBalancing: 'Round-robin',
          bottlenecks: ['Database connections', 'API rate limits'],
          mitigations: ['Connection pooling', 'Caching', 'Queue-based processing'],
        }
      : {
          horizontalScaling: false,
          verticalScaling: false,
          loadBalancing: 'None',
          bottlenecks: [],
          mitigations: [],
        };

    return {
      overview: `Architecture design for: ${vision.title}\n\nGoals:\n${vision.goals.map(g => `- ${g}`).join('\n')}`,
      techStack,
      systemComponents,
      dataArchitecture,
      integrations,
      securityArchitecture,
      scalabilityPlan,
    };
  }

  /**
   * 生成架构 Markdown
   */
  generateMarkdown(architecture: ArchitectureArtifact): string {
    const { content } = architecture;
    const lines: string[] = [
      `# ${architecture.title}`,
      '',
      '## Overview',
      content.overview,
      '',
      '## Tech Stack',
      '',
    ];

    for (const tech of content.techStack) {
      lines.push(`### ${tech.name}`);
      lines.push(`- **Category**: ${tech.category}`);
      if (tech.version) lines.push(`- **Version**: ${tech.version}`);
      lines.push(`- **Rationale**: ${tech.rationale}`);
      if (tech.alternatives.length > 0) {
        lines.push(`- **Alternatives**: ${tech.alternatives.join(', ')}`);
      }
      if (tech.tradeoffs.length > 0) {
        lines.push(`- **Tradeoffs**: ${tech.tradeoffs.join(', ')}`);
      }
      lines.push('');
    }

    lines.push('## System Components', '');
    for (const comp of content.systemComponents) {
      lines.push(`### ${comp.name}`);
      lines.push(`- **Type**: ${comp.type}`);
      lines.push(`- **Responsibility**: ${comp.responsibility}`);
      if (comp.dependencies.length > 0) {
        lines.push(`- **Dependencies**: ${comp.dependencies.join(', ')}`);
      }
      lines.push('');
    }

    lines.push('## Data Architecture', '');
    lines.push('### Data Stores');
    for (const store of content.dataArchitecture.dataStores) {
      lines.push(`- **${store.name}** (${store.type}): ${store.technology} - ${store.purpose}`);
    }
    lines.push('');

    if (content.securityArchitecture.authentication !== 'TBD') {
      lines.push('## Security Architecture', '');
      lines.push(`- **Authentication**: ${content.securityArchitecture.authentication}`);
      lines.push(`- **Authorization**: ${content.securityArchitecture.authorization}`);
      lines.push(`- **Encryption**: ${content.securityArchitecture.encryption.join(', ')}`);
      lines.push('');
    }

    if (content.scalabilityPlan.horizontalScaling) {
      lines.push('## Scalability Plan', '');
      lines.push(`- **Horizontal Scaling**: ${content.scalabilityPlan.horizontalScaling ? 'Yes' : 'No'}`);
      lines.push(`- **Load Balancing**: ${content.scalabilityPlan.loadBalancing}`);
      if (content.scalabilityPlan.bottlenecks.length > 0) {
        lines.push(`- **Bottlenecks**: ${content.scalabilityPlan.bottlenecks.join(', ')}`);
      }
      if (content.scalabilityPlan.mitigations.length > 0) {
        lines.push(`- **Mitigations**: ${content.scalabilityPlan.mitigations.join(', ')}`);
      }
      lines.push('');
    }

    return lines.join('\n');
  }
}

/**
 * RoadmapGenerator - 里程碑规划器
 */
export class RoadmapGenerator extends EventEmitter {
  private config: RoadmapGeneratorConfig;
  private aiCallback?: RoadmapGenerateCallback;

  constructor(config: Partial<RoadmapGeneratorConfig> = {}, aiCallback?: RoadmapGenerateCallback) {
    super();
    this.config = { ...DEFAULT_CONFIG.roadmap, ...config };
    this.aiCallback = aiCallback;
  }

  /**
   * 生成 Roadmap
   */
  async generate(
    vision: VisionDocument,
    architecture: ArchitectureArtifact
  ): Promise<RoadmapArtifact> {
    this.emit('roadmap:start', { visionId: vision.id });

    let content: RoadmapArtifact['content'];

    if (this.aiCallback) {
      content = await this.aiCallback(vision, architecture);
    } else {
      content = this.generateStaticRoadmap(vision, architecture);
    }

    const artifact: RoadmapArtifact = {
      id: `roadmap-${Date.now()}`,
      type: 'roadmap',
      title: `Roadmap for ${vision.title}`,
      version: 1,
      status: 'draft',
      path: '',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      content,
    };

    this.emit('roadmap:complete', artifact);
    return artifact;
  }

  private generateStaticRoadmap(
    vision: VisionDocument,
    architecture: ArchitectureArtifact
  ): RoadmapArtifact['content'] {
    const milestones: Milestone[] = [];
    const prdQueue: PRDItem[] = [];
    const timeline: TimelineEntry[] = [];
    const dependencies: RoadmapDependency[] = [];

    // 基于优先级生成里程碑
    const priorities = vision.priorities.length > 0 ? vision.priorities : vision.goals;
    const milestoneCount = Math.min(priorities.length, this.config.defaultMilestoneCount);

    for (let i = 0; i < milestoneCount; i++) {
      const milestone: Milestone = {
        id: `milestone-${i + 1}`,
        name: `Milestone ${i + 1}`,
        description: priorities[i] || `Phase ${i + 1} implementation`,
        deliverables: [priorities[i] || `Feature ${i + 1}`],
        status: 'planned',
        priority: i === 0 ? 'critical' : i === 1 ? 'high' : 'medium',
      };
      milestones.push(milestone);

      // 为每个里程碑生成 PRD
      const prd: PRDItem = {
        id: `prd-${i + 1}`,
        title: `PRD: ${milestone.name}`,
        description: milestone.description,
        milestoneId: milestone.id,
        priority: i + 1,
        estimatedEffort: i === 0 ? 'large' : 'medium',
        status: 'backlog',
        acceptanceCriteria: [`Complete ${milestone.deliverables[0]}`],
      };
      prdQueue.push(prd);

      // 生成时间线
      if (this.config.estimateTimeline) {
        const startOffset = i * 14; // 2 weeks per milestone
        const endOffset = startOffset + 14;
        const startDate = new Date();
        startDate.setDate(startDate.getDate() + startOffset);
        const endDate = new Date();
        endDate.setDate(endDate.getDate() + endOffset);

        timeline.push({
          milestoneId: milestone.id,
          startDate: startDate.toISOString().split('T')[0],
          endDate: endDate.toISOString().split('T')[0],
          phase: `Phase ${i + 1}`,
        });
      }

      // 生成依赖
      if (i > 0) {
        dependencies.push({
          itemId: milestone.id,
          dependsOn: [`milestone-${i}`],
          type: 'requires',
        });
      }
    }

    return {
      milestones,
      prdQueue,
      timeline,
      dependencies,
    };
  }

  /**
   * 生成 Roadmap JSON
   */
  generateJSON(roadmap: RoadmapArtifact): string {
    return JSON.stringify(roadmap.content, null, 2);
  }

  /**
   * 生成 Roadmap Markdown
   */
  generateMarkdown(roadmap: RoadmapArtifact): string {
    const { content } = roadmap;
    const lines: string[] = [
      `# ${roadmap.title}`,
      '',
      '## Milestones',
      '',
    ];

    for (const milestone of content.milestones) {
      lines.push(`### ${milestone.name}`);
      lines.push(`- **Priority**: ${milestone.priority}`);
      lines.push(`- **Status**: ${milestone.status}`);
      lines.push(`- **Description**: ${milestone.description}`);
      lines.push(`- **Deliverables**: ${milestone.deliverables.join(', ')}`);

      const timeline = content.timeline.find(t => t.milestoneId === milestone.id);
      if (timeline) {
        lines.push(`- **Timeline**: ${timeline.startDate} to ${timeline.endDate}`);
      }
      lines.push('');
    }

    lines.push('## PRD Queue', '');
    for (const prd of content.prdQueue) {
      lines.push(`### ${prd.title}`);
      lines.push(`- **Priority**: ${prd.priority}`);
      lines.push(`- **Effort**: ${prd.estimatedEffort}`);
      lines.push(`- **Status**: ${prd.status}`);
      lines.push(`- **Acceptance Criteria**:`);
      for (const criteria of prd.acceptanceCriteria) {
        lines.push(`  - ${criteria}`);
      }
      lines.push('');
    }

    return lines.join('\n');
  }
}

/**
 * ArchitectPhase - 架构设计阶段编排器
 */
export class ArchitectPhase extends EventEmitter {
  private config: ArchitectPhaseConfig;
  private researcher: TechResearcher;
  private designer: ArchitectureDesigner;
  private roadmapGenerator: RoadmapGenerator;
  private researchResults: TechResearchResult[] = [];
  private architecture?: ArchitectureArtifact;
  private roadmap?: RoadmapArtifact;

  constructor(
    config: Partial<ArchitectPhaseConfig> = {},
    callbacks?: {
      research?: TechResearchCallback;
      design?: ArchitectureDesignCallback;
      roadmap?: RoadmapGenerateCallback;
    }
  ) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.researcher = new TechResearcher(this.config.researcher, callbacks?.research);
    this.designer = new ArchitectureDesigner(this.config.designer, callbacks?.design);
    this.roadmapGenerator = new RoadmapGenerator(this.config.roadmap, callbacks?.roadmap);

    // 转发事件
    this.researcher.on('research:complete', (result) => this.emit('research:complete', result));
    this.designer.on('design:complete', (arch) => this.emit('architecture:complete', arch));
    this.roadmapGenerator.on('roadmap:complete', (rm) => this.emit('roadmap:complete', rm));
  }

  /**
   * 执行架构设计阶段
   */
  async execute(
    vision: VisionDocument,
    constraints: ConstraintSet
  ): Promise<{
    research: TechResearchResult[];
    architecture: ArchitectureArtifact;
    roadmap: RoadmapArtifact;
  }> {
    this.emit('phase:start', { phase: 'architect' });

    // 1. 技术研究
    const topics = this.researcher.extractTopicsFromVision(vision);
    this.researchResults = await this.researcher.researchBatch(topics);

    // 2. 架构设计
    this.architecture = await this.designer.design(vision, constraints, this.researchResults);

    // 3. Roadmap 生成
    this.roadmap = await this.roadmapGenerator.generate(vision, this.architecture);

    // 4. 保存工件
    await this.saveArtifacts();

    this.emit('phase:complete', { phase: 'architect' });

    return {
      research: this.researchResults,
      architecture: this.architecture,
      roadmap: this.roadmap,
    };
  }

  /**
   * 保存工件到文件系统
   */
  private async saveArtifacts(): Promise<void> {
    const outputDir = this.config.outputDir;

    // 确保目录存在
    if (!fs.existsSync(outputDir)) {
      fs.mkdirSync(outputDir, { recursive: true });
    }

    // 保存架构文档
    if (this.architecture) {
      const archPath = path.join(outputDir, 'architecture.md');
      const archContent = this.designer.generateMarkdown(this.architecture);
      fs.writeFileSync(archPath, archContent, 'utf-8');
      this.architecture.path = archPath;
    }

    // 保存 Roadmap
    if (this.roadmap) {
      const roadmapJsonPath = path.join(outputDir, 'roadmap.json');
      const roadmapMdPath = path.join(outputDir, 'roadmap.md');
      fs.writeFileSync(roadmapJsonPath, this.roadmapGenerator.generateJSON(this.roadmap), 'utf-8');
      fs.writeFileSync(roadmapMdPath, this.roadmapGenerator.generateMarkdown(this.roadmap), 'utf-8');
      this.roadmap.path = roadmapJsonPath;
    }

    // 保存研究结果
    if (this.researchResults.length > 0) {
      const researchDir = path.join(outputDir, 'research');
      if (!fs.existsSync(researchDir)) {
        fs.mkdirSync(researchDir, { recursive: true });
      }

      for (const result of this.researchResults) {
        const researchPath = path.join(researchDir, `${result.id}.json`);
        fs.writeFileSync(researchPath, JSON.stringify(result, null, 2), 'utf-8');
      }
    }
  }

  /**
   * 获取研究结果
   */
  getResearchResults(): TechResearchResult[] {
    return this.researchResults;
  }

  /**
   * 获取架构
   */
  getArchitecture(): ArchitectureArtifact | undefined {
    return this.architecture;
  }

  /**
   * 获取 Roadmap
   */
  getRoadmap(): RoadmapArtifact | undefined {
    return this.roadmap;
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<ArchitectPhaseConfig>): void {
    this.config = { ...this.config, ...config };
  }

  /**
   * 获取配置
   */
  getConfig(): ArchitectPhaseConfig {
    return { ...this.config };
  }
}
