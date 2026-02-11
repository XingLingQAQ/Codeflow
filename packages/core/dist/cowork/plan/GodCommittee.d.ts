/**
 * GodCommittee - God Committee 独立监督机制
 * 实现 Aha-Loop 的独立监督委员会，监督所有关键决策
 */
import { EventEmitter } from 'events';
import { AuditManager } from '../../audit/AuditManager.js';
/**
 * 决策类型
 */
export type DecisionType = 'architecture' | 'technology' | 'implementation' | 'security' | 'performance' | 'resource' | 'scope' | 'priority';
/**
 * 决策严重程度
 */
export type DecisionSeverity = 'critical' | 'high' | 'medium' | 'low';
/**
 * 投票选项
 */
export type VoteOption = 'approve' | 'reject' | 'abstain' | 'needs_revision';
/**
 * 委员会成员
 */
export interface CommitteeMember {
    id: string;
    name: string;
    role: 'chair' | 'member' | 'observer';
    expertise: string[];
    modelId?: string;
    weight: number;
}
/**
 * 决策提案
 */
export interface DecisionProposal {
    id: string;
    type: DecisionType;
    severity: DecisionSeverity;
    title: string;
    description: string;
    rationale: string;
    alternatives: Alternative[];
    impact: Impact;
    requestedBy: string;
    sessionId: string;
    createdAt: number;
}
/**
 * 替代方案
 */
export interface Alternative {
    id: string;
    description: string;
    pros: string[];
    cons: string[];
    effort: 'low' | 'medium' | 'high';
}
/**
 * 影响评估
 */
export interface Impact {
    scope: 'local' | 'module' | 'system' | 'global';
    reversibility: 'easy' | 'moderate' | 'difficult' | 'irreversible';
    affectedAreas: string[];
    risks: string[];
    mitigations: string[];
}
/**
 * 投票
 */
export interface Vote {
    memberId: string;
    proposalId: string;
    option: VoteOption;
    reasoning: string;
    concerns?: string[];
    suggestions?: string[];
    timestamp: number;
}
/**
 * 投票结果
 */
export interface VotingResult {
    proposalId: string;
    votes: Vote[];
    outcome: 'approved' | 'rejected' | 'needs_revision' | 'deadlock';
    approvalRate: number;
    weightedScore: number;
    summary: string;
    conditions?: string[];
    completedAt: number;
}
/**
 * 监督报告
 */
export interface OversightReport {
    id: string;
    sessionId: string;
    period: {
        start: number;
        end: number;
    };
    decisions: DecisionSummary[];
    statistics: OversightStatistics;
    concerns: OversightConcern[];
    recommendations: string[];
    generatedAt: number;
}
/**
 * 决策摘要
 */
export interface DecisionSummary {
    proposalId: string;
    title: string;
    type: DecisionType;
    severity: DecisionSeverity;
    outcome: VotingResult['outcome'];
    approvalRate: number;
    keyReasons: string[];
}
/**
 * 监督统计
 */
export interface OversightStatistics {
    totalDecisions: number;
    approved: number;
    rejected: number;
    needsRevision: number;
    deadlock: number;
    averageApprovalRate: number;
    byType: Record<DecisionType, number>;
    bySeverity: Record<DecisionSeverity, number>;
}
/**
 * 监督关注点
 */
export interface OversightConcern {
    id: string;
    level: 'critical' | 'warning' | 'info';
    category: string;
    description: string;
    relatedDecisions: string[];
    suggestedAction: string;
}
/**
 * 风险评估
 */
export interface RiskAssessment {
    id: string;
    proposalId: string;
    overallRisk: 'critical' | 'high' | 'medium' | 'low';
    factors: RiskFactor[];
    mitigationPlan: MitigationStep[];
    assessedBy: string;
    assessedAt: number;
}
/**
 * 风险因素
 */
