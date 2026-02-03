/**
 * GodCommittee - God Committee 独立监督机制
 * 实现 Aha-Loop 的独立监督委员会，监督所有关键决策
 */

import { EventEmitter } from 'events';
import { AuditManager } from '../../audit/AuditManager.js';
import { AuditActor, AuditResource } from '../../audit/types.js';

/**
 * 决策类型
 */
export type DecisionType =
  | 'architecture'
  | 'technology'
  | 'implementation'
  | 'security'
  | 'performance'
  | 'resource'
  | 'scope'
  | 'priority';

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

const DEFAULT_CONFIG: GodCommitteeConfig = {
  minMembers: 3,
  quorumPercentage: 0.6,
  approvalThreshold: 0.5,
  criticalApprovalThreshold: 0.75,
  votingTimeout: 300000, // 5 minutes
  asyncVoting: true,
  autoApproveBelow: 'low',
};

/**
 * GodCommittee - 监督委员会
 */
export class GodCommittee extends EventEmitter {
  private config: GodCommitteeConfig;
  private members: Map<string, CommitteeMember> = new Map();
  private proposals: Map<string, DecisionProposal> = new Map();
  private votes: Map<string, Vote[]> = new Map();
  private results: Map<string, VotingResult> = new Map();
  private riskAssessments: Map<string, RiskAssessment> = new Map();
  private auditRecords: DecisionAuditRecord[] = [];
  private auditManager?: AuditManager;

