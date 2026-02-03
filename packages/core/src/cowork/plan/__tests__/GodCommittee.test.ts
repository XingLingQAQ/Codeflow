import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  GodCommittee,
  CommitteeMember,
  DecisionProposal,
  Vote,
  VotingResult,
  RiskAssessment,
  OversightReport,
} from '../GodCommittee.js';

// Mock data
const createMockMember = (id: string, role: CommitteeMember['role'] = 'member', weight: number = 1): CommitteeMember => ({
  id,
  name: `Member ${id}`,
  role,
  expertise: ['general'],
  weight,
});

const createMockProposal = (
  type: DecisionProposal['type'] = 'implementation',
  severity: DecisionProposal['severity'] = 'medium'
): Omit<DecisionProposal, 'id' | 'createdAt'> => ({
  type,
  severity,
  title: 'Test Proposal',
  description: 'Test description',
  rationale: 'Test rationale',
  alternatives: [
    {
      id: 'alt-1',
      description: 'Alternative 1',
      pros: ['Pro 1'],
      cons: ['Con 1'],
      effort: 'medium',
    },
  ],
  impact: {
    scope: 'module',
    reversibility: 'moderate',
    affectedAreas: ['area1'],
    risks: ['risk1'],
    mitigations: ['mitigation1'],
  },
  requestedBy: 'user-1',
  sessionId: 'session-1',
});

describe('GodCommittee', () => {
  let committee: GodCommittee;

  beforeEach(() => {
    committee = new GodCommittee();
  });

  describe('member management', () => {
    it('should add a member', () => {
      const member = createMockMember('m1');
      committee.addMember(member);

      const members = committee.getMembers();
      expect(members).toHaveLength(1);
      expect(members[0].id).toBe('m1');
    });

    it('should emit member:added event', () => {
      const listener = vi.fn();
      committee.on('member:added', listener);

      const member = createMockMember('m1');
      committee.addMember(member);

      expect(listener).toHaveBeenCalledWith(member);
    });

    it('should remove a member', () => {
      const member = createMockMember('m1');
      committee.addMember(member);

      const removed = committee.removeMember('m1');
      expect(removed).toBe(true);
      expect(committee.getMembers()).toHaveLength(0);
    });

    it('should return false when removing non-existent member', () => {
      const removed = committee.removeMember('non-existent');
      expect(removed).toBe(false);
    });
  });

  describe('proposal submission', () => {
    it('should submit a proposal', async () => {
      const proposalData = createMockProposal();
      const proposal = await committee.submitProposal(proposalData);

      expect(proposal.id).toBeDefined();
      expect(proposal.title).toBe('Test Proposal');
      expect(proposal.createdAt).toBeDefined();
    });

    it('should emit proposal:submitted event', async () => {
      const listener = vi.fn();
      committee.on('proposal:submitted', listener);

      await committee.submitProposal(createMockProposal());

      expect(listener).toHaveBeenCalled();
    });

    it('should auto-approve low severity proposals when configured', async () => {
      const autoApproveCommittee = new GodCommittee({ autoApproveBelow: 'low' });
      const listener = vi.fn();
      autoApproveCommittee.on('proposal:auto-approved', listener);

      await autoApproveCommittee.submitProposal(createMockProposal('implementation', 'low'));

      expect(listener).toHaveBeenCalled();
    });

    it('should not auto-approve high severity proposals', async () => {
      const autoApproveCommittee = new GodCommittee({ autoApproveBelow: 'low' });
      const listener = vi.fn();
      autoApproveCommittee.on('proposal:auto-approved', listener);

      await autoApproveCommittee.submitProposal(createMockProposal('implementation', 'high'));

      expect(listener).not.toHaveBeenCalled();
    });

    it('should retrieve a proposal', async () => {
      const proposal = await committee.submitProposal(createMockProposal());
      const retrieved = committee.getProposal(proposal.id);

      expect(retrieved).toBeDefined();
      expect(retrieved?.id).toBe(proposal.id);
    });
  });

  describe('voting', () => {
    let proposal: DecisionProposal;

    beforeEach(async () => {
      committee.addMember(createMockMember('m1', 'chair', 2));
      committee.addMember(createMockMember('m2', 'member', 1));
      committee.addMember(createMockMember('m3', 'member', 1));
      proposal = await committee.submitProposal(createMockProposal());
    });

    it('should cast a vote', async () => {
      const vote = await committee.vote({
        memberId: 'm1',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Looks good',
      });

      expect(vote.timestamp).toBeDefined();
      expect(vote.option).toBe('approve');
    });

    it('should emit vote:cast event', async () => {
      const listener = vi.fn();
      committee.on('vote:cast', listener);

      await committee.vote({
        memberId: 'm1',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Looks good',
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should throw when voting on non-existent proposal', async () => {
      await expect(committee.vote({
        memberId: 'm1',
        proposalId: 'non-existent',
        option: 'approve',
        reasoning: 'Test',
      })).rejects.toThrow('Proposal non-existent not found');
    });

    it('should throw when non-member votes', async () => {
      await expect(committee.vote({
        memberId: 'non-member',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Test',
      })).rejects.toThrow('Member non-member not found');
    });

    it('should throw when observer tries to vote', async () => {
      committee.addMember(createMockMember('observer', 'observer', 0));

      await expect(committee.vote({
        memberId: 'observer',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Test',
      })).rejects.toThrow('Observers cannot vote');
    });

    it('should throw when member votes twice', async () => {
      await committee.vote({
        memberId: 'm1',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'First vote',
      });

      await expect(committee.vote({
        memberId: 'm1',
        proposalId: proposal.id,
        option: 'reject',
        reasoning: 'Second vote',
      })).rejects.toThrow('Member m1 has already voted');
    });

    it('should get votes for a proposal', async () => {
      await committee.vote({
        memberId: 'm1',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Test',
      });

      const votes = committee.getVotes(proposal.id);
      expect(votes).toHaveLength(1);
    });
  });

  describe('voting finalization', () => {
    let proposal: DecisionProposal;

    beforeEach(async () => {
      committee.addMember(createMockMember('m1', 'chair', 2));
      committee.addMember(createMockMember('m2', 'member', 1));
      committee.addMember(createMockMember('m3', 'member', 1));
      proposal = await committee.submitProposal(createMockProposal());
    });

    it('should finalize voting with approval', async () => {
      await committee.vote({ memberId: 'm1', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });
      await committee.vote({ memberId: 'm2', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });

      const result = await committee.finalizeVoting(proposal.id);

      expect(result.outcome).toBe('approved');
      expect(result.approvalRate).toBeGreaterThan(0.5);
    });

    it('should finalize voting with rejection', async () => {
      await committee.vote({ memberId: 'm1', proposalId: proposal.id, option: 'reject', reasoning: 'Bad' });
      await committee.vote({ memberId: 'm2', proposalId: proposal.id, option: 'reject', reasoning: 'Bad' });
      await committee.vote({ memberId: 'm3', proposalId: proposal.id, option: 'reject', reasoning: 'Bad' });

      const result = await committee.finalizeVoting(proposal.id);

      expect(result.outcome).toBe('rejected');
    });

    it('should finalize voting with needs_revision', async () => {
      await committee.vote({ memberId: 'm1', proposalId: proposal.id, option: 'needs_revision', reasoning: 'Needs work' });
      await committee.vote({ memberId: 'm2', proposalId: proposal.id, option: 'needs_revision', reasoning: 'Needs work' });
      await committee.vote({ memberId: 'm3', proposalId: proposal.id, option: 'abstain', reasoning: 'No opinion' });

      const result = await committee.finalizeVoting(proposal.id);

      expect(result.outcome).toBe('needs_revision');
    });

    it('should emit voting:finalized event', async () => {
      const listener = vi.fn();
      committee.on('voting:finalized', listener);

      await committee.vote({ memberId: 'm1', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });
      await committee.vote({ memberId: 'm2', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });
      await committee.finalizeVoting(proposal.id);

      expect(listener).toHaveBeenCalled();
    });

    it('should include conditions when approved with concerns', async () => {
      await committee.vote({
        memberId: 'm1',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Good',
        concerns: ['Need more testing'],
      });
      await committee.vote({
        memberId: 'm2',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Good',
        concerns: ['Security review needed'],
      });

      const result = await committee.finalizeVoting(proposal.id);

      expect(result.conditions).toBeDefined();
      expect(result.conditions).toContain('Need more testing');
      expect(result.conditions).toContain('Security review needed');
    });

    it('should get voting result', async () => {
      await committee.vote({ memberId: 'm1', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });
      await committee.vote({ memberId: 'm2', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });
      await committee.finalizeVoting(proposal.id);

      const result = committee.getResult(proposal.id);
      expect(result).toBeDefined();
      expect(result?.outcome).toBe('approved');
    });

    it('should require higher threshold for critical decisions', async () => {
      const criticalProposal = await committee.submitProposal(createMockProposal('security', 'critical'));

      // 3 out of 4 weight approve (75%)
      await committee.vote({ memberId: 'm1', proposalId: criticalProposal.id, option: 'approve', reasoning: 'Good' });
      await committee.vote({ memberId: 'm2', proposalId: criticalProposal.id, option: 'approve', reasoning: 'Good' });
      await committee.vote({ memberId: 'm3', proposalId: criticalProposal.id, option: 'reject', reasoning: 'Bad' });

      const result = await committee.finalizeVoting(criticalProposal.id);

      // With default criticalApprovalThreshold of 0.75, this should pass
      expect(result.approvalRate).toBe(0.75);
    });
  });

  describe('risk assessment', () => {
    let proposal: DecisionProposal;

    beforeEach(async () => {
      proposal = await committee.submitProposal(createMockProposal());
    });

    it('should assess risk', async () => {
      const assessment = await committee.assessRisk(proposal.id, 'assessor-1');

      expect(assessment.id).toBeDefined();
      expect(assessment.proposalId).toBe(proposal.id);
      expect(assessment.overallRisk).toBeDefined();
      expect(assessment.factors).toBeDefined();
      expect(assessment.mitigationPlan).toBeDefined();
    });

    it('should emit risk:assessed event', async () => {
      const listener = vi.fn();
      committee.on('risk:assessed', listener);

      await committee.assessRisk(proposal.id, 'assessor-1');

      expect(listener).toHaveBeenCalled();
    });

    it('should throw when assessing non-existent proposal', async () => {
      await expect(committee.assessRisk('non-existent', 'assessor-1'))
        .rejects.toThrow('Proposal non-existent not found');
    });

    it('should get risk assessment', async () => {
      await committee.assessRisk(proposal.id, 'assessor-1');
      const assessment = committee.getRiskAssessment(proposal.id);

      expect(assessment).toBeDefined();
      expect(assessment?.proposalId).toBe(proposal.id);
    });

    it('should identify high risk for global scope', async () => {
      const globalProposal = await committee.submitProposal({
        ...createMockProposal(),
        impact: {
          scope: 'global',
          reversibility: 'irreversible',
          affectedAreas: ['all'],
          risks: ['major risk'],
          mitigations: [],
        },
      });

      const assessment = await committee.assessRisk(globalProposal.id, 'assessor-1');

      expect(['critical', 'high']).toContain(assessment.overallRisk);
    });
  });

  describe('oversight report', () => {
    beforeEach(async () => {
      committee.addMember(createMockMember('m1', 'chair', 2));
      committee.addMember(createMockMember('m2', 'member', 1));

      // Create and finalize some proposals
      const proposal1 = await committee.submitProposal(createMockProposal('implementation', 'medium'));
      await committee.vote({ memberId: 'm1', proposalId: proposal1.id, option: 'approve', reasoning: 'Good' });
      await committee.vote({ memberId: 'm2', proposalId: proposal1.id, option: 'approve', reasoning: 'Good' });
      await committee.finalizeVoting(proposal1.id);

      const proposal2 = await committee.submitProposal(createMockProposal('security', 'high'));
      await committee.vote({ memberId: 'm1', proposalId: proposal2.id, option: 'reject', reasoning: 'Bad' });
      await committee.vote({ memberId: 'm2', proposalId: proposal2.id, option: 'reject', reasoning: 'Bad' });
      await committee.finalizeVoting(proposal2.id);
    });

    it('should generate oversight report', async () => {
      const report = await committee.generateReport('session-1');

      expect(report.id).toBeDefined();
      expect(report.sessionId).toBe('session-1');
      expect(report.decisions).toBeDefined();
      expect(report.statistics).toBeDefined();
      expect(report.concerns).toBeDefined();
      expect(report.recommendations).toBeDefined();
    });

    it('should emit report:generated event', async () => {
      const listener = vi.fn();
      committee.on('report:generated', listener);

      await committee.generateReport('session-1');

      expect(listener).toHaveBeenCalled();
    });

    it('should include statistics', async () => {
      const report = await committee.generateReport('session-1');

      expect(report.statistics.totalDecisions).toBe(2);
      expect(report.statistics.approved).toBe(1);
      expect(report.statistics.rejected).toBe(1);
    });

    it('should identify concerns when rejection rate is high', async () => {
      // Add more rejected proposals
      const proposal3 = await committee.submitProposal(createMockProposal());
      await committee.vote({ memberId: 'm1', proposalId: proposal3.id, option: 'reject', reasoning: 'Bad' });
      await committee.vote({ memberId: 'm2', proposalId: proposal3.id, option: 'reject', reasoning: 'Bad' });
      await committee.finalizeVoting(proposal3.id);

      const report = await committee.generateReport('session-1');

      expect(report.concerns.some(c => c.category === 'decision_quality')).toBe(true);
    });
  });

  describe('audit records', () => {
    it('should record audit for proposal creation', async () => {
      const proposal = await committee.submitProposal(createMockProposal());
      const records = committee.getAuditRecords(proposal.id);

      expect(records.some(r => r.action === 'created')).toBe(true);
    });

    it('should record audit for voting', async () => {
      committee.addMember(createMockMember('m1'));
      const proposal = await committee.submitProposal(createMockProposal());

      await committee.vote({
        memberId: 'm1',
        proposalId: proposal.id,
        option: 'approve',
        reasoning: 'Good',
      });

      const records = committee.getAuditRecords(proposal.id);
      expect(records.some(r => r.action === 'voted')).toBe(true);
    });

    it('should record audit for finalization', async () => {
      committee.addMember(createMockMember('m1'));
      committee.addMember(createMockMember('m2'));
      const proposal = await committee.submitProposal(createMockProposal());

      await committee.vote({ memberId: 'm1', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });
      await committee.vote({ memberId: 'm2', proposalId: proposal.id, option: 'approve', reasoning: 'Good' });
      await committee.finalizeVoting(proposal.id);

      const records = committee.getAuditRecords(proposal.id);
      expect(records.some(r => r.action === 'finalized')).toBe(true);
    });

    it('should get all audit records', async () => {
      const proposal = await committee.submitProposal(createMockProposal());
      const allRecords = committee.getAuditRecords();

      expect(allRecords.length).toBeGreaterThan(0);
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      committee.updateConfig({ minMembers: 5 });
      const config = committee.getConfig();

      expect(config.minMembers).toBe(5);
    });

    it('should emit config:updated event', () => {
      const listener = vi.fn();
      committee.on('config:updated', listener);

      committee.updateConfig({ minMembers: 5 });

      expect(listener).toHaveBeenCalled();
    });

    it('should get config', () => {
      const config = committee.getConfig();

      expect(config.minMembers).toBeDefined();
      expect(config.quorumPercentage).toBeDefined();
      expect(config.approvalThreshold).toBeDefined();
    });
  });

  describe('reset', () => {
    it('should reset committee', async () => {
      committee.addMember(createMockMember('m1'));
      await committee.submitProposal(createMockProposal());

      committee.reset();

      expect(committee.getAuditRecords()).toHaveLength(0);
    });

    it('should emit committee:reset event', () => {
      const listener = vi.fn();
      committee.on('committee:reset', listener);

      committee.reset();

      expect(listener).toHaveBeenCalled();
    });
  });
});