export interface RiskFactor {
    id: string;
    category: 'technical' | 'security' | 'performance' | 'compatibility' | 'resource' | 'timeline';
    description: string;
    likelihood: 'high' | 'medium' | 'low';
    impact: 'high' | 'medium' | 'low';
    score: number;
}
/**
 * 缓解步骤
 */
export interface MitigationStep {
    id: string;
    riskFactorId: string;
    action: string;
    owner?: string;
    deadline?: string;
    status: 'pending' | 'in_progress' | 'completed';
}
/**
 * 决策审计记录
 */
export interface DecisionAuditRecord {
    id: string;
    proposalId: string;
    action: 'created' | 'voted' | 'revised' | 'finalized' | 'executed' | 'rolled_back';
    actor: string;
    details: Record<string, unknown>;
    timestamp: number;
}
/**
 * God Committee 配置
 */
export interface GodCommitteeConfig {
    minMembers: number;
    quorumPercentage: number;
    approvalThreshold: number;
    criticalApprovalThreshold: number;
    votingTimeout: number;
    asyncVoting: boolean;
    autoApproveBelow: DecisionSeverity | null;
}
/**
 * GodCommittee - 监督委员会
 */
export declare class GodCommittee extends EventEmitter {
    private config;
    private members;
    private proposals;
    private votes;
    private results;
    private riskAssessments;
    private auditRecords;
    private auditManager?;
    constructor(config?: Partial<GodCommitteeConfig>, auditManager?: AuditManager);
    /**
     * 添加委员会成员
     */
    addMember(member: CommitteeMember): void;
    /**
     * 移除委员会成员
     */
    removeMember(memberId: string): boolean;
    /**
     * 获取所有成员
     */
    getMembers(): CommitteeMember[];
    /**
     * 提交决策提案
     */
    submitProposal(proposal: Omit<DecisionProposal, 'id' | 'createdAt'>): Promise<DecisionProposal>;
    /**
     * 检查是否可以自动批准
     */
    private canAutoApprove;
    /**
     * 自动批准
     */
    private autoApprove;
    /**
     * 投票
     */
    vote(vote: Omit<Vote, 'timestamp'>): Promise<Vote>;
    /**
     * 检查是否达到法定人数
     */
    private hasQuorum;
    /**
     * 完成投票
     */
    finalizeVoting(proposalId: string): Promise<VotingResult>;
    /**
     * 计算投票结果
     */
    private calculateResult;
    /**
     * 生成结果摘要
     */
    private generateResultSummary;
    /**
     * 获取投票结果
     */
    getResult(proposalId: string): VotingResult | undefined;
    /**
     * 获取提案
     */
    getProposal(proposalId: string): DecisionProposal | undefined;
    /**
     * 获取提案的投票
     */
    getVotes(proposalId: string): Vote[];
    /**
     * 进行风险评估
     */
    assessRisk(proposalId: string, assessedBy: string): Promise<RiskAssessment>;
    /**
     * 分析风险因素
     */
    private analyzeRiskFactors;
    /**
     * 计算总体风险
     */
    private calculateOverallRisk;
    /**
     * 生成缓解计划
     */
    private generateMitigationPlan;
    /**
     * 获取风险评估
     */
    getRiskAssessment(proposalId: string): RiskAssessment | undefined;
    /**
     * 生成监督报告
     */
    generateReport(sessionId: string, startTime?: number, endTime?: number): Promise<OversightReport>;
    /**
     * 识别关注点
     */
    private identifyConcerns;
    /**
     * 生成建议
     */
    private generateRecommendations;
    /**
     * 记录审计
     */
    private recordAudit;
    /**
     * 获取审计记录
     */
    getAuditRecords(proposalId?: string): DecisionAuditRecord[];
    /**
     * 更新配置
     */
    updateConfig(config: Partial<GodCommitteeConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): GodCommitteeConfig;
    /**
     * 重置委员会
     */
    reset(): void;
}
//# sourceMappingURL=GodCommittee.d.ts.map