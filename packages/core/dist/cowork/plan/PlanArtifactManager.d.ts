/**
 * PlanArtifactManager - 工件管理器
 * 管理 OpenSpec 风格的工件（proposal/specs/design/tasks）
 */
import { EventEmitter } from 'events';
import { ArtifactMetadata, ArtifactType, ArtifactStatus, ProposalArtifact, SpecArtifact, DesignArtifact, TasksArtifact, ArtifactManagerConfig, VisionDocument, ConstraintSet } from './types.js';
/**
 * PlanArtifactManager - 工件管理器
 */
export declare class PlanArtifactManager extends EventEmitter {
    private config;
    private artifacts;
    private baseDir;
    constructor(baseDir: string, config?: Partial<ArtifactManagerConfig>);
    /**
     * 初始化工件目录
     */
    initialize(): Promise<void>;
    /**
     * 获取输出路径
     */
    getOutputPath(): string;
    /**
     * 创建 Proposal 工件
     */
    createProposal(vision: VisionDocument, constraints: ConstraintSet): Promise<ProposalArtifact>;
    /**
     * 创建 Spec 工件
     */
    createSpec(name: string, requirement: string, scenarios: {
        name: string;
        given: string;
        when: string;
        then: string;
    }[]): Promise<SpecArtifact>;
    /**
     * 创建 Design 工件
     */
    createDesign(title: string, overview: string, components: {
        name: string;
        responsibility: string;
        dependencies: string[];
    }[], interfaces: {
        name: string;
        methods: string[];
        description: string;
    }[]): Promise<DesignArtifact>;
    /**
     * 创建 Tasks 工件
     */
    createTasks(title: string, tasks: {
        title: string;
        description: string;
        priority: 'high' | 'medium' | 'low';
        files?: string[];
    }[], dependencies?: {
        taskIndex: number;
        dependsOn: number[];
    }[]): Promise<TasksArtifact>;
    /**
     * 更新工件状态
     */
    updateStatus(artifactId: string, status: ArtifactStatus): Promise<boolean>;
    /**
     * 获取工件
     */
    getArtifact(id: string): ArtifactMetadata | undefined;
    /**
     * 获取所有工件
     */
    getAllArtifacts(): ArtifactMetadata[];
    /**
     * 按类型获取工件
     */
    getArtifactsByType(type: ArtifactType): ArtifactMetadata[];
    /**
     * 读取工件内容
     */
    readArtifact(artifactId: string): Promise<string | null>;
    /**
     * 写入工件
     */
    private writeArtifact;
    /**
     * 生成 Proposal Markdown
     */
    private generateProposalMarkdown;
    /**
     * 生成 Spec Markdown
     */
    private generateSpecMarkdown;
    /**
     * 生成 Design Markdown
     */
    private generateDesignMarkdown;
    /**
     * 生成 Tasks Markdown
     */
    private generateTasksMarkdown;
    /**
     * 生成 How 部分
     */
    private generateHowSection;
    /**
     * 生成 Impact 部分
     */
    private generateImpactSection;
    /**
     * 清理文件名
     */
    private sanitizeFileName;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<ArtifactManagerConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): ArtifactManagerConfig;
    /**
     * 清理所有工件
     */
    cleanup(): Promise<void>;
    /**
     * 发送事件
     */
    private emitEvent;
}
//# sourceMappingURL=PlanArtifactManager.d.ts.map