/**
 * PlanArtifactManager - 工件管理器
 * 管理 OpenSpec 风格的工件（proposal/specs/design/tasks）
 */

import { EventEmitter } from 'events';
import * as fs from 'fs';
import * as path from 'path';
import {
  ArtifactMetadata,
  ArtifactType,
  ArtifactStatus,
  ProposalArtifact,
  SpecArtifact,
  DesignArtifact,
  TasksArtifact,
  ArtifactManagerConfig,
  VisionDocument,
  ConstraintSet,
  PlanEvent,
} from './types.js';

/**
 * 默认配置
 */
const DEFAULT_CONFIG: ArtifactManagerConfig = {
  outputDir: '.codeflow/changes',
  versionControl: true,
  autoBackup: true,
};

/**
 * PlanArtifactManager - 工件管理器
 */
export class PlanArtifactManager extends EventEmitter {
  private config: ArtifactManagerConfig;
  private artifacts: Map<string, ArtifactMetadata> = new Map();
  private baseDir: string;

  constructor(baseDir: string, config: Partial<ArtifactManagerConfig> = {}) {
    super();
    this.baseDir = baseDir;
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 初始化工件目录
   */
  async initialize(): Promise<void> {
    const outputPath = this.getOutputPath();
    await fs.promises.mkdir(outputPath, { recursive: true });
    await fs.promises.mkdir(path.join(outputPath, 'specs'), { recursive: true });
    await fs.promises.mkdir(path.join(outputPath, 'backups'), { recursive: true });
  }

  /**
   * 获取输出路径
   */
  getOutputPath(): string {
    return path.join(this.baseDir, this.config.outputDir);
  }

  /**
   * 创建 Proposal 工件
   */
  async createProposal(
    vision: VisionDocument,
    constraints: ConstraintSet
  ): Promise<ProposalArtifact> {
    const id = `proposal-${Date.now()}`;
    const artifactPath = path.join(this.getOutputPath(), 'proposal.md');

    const proposal: ProposalArtifact = {
      id,
      type: 'proposal',
      title: vision.title,
      version: 1,
      status: 'draft',
      path: artifactPath,
      createdAt: Date.now(),
      updatedAt: Date.now(),
      content: {
        why: vision.summary,
        what: vision.goals.join('\n'),
        how: this.generateHowSection(vision, constraints),
        impact: this.generateImpactSection(vision),
        risks: vision.risks,
        alternatives: [],
      },
    };

    // 生成 Markdown 内容
    const markdown = this.generateProposalMarkdown(proposal);
    await this.writeArtifact(artifactPath, markdown);

    this.artifacts.set(id, proposal);
    this.emitEvent({ type: 'artifact:created', artifact: proposal });

    return proposal;
  }

  /**
   * 创建 Spec 工件
   */
  async createSpec(
    name: string,
    requirement: string,
    scenarios: { name: string; given: string; when: string; then: string }[]
  ): Promise<SpecArtifact> {
    const id = `spec-${Date.now()}`;
    const fileName = this.sanitizeFileName(name);
    const artifactPath = path.join(this.getOutputPath(), 'specs', `${fileName}.md`);

    const spec: SpecArtifact = {
      id,
      type: 'spec',
      title: name,
      version: 1,
      status: 'draft',
      path: artifactPath,
      createdAt: Date.now(),
      updatedAt: Date.now(),
      content: {
        requirement,
        scenarios: scenarios.map((s, i) => ({
          id: `scenario-${i + 1}`,
          ...s,
        })),
        acceptanceCriteria: scenarios.map(s => s.then),
      },
    };

    const markdown = this.generateSpecMarkdown(spec);
    await this.writeArtifact(artifactPath, markdown);

    this.artifacts.set(id, spec);
    this.emitEvent({ type: 'artifact:created', artifact: spec });

    return spec;
  }

  /**
   * 创建 Design 工件
   */
  async createDesign(
    title: string,
    overview: string,
    components: { name: string; responsibility: string; dependencies: string[] }[],
    interfaces: { name: string; methods: string[]; description: string }[]
  ): Promise<DesignArtifact> {
    const id = `design-${Date.now()}`;
    const artifactPath = path.join(this.getOutputPath(), 'design.md');

    const design: DesignArtifact = {
      id,
      type: 'design',
      title,
      version: 1,
      status: 'draft',
      path: artifactPath,
      createdAt: Date.now(),
      updatedAt: Date.now(),
      content: {
        overview,
        components,
        interfaces,
        dataFlow: '',
        dependencies: [],
      },
    };

    const markdown = this.generateDesignMarkdown(design);
    await this.writeArtifact(artifactPath, markdown);

    this.artifacts.set(id, design);
    this.emitEvent({ type: 'artifact:created', artifact: design });

    return design;
  }

  /**
   * 创建 Tasks 工件
   */
  async createTasks(
    title: string,
    tasks: { title: string; description: string; priority: 'high' | 'medium' | 'low'; files?: string[] }[],
    dependencies?: { taskIndex: number; dependsOn: number[] }[]
  ): Promise<TasksArtifact> {
    const id = `tasks-${Date.now()}`;
    const artifactPath = path.join(this.getOutputPath(), 'tasks.md');

    const planTasks = tasks.map((t, i) => ({
      id: `task-${i + 1}`,
      title: t.title,
      description: t.description,
      priority: t.priority,
      status: 'pending' as const,
      files: t.files,
    }));

    const taskDeps = (dependencies || []).map(d => ({
      taskId: `task-${d.taskIndex + 1}`,
      dependsOn: d.dependsOn.map(idx => `task-${idx + 1}`),
    }));

    const tasksArtifact: TasksArtifact = {
      id,
      type: 'tasks',
      title,
      version: 1,
      status: 'draft',
      path: artifactPath,
      createdAt: Date.now(),
      updatedAt: Date.now(),
      content: {
        tasks: planTasks,
        dependencies: taskDeps,
        estimatedDuration: tasks.length * 2, // 简单估算
      },
    };

    const markdown = this.generateTasksMarkdown(tasksArtifact);
    await this.writeArtifact(artifactPath, markdown);

    this.artifacts.set(id, tasksArtifact);
    this.emitEvent({ type: 'artifact:created', artifact: tasksArtifact });

    return tasksArtifact;
  }

  /**
   * 更新工件状态
   */
  async updateStatus(artifactId: string, status: ArtifactStatus): Promise<boolean> {
    const artifact = this.artifacts.get(artifactId);
    if (!artifact) return false;

    artifact.status = status;
    artifact.updatedAt = Date.now();

    this.emitEvent({ type: 'artifact:updated', artifact });
    return true;
  }

  /**
   * 获取工件
   */
  getArtifact(id: string): ArtifactMetadata | undefined {
    return this.artifacts.get(id);
  }

  /**
   * 获取所有工件
   */
  getAllArtifacts(): ArtifactMetadata[] {
    return Array.from(this.artifacts.values());
  }

  /**
   * 按类型获取工件
   */
  getArtifactsByType(type: ArtifactType): ArtifactMetadata[] {
    return Array.from(this.artifacts.values()).filter(a => a.type === type);
  }

  /**
   * 读取工件内容
   */
  async readArtifact(artifactId: string): Promise<string | null> {
    const artifact = this.artifacts.get(artifactId);
    if (!artifact) return null;

    try {
      return await fs.promises.readFile(artifact.path, 'utf-8');
    } catch {
      return null;
    }
  }

  /**
   * 写入工件
   */
  private async writeArtifact(filePath: string, content: string): Promise<void> {
    // 备份现有文件
    if (this.config.autoBackup && fs.existsSync(filePath)) {
      const backupPath = path.join(
        this.getOutputPath(),
        'backups',
        `${path.basename(filePath)}.${Date.now()}.bak`
      );
      await fs.promises.copyFile(filePath, backupPath);
    }

    await fs.promises.writeFile(filePath, content, 'utf-8');
  }

  /**
   * 生成 Proposal Markdown
   */
  private generateProposalMarkdown(proposal: ProposalArtifact): string {
    return `# ${proposal.title}

## Why

${proposal.content.why}

## What

${proposal.content.what}

## How

${proposal.content.how}

## Impact

${proposal.content.impact}

## Risks

${proposal.content.risks.map(r => `- ${r}`).join('\n')}

## Alternatives

${proposal.content.alternatives.length > 0 ? proposal.content.alternatives.map(a => `- ${a}`).join('\n') : '_None identified_'}

---
_Generated at: ${new Date(proposal.createdAt).toISOString()}_
_Status: ${proposal.status}_
`;
  }

  /**
   * 生成 Spec Markdown
   */
  private generateSpecMarkdown(spec: SpecArtifact): string {
    const scenarios = spec.content.scenarios.map(s => `
### ${s.name}

**Given:** ${s.given}

**When:** ${s.when}

**Then:** ${s.then}
`).join('\n');

    return `# ${spec.title}

## Requirement

${spec.content.requirement}

## Scenarios

${scenarios}

## Acceptance Criteria

${spec.content.acceptanceCriteria.map((c, i) => `${i + 1}. ${c}`).join('\n')}

---
_Generated at: ${new Date(spec.createdAt).toISOString()}_
_Status: ${spec.status}_
`;
  }

  /**
   * 生成 Design Markdown
   */
  private generateDesignMarkdown(design: DesignArtifact): string {
    const components = design.content.components.map(c => `
### ${c.name}

**Responsibility:** ${c.responsibility}

**Dependencies:** ${c.dependencies.length > 0 ? c.dependencies.join(', ') : 'None'}
`).join('\n');

    const interfaces = design.content.interfaces.map(i => `
### ${i.name}

${i.description}

**Methods:**
${i.methods.map(m => `- \`${m}\``).join('\n')}
`).join('\n');

    return `# ${design.title}

## Overview

${design.content.overview}

## Components

${components}

## Interfaces

${interfaces}

## Data Flow

${design.content.dataFlow || '_To be defined_'}

## Dependencies

${design.content.dependencies.length > 0 ? design.content.dependencies.map(d => `- ${d}`).join('\n') : '_None_'}

---
_Generated at: ${new Date(design.createdAt).toISOString()}_
_Status: ${design.status}_
`;
  }

  /**
   * 生成 Tasks Markdown
   */
  private generateTasksMarkdown(tasks: TasksArtifact): string {
    const taskList = tasks.content.tasks.map(t => {
      const deps = tasks.content.dependencies.find(d => d.taskId === t.id);
      const depsStr = deps && deps.dependsOn.length > 0
        ? `\n  - **Depends on:** ${deps.dependsOn.join(', ')}`
        : '';
      const filesStr = t.files && t.files.length > 0
        ? `\n  - **Files:** ${t.files.join(', ')}`
        : '';

      return `
### ${t.id}: ${t.title}

- **Priority:** ${t.priority}
- **Status:** ${t.status}${depsStr}${filesStr}

${t.description}
`;
    }).join('\n');

    return `# ${tasks.title}

## Summary

- **Total Tasks:** ${tasks.content.tasks.length}
- **Estimated Duration:** ${tasks.content.estimatedDuration} hours

## Tasks

${taskList}

---
_Generated at: ${new Date(tasks.createdAt).toISOString()}_
_Status: ${tasks.status}_
`;
  }

  /**
   * 生成 How 部分
   */
  private generateHowSection(vision: VisionDocument, constraints: ConstraintSet): string {
    const mustConstraints = constraints.constraints.filter(c => c.priority === 'must');
    const technicalConstraints = constraints.constraints.filter(c => c.type === 'technical');

    let how = 'Implementation approach:\n\n';

    if (mustConstraints.length > 0) {
      how += '**Must-have requirements:**\n';
      how += mustConstraints.map(c => `- ${c.description}`).join('\n');
      how += '\n\n';
    }

    if (technicalConstraints.length > 0) {
      how += '**Technical approach:**\n';
      how += technicalConstraints.map(c => `- ${c.description}`).join('\n');
    }

    return how;
  }

  /**
   * 生成 Impact 部分
   */
  private generateImpactSection(vision: VisionDocument): string {
    let impact = '';

    if (vision.scope.included.length > 0) {
      impact += '**Affected areas:**\n';
      impact += vision.scope.included.map(s => `- ${s}`).join('\n');
    }

    return impact || '_Impact analysis pending_';
  }

  /**
   * 清理文件名
   */
  private sanitizeFileName(name: string): string {
    return name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '');
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<ArtifactManagerConfig>): void {
    this.config = { ...this.config, ...config };
  }

  /**
   * 获取配置
   */
  getConfig(): ArtifactManagerConfig {
    return { ...this.config };
  }

  /**
   * 清理所有工件
   */
  async cleanup(): Promise<void> {
    this.artifacts.clear();
  }

  /**
   * 发送事件
   */
  private emitEvent(event: PlanEvent): void {
    this.emit('event', event);
  }
}