  constructor(config: Partial<GodCommitteeConfig> = {}, auditManager?: AuditManager) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.auditManager = auditManager;
  }

  /**
   * 添加委员会成员
   */
  addMember(member: CommitteeMember): void {
    this.members.set(member.id, member);
    this.emit('member:added', member);
  }

  /**
   * 移除委员会成员
   */
  removeMember(memberId: string): boolean {
    const removed = this.members.delete(memberId);
    if (removed) {
      this.emit('member:removed', memberId);
    }
    return removed;
  }

  /**
   * 获取所有成员
   */
  getMembers(): CommitteeMember[] {
    return Array.from(this.members.values());
  }

  /**
   * 提交决策提案
   */
  async submitProposal(proposal: Omit<DecisionProposal, 'id' | 'createdAt'>): Promise<DecisionProposal> {
    const id = `proposal_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    const fullProposal: DecisionProposal = {
      ...proposal,
      id,
      createdAt: Date.now(),
    };

    this.proposals.set(id, fullProposal);
    this.votes.set(id, []);

    // 记录审计
    await this.recordAudit(id, 'created', proposal.requestedBy, { proposal: fullProposal });

    this.emit('proposal:submitted', fullProposal);

    // 检查是否可以自动批准
    if (this.canAutoApprove(fullProposal)) {
      return this.autoApprove(fullProposal);
    }

    return fullProposal;
  }

  /**
   * 检查是否可以自动批准
   */
  private canAutoApprove(proposal: DecisionProposal): boolean {
    if (!this.config.autoApproveBelow) return false;

    const severityOrder: DecisionSeverity[] = ['low', 'medium', 'high', 'critical'];
    const proposalIndex = severityOrder.indexOf(proposal.severity);
    const thresholdIndex = severityOrder.indexOf(this.config.autoApproveBelow);

    return proposalIndex <= thresholdIndex;
  }

  /**
   * 自动批准
   */
  private async autoApprove(proposal: DecisionProposal): Promise<DecisionProposal> {
    const result: VotingResult = {
      proposalId: proposal.id,
      votes: [],
      outcome: 'approved',
      approvalRate: 1,
      weightedScore: 1,
      summary: `Auto-approved: severity ${proposal.severity} is below threshold`,
      completedAt: Date.now(),
    };

    this.results.set(proposal.id, result);
    await this.recordAudit(proposal.id, 'finalized', 'system', { result, autoApproved: true });
    this.emit('proposal:auto-approved', { proposal, result });

    return proposal;
  }

  /**
   * 投票
   */
  async vote(vote: Omit<Vote, 'timestamp'>): Promise<Vote> {
    const proposal = this.proposals.get(vote.proposalId);
    if (!proposal) {
      throw new Error(`Proposal ${vote.proposalId} not found`);
    }

    const member = this.members.get(vote.memberId);
    if (!member) {
      throw new Error(`Member ${vote.memberId} not found`);
    }

    if (member.role === 'observer') {
      throw new Error('Observers cannot vote');
    }

    // 检查是否已投票
    const existingVotes = this.votes.get(vote.proposalId) || [];
    if (existingVotes.some(v => v.memberId === vote.memberId)) {
      throw new Error(`Member ${vote.memberId} has already voted`);
    }

    const fullVote: Vote = {
      ...vote,
      timestamp: Date.now(),
    };

    existingVotes.push(fullVote);
    this.votes.set(vote.proposalId, existingVotes);

    // 记录审计
    await this.recordAudit(vote.proposalId, 'voted', vote.memberId, { vote: fullVote });

    this.emit('vote:cast', fullVote);

    // 检查是否达到法定人数
    if (this.hasQuorum(vote.proposalId)) {
      await this.finalizeVoting(vote.proposalId);
    }

    return fullVote;
  }

  /**
   * 检查是否达到法定人数
   */
  private hasQuorum(proposalId: string): boolean {
    const votes = this.votes.get(proposalId) || [];
    const votingMembers = Array.from(this.members.values()).filter(m => m.role !== 'observer');
    return votes.length >= Math.ceil(votingMembers.length * this.config.quorumPercentage);
  }

  /**
   * 完成投票
   */
  async finalizeVoting(proposalId: string): Promise<VotingResult> {
    const proposal = this.proposals.get(proposalId);
    if (!proposal) {
      throw new Error(`Proposal ${proposalId} not found`);
    }

    const votes = this.votes.get(proposalId) || [];
    const result = this.calculateResult(proposal, votes);

    this.results.set(proposalId, result);

    // 记录审计
    await this.recordAudit(proposalId, 'finalized', 'system', { result });

    this.emit('voting:finalized', result);

    return result;
  }

  /**
   * 计算投票结果
   */
  private calculateResult(proposal: DecisionProposal, votes: Vote[]): VotingResult {
    let totalWeight = 0;
    let approveWeight = 0;
    let rejectWeight = 0;
    let revisionWeight = 0;

    for (const vote of votes) {
      const member = this.members.get(vote.memberId);
      if (!member) continue;

      totalWeight += member.weight;

      switch (vote.option) {
        case 'approve':
          approveWeight += member.weight;
          break;
        case 'reject':
          rejectWeight += member.weight;
          break;
        case 'needs_revision':
          revisionWeight += member.weight;
          break;
      }
    }

    const approvalRate = totalWeight > 0 ? approveWeight / totalWeight : 0;
    const threshold = proposal.severity === 'critical'
      ? this.config.criticalApprovalThreshold
      : this.config.approvalThreshold;

    let outcome: VotingResult['outcome'];
    if (approvalRate >= threshold) {
      outcome = 'approved';
    } else if (rejectWeight > approveWeight) {
      outcome = 'rejected';
    } else if (revisionWeight > approveWeight && revisionWeight > rejectWeight) {
      outcome = 'needs_revision';
    } else {
      outcome = 'deadlock';
    }

    // 收集条件（如果批准但有关注点）
    const conditions: string[] = [];
    for (const vote of votes) {
      if (vote.option === 'approve' && vote.concerns?.length) {
        conditions.push(...vote.concerns);
      }
    }

    return {
      proposalId: proposal.id,
      votes,
      outcome,
      approvalRate,
      weightedScore: approveWeight - rejectWeight,
      summary: this.generateResultSummary(outcome, approvalRate, votes),
      conditions: conditions.length > 0 ? [...new Set(conditions)] : undefined,
      completedAt: Date.now(),
    };
  }

  /**
   * 生成结果摘要
   */
  private generateResultSummary(outcome: VotingResult['outcome'], approvalRate: number, votes: Vote[]): string {
    const approveCount = votes.filter(v => v.option === 'approve').length;
    const rejectCount = votes.filter(v => v.option === 'reject').length;
    const revisionCount = votes.filter(v => v.option === 'needs_revision').length;

    switch (outcome) {
      case 'approved':
        return `Approved with ${(approvalRate * 100).toFixed(1)}% approval (${approveCount} approve, ${rejectCount} reject)`;
      case 'rejected':
        return `Rejected (${rejectCount} reject, ${approveCount} approve)`;
      case 'needs_revision':
        return `Needs revision (${revisionCount} revision requests)`;
      case 'deadlock':
        return `Deadlock - no clear majority (${approveCount} approve, ${rejectCount} reject, ${revisionCount} revision)`;
    }
  }

  /**
   * 获取投票结果
   */
  getResult(proposalId: string): VotingResult | undefined {
    return this.results.get(proposalId);
  }

  /**
   * 获取提案
   */
  getProposal(proposalId: string): DecisionProposal | undefined {
    return this.proposals.get(proposalId);
  }

  /**
   * 获取提案的投票
   */
  getVotes(proposalId: string): Vote[] {
    return this.votes.get(proposalId) || [];
  }

  /**
   * 进行风险评估
   */
  async assessRisk(proposalId: string, assessedBy: string): Promise<RiskAssessment> {
    const proposal = this.proposals.get(proposalId);
    if (!proposal) {
      throw new Error(`Proposal ${proposalId} not found`);
    }

    const factors = this.analyzeRiskFactors(proposal);
    const overallRisk = this.calculateOverallRisk(factors);
    const mitigationPlan = this.generateMitigationPlan(factors);

    const assessment: RiskAssessment = {
      id: `risk_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      proposalId,
      overallRisk,
      factors,
      mitigationPlan,
      assessedBy,
      assessedAt: Date.now(),
    };

    this.riskAssessments.set(proposalId, assessment);
    this.emit('risk:assessed', assessment);

    return assessment;
  }

  /**
   * 分析风险因素
   */
  private analyzeRiskFactors(proposal: DecisionProposal): RiskFactor[] {
    const factors: RiskFactor[] = [];

    // 基于影响范围评估
    if (proposal.impact.scope === 'global' || proposal.impact.scope === 'system') {
      factors.push({
        id: `rf_scope_${Date.now()}`,
        category: 'technical',
        description: `Wide impact scope: ${proposal.impact.scope}`,
        likelihood: 'high',
        impact: 'high',
        score: 9,
      });
    }

    // 基于可逆性评估
    if (proposal.impact.reversibility === 'irreversible' || proposal.impact.reversibility === 'difficult') {
      factors.push({
        id: `rf_reversibility_${Date.now()}`,
        category: 'technical',
        description: `Low reversibility: ${proposal.impact.reversibility}`,
        likelihood: 'medium',
        impact: 'high',
        score: 7,
      });
    }

    // 基于风险列表
    for (const risk of proposal.impact.risks) {
      factors.push({
        id: `rf_listed_${Date.now()}_${Math.random().toString(36).substr(2, 4)}`,
        category: 'technical',
        description: risk,
        likelihood: 'medium',
        impact: 'medium',
        score: 5,
      });
    }

    return factors;
  }

  /**
   * 计算总体风险
   */
  private calculateOverallRisk(factors: RiskFactor[]): RiskAssessment['overallRisk'] {
    if (factors.length === 0) return 'low';

    const avgScore = factors.reduce((sum, f) => sum + f.score, 0) / factors.length;
    const maxScore = Math.max(...factors.map(f => f.score));

    if (maxScore >= 9 || avgScore >= 7) return 'critical';
    if (maxScore >= 7 || avgScore >= 5) return 'high';
    if (maxScore >= 5 || avgScore >= 3) return 'medium';
    return 'low';
  }

  /**
   * 生成缓解计划
   */
  private generateMitigationPlan(factors: RiskFactor[]): MitigationStep[] {
    const steps: MitigationStep[] = [];

    for (const factor of factors) {
      if (factor.score >= 5) {
        steps.push({
          id: `ms_${Date.now()}_${Math.random().toString(36).substr(2, 4)}`,
          riskFactorId: factor.id,
          action: `Mitigate: ${factor.description}`,
          status: 'pending',
        });
      }
    }

    return steps;
  }

  /**
   * 获取风险评估
   */
  getRiskAssessment(proposalId: string): RiskAssessment | undefined {
    return this.riskAssessments.get(proposalId);
  }

  /**
   * 生成监督报告
   */
  async generateReport(sessionId: string, startTime?: number, endTime?: number): Promise<OversightReport> {
    const now = Date.now();
    const start = startTime || now - 24 * 60 * 60 * 1000; // 默认过去24小时
    const end = endTime || now;

    // 收集期间内的决策
    const decisions: DecisionSummary[] = [];
    const statistics: OversightStatistics = {
      totalDecisions: 0,
      approved: 0,
      rejected: 0,
      needsRevision: 0,
      deadlock: 0,
      averageApprovalRate: 0,
      byType: {} as Record<DecisionType, number>,
      bySeverity: {} as Record<DecisionSeverity, number>,
    };

    // 初始化统计
    const types: DecisionType[] = ['architecture', 'technology', 'implementation', 'security', 'performance', 'resource', 'scope', 'priority'];
    const severities: DecisionSeverity[] = ['critical', 'high', 'medium', 'low'];
    for (const t of types) statistics.byType[t] = 0;
    for (const s of severities) statistics.bySeverity[s] = 0;

    let totalApprovalRate = 0;

    for (const [proposalId, result] of this.results) {
      const proposal = this.proposals.get(proposalId);
      if (!proposal) continue;

      if (proposal.createdAt >= start && proposal.createdAt <= end) {
        decisions.push({
          proposalId,
          title: proposal.title,
          type: proposal.type,
          severity: proposal.severity,
          outcome: result.outcome,
          approvalRate: result.approvalRate,
          keyReasons: result.votes.slice(0, 3).map(v => v.reasoning),
        });

        statistics.totalDecisions++;
        statistics[result.outcome === 'needs_revision' ? 'needsRevision' : result.outcome]++;
        statistics.byType[proposal.type]++;
        statistics.bySeverity[proposal.severity]++;
        totalApprovalRate += result.approvalRate;
      }
    }

    statistics.averageApprovalRate = statistics.totalDecisions > 0
      ? totalApprovalRate / statistics.totalDecisions
      : 0;

    // 识别关注点
    const concerns = this.identifyConcerns(decisions, statistics);

    // 生成建议
    const recommendations = this.generateRecommendations(concerns, statistics);

    const report: OversightReport = {
      id: `report_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      sessionId,
      period: { start, end },
      decisions,
      statistics,
      concerns,
      recommendations,
      generatedAt: now,
    };

    this.emit('report:generated', report);

    return report;
  }

  /**
   * 识别关注点
   */
  private identifyConcerns(decisions: DecisionSummary[], statistics: OversightStatistics): OversightConcern[] {
    const concerns: OversightConcern[] = [];

    // 检查高拒绝率
    if (statistics.rejected > statistics.approved) {
      concerns.push({
        id: `concern_${Date.now()}_1`,
        level: 'warning',
        category: 'decision_quality',
        description: 'High rejection rate indicates potential issues with proposal quality',
        relatedDecisions: decisions.filter(d => d.outcome === 'rejected').map(d => d.proposalId),
        suggestedAction: 'Review proposal preparation process and requirements',
      });
    }

    // 检查死锁
    if (statistics.deadlock > 0) {
      concerns.push({
        id: `concern_${Date.now()}_2`,
        level: 'warning',
        category: 'governance',
        description: `${statistics.deadlock} decision(s) resulted in deadlock`,
        relatedDecisions: decisions.filter(d => d.outcome === 'deadlock').map(d => d.proposalId),
        suggestedAction: 'Consider adding tie-breaking mechanism or additional committee members',
      });
    }

    // 检查关键决策
    const criticalDecisions = decisions.filter(d => d.severity === 'critical');
    if (criticalDecisions.length > 0) {
      const lowApprovalCritical = criticalDecisions.filter(d => d.approvalRate < 0.8);
      if (lowApprovalCritical.length > 0) {
        concerns.push({
          id: `concern_${Date.now()}_3`,
          level: 'critical',
          category: 'risk',
          description: 'Critical decisions with low approval rate may indicate unresolved concerns',
          relatedDecisions: lowApprovalCritical.map(d => d.proposalId),
          suggestedAction: 'Review and address concerns before proceeding',
        });
      }
    }

    return concerns;
  }

  /**
   * 生成建议
   */
  private generateRecommendations(concerns: OversightConcern[], statistics: OversightStatistics): string[] {
    const recommendations: string[] = [];

    if (concerns.some(c => c.level === 'critical')) {
      recommendations.push('Address critical concerns before proceeding with implementation');
    }

    if (statistics.averageApprovalRate < 0.6) {
      recommendations.push('Improve proposal quality and stakeholder alignment');
    }

    if (statistics.deadlock > 0) {
      recommendations.push('Establish clearer decision criteria to avoid deadlocks');
    }

    if (statistics.byType['security'] > 0 && statistics.bySeverity['critical'] > 0) {
      recommendations.push('Conduct thorough security review for critical security decisions');
    }

    if (recommendations.length === 0) {
      recommendations.push('Continue current governance practices');
    }

    return recommendations;
  }

  /**
   * 记录审计
   */
  private async recordAudit(
    proposalId: string,
    action: DecisionAuditRecord['action'],
    actor: string,
    details: Record<string, unknown>
  ): Promise<void> {
    const record: DecisionAuditRecord = {
      id: `audit_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      proposalId,
      action,
      actor,
      details,
      timestamp: Date.now(),
    };

    this.auditRecords.push(record);

    // 如果有审计管理器，也记录到那里
    if (this.auditManager) {
      const auditActor: AuditActor = {
        id: actor,
        type: actor === 'system' ? 'system' : 'agent',
      };
      const auditResource: AuditResource = {
        type: 'decision',
        id: proposalId,
      };

      await this.auditManager.log({
        eventType: 'modify',
        severity: 'info',
        actor: auditActor,
        resource: auditResource,
        action,
        outcome: 'success',
        details,
      });
    }

    this.emit('audit:recorded', record);
  }

  /**
   * 获取审计记录
   */
  getAuditRecords(proposalId?: string): DecisionAuditRecord[] {
    if (proposalId) {
      return this.auditRecords.filter(r => r.proposalId === proposalId);
    }
    return [...this.auditRecords];
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<GodCommitteeConfig>): void {
    this.config = { ...this.config, ...config };
    this.emit('config:updated', this.config);
  }

  /**
   * 获取配置
   */
  getConfig(): GodCommitteeConfig {
    return { ...this.config };
  }

  /**
   * 重置委员会
   */
  reset(): void {
    this.proposals.clear();
    this.votes.clear();
    this.results.clear();
    this.riskAssessments.clear();
    this.auditRecords = [];
    this.emit('committee:reset');
  }
}
