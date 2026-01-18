/**
 * 多智能体协作看板组件
 * 黑板模式协作空间
 */

import React, { useState, useMemo } from 'react';
import {
  AgentBoardProps,
  AgentCardProps,
  BlackboardAreaProps,
  VotingProgressProps,
  AgentInfo,
  BOARD_AGENT_ROLE_CONFIG,
  BOARD_AGENT_STATUS_CONFIG,
  VOTE_STATUS_CONFIG,
  BLACKBOARD_TYPE_CONFIG,
  formatBoardTimestamp,
  calculateVoteProgress,
} from './types';

/**
 * Agent卡片组件
 */
export const AgentCard: React.FC<AgentCardProps> = ({
  agent,
  isSelected,
  onSelect,
  onToggleExpand,
}) => {
  const roleConfig = BOARD_AGENT_ROLE_CONFIG[agent.role];
  const statusConfig = BOARD_AGENT_STATUS_CONFIG[agent.status];

  return (
    <div
      style={{
        width: 160,
        backgroundColor: isSelected ? `${roleConfig.color}15` : '#fff',
        border: `2px solid ${isSelected ? roleConfig.color : '#e0e0e0'}`,
        borderRadius: 12,
        overflow: 'hidden',
        cursor: 'pointer',
        transition: 'all 0.2s',
      }}
      onClick={() => onSelect?.(agent.id)}
    >
      {/* 头部 */}
      <div
        style={{
          padding: '12px 10px',
          backgroundColor: `${roleConfig.color}10`,
          borderBottom: '1px solid #e0e0e0',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 24 }}>{roleConfig.icon}</span>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div
              style={{
                fontSize: 13,
                fontWeight: 600,
                color: '#333',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {agent.name}
            </div>
            <div style={{ fontSize: 10, color: '#666' }}>{roleConfig.label}</div>
          </div>
        </div>
      </div>

      {/* 状态 */}
      <div style={{ padding: '10px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8 }}>
          <span
            style={{
              width: 8,
              height: 8,
              borderRadius: '50%',
              backgroundColor: statusConfig.color,
              animation: statusConfig.animation === 'pulse' ? 'pulse 1.5s infinite' : 'none',
            }}
          />
          <span style={{ fontSize: 11, color: statusConfig.color, fontWeight: 500 }}>
            {statusConfig.label}
          </span>
        </div>

        {/* 当前任务 */}
        {agent.currentTask && (
          <div
            style={{
              fontSize: 11,
              color: '#666',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              marginBottom: 6,
            }}
          >
            {agent.currentTask}
          </div>
        )}

        {/* 最后活动时间 */}
        <div style={{ fontSize: 10, color: '#999' }}>
          Last: {formatBoardTimestamp(agent.lastActivity)}
        </div>
      </div>

      {/* 展开日志按钮 */}
      <div
        style={{
          padding: '8px 10px',
          borderTop: '1px solid #e0e0e0',
          backgroundColor: '#fafafa',
          textAlign: 'center',
        }}
        onClick={(e) => {
          e.stopPropagation();
          onToggleExpand?.(agent.id);
        }}
      >
        <span style={{ fontSize: 11, color: '#666' }}>
          {agent.isExpanded ? '▲ Hide Logs' : '▼ Show Logs'} ({agent.logs.length})
        </span>
      </div>

      {/* 日志列表 */}
      {agent.isExpanded && (
        <div
          style={{
            maxHeight: 200,
            overflow: 'auto',
            borderTop: '1px solid #e0e0e0',
            backgroundColor: '#f5f5f5',
          }}
        >
          {agent.logs.length === 0 ? (
            <div style={{ padding: 12, textAlign: 'center', color: '#999', fontSize: 11 }}>
              No logs
            </div>
          ) : (
            agent.logs.slice(-10).map((log) => (
              <div
                key={log.id}
                style={{
                  padding: '6px 10px',
                  borderBottom: '1px solid #e0e0e0',
                  fontSize: 10,
                }}
              >
                <div style={{ color: '#999', marginBottom: 2 }}>
                  {formatBoardTimestamp(log.timestamp)}
                </div>
                <div
                  style={{
                    color: log.type === 'error' ? '#F44336' : '#333',
                  }}
                >
                  {log.message}
                </div>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
};

/**
 * 黑板区域组件
 */
export const BlackboardArea: React.FC<BlackboardAreaProps> = ({ entries, onEntryClick }) => {
  const groupedEntries = useMemo(() => {
    const groups: Record<string, typeof entries> = {
      state: [],
      proposal: [],
      decision: [],
      artifact: [],
    };
    entries.forEach((entry) => {
      groups[entry.type].push(entry);
    });
    return groups;
  }, [entries]);

  return (
    <div
      style={{
        flex: 1,
        backgroundColor: '#1a1a2e',
        borderRadius: 12,
        padding: 16,
        overflow: 'auto',
      }}
    >
      {/* 标题 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          marginBottom: 16,
          paddingBottom: 12,
          borderBottom: '1px solid #333',
        }}
      >
        <span style={{ fontSize: 20 }}>📋</span>
        <span style={{ fontSize: 16, fontWeight: 600, color: '#fff' }}>Blackboard</span>
        <span style={{ fontSize: 12, color: '#888' }}>({entries.length} entries)</span>
      </div>

      {/* 分组显示 */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 12 }}>
        {Object.entries(groupedEntries).map(([type, items]) => {
          const config = BLACKBOARD_TYPE_CONFIG[type as keyof typeof BLACKBOARD_TYPE_CONFIG];
          return (
            <div
              key={type}
              style={{
                backgroundColor: '#252540',
                borderRadius: 8,
                padding: 12,
                minHeight: 100,
              }}
            >
              {/* 分组标题 */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  marginBottom: 10,
                }}
              >
                <span style={{ fontSize: 14 }}>{config.icon}</span>
                <span style={{ fontSize: 12, fontWeight: 500, color: config.color }}>
                  {config.label}
                </span>
                <span style={{ fontSize: 10, color: '#666' }}>({items.length})</span>
              </div>

              {/* 条目列表 */}
              {items.length === 0 ? (
                <div style={{ color: '#555', fontSize: 11, textAlign: 'center', padding: 10 }}>
                  No {config.label.toLowerCase()}s
                </div>
              ) : (
                items.slice(-5).map((entry) => (
                  <div
                    key={entry.id}
                    style={{
                      padding: '8px 10px',
                      marginBottom: 6,
                      backgroundColor: '#1a1a2e',
                      borderRadius: 6,
                      borderLeft: `3px solid ${config.color}`,
                      cursor: 'pointer',
                    }}
                    onClick={() => onEntryClick?.(entry.id)}
                  >
                    <div
                      style={{
                        fontSize: 11,
                        fontWeight: 500,
                        color: '#ddd',
                        marginBottom: 4,
                      }}
                    >
                      {entry.key}
                    </div>
                    <div
                      style={{
                        fontSize: 10,
                        color: '#888',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {typeof entry.value === 'string'
                        ? entry.value
                        : JSON.stringify(entry.value)}
                    </div>
                    <div style={{ fontSize: 9, color: '#555', marginTop: 4 }}>
                      by {entry.author} • v{entry.version}
                    </div>
                  </div>
                ))
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

/**
 * 投票进度环形图
 */
const VotingRing: React.FC<{ progress: ReturnType<typeof calculateVoteProgress> }> = ({
  progress,
}) => {
  const size = 80;
  const strokeWidth = 8;
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;

  const approvedOffset = circumference * (1 - progress.approved / 100);
  const rejectedOffset = circumference * (1 - (progress.approved + progress.rejected) / 100);

  return (
    <svg width={size} height={size} style={{ transform: 'rotate(-90deg)' }}>
      {/* 背景圆 */}
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="#e0e0e0"
        strokeWidth={strokeWidth}
      />
      {/* Rejected */}
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="#F44336"
        strokeWidth={strokeWidth}
        strokeDasharray={circumference}
        strokeDashoffset={rejectedOffset}
        style={{ transition: 'stroke-dashoffset 0.3s' }}
      />
      {/* Approved */}
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="#4CAF50"
        strokeWidth={strokeWidth}
        strokeDasharray={circumference}
        strokeDashoffset={approvedOffset}
        style={{ transition: 'stroke-dashoffset 0.3s' }}
      />
    </svg>
  );
};

/**
 * 投票进度组件
 */
export const VotingProgress: React.FC<VotingProgressProps> = ({ vote, onVoteAction }) => {
  const progress = calculateVoteProgress(vote);
  const approvedCount = vote.votes.filter((v) => v.status === 'approved').length;
  const rejectedCount = vote.votes.filter((v) => v.status === 'rejected').length;

  return (
    <div
      style={{
        backgroundColor: '#fff',
        border: '1px solid #e0e0e0',
        borderRadius: 12,
        padding: 16,
      }}
    >
      {/* 标题 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 12,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 18 }}>🗳️</span>
          <span style={{ fontSize: 14, fontWeight: 600, color: '#333' }}>BFT Consensus</span>
        </div>
        <span
          style={{
            fontSize: 10,
            padding: '3px 8px',
            borderRadius: 10,
            backgroundColor: vote.status === 'active' ? '#e3f2fd' : '#f5f5f5',
            color: vote.status === 'active' ? '#1976D2' : '#666',
          }}
        >
          {vote.status.toUpperCase()}
        </span>
      </div>

      {/* 提案信息 */}
      <div style={{ marginBottom: 16 }}>
        <div style={{ fontSize: 13, fontWeight: 500, color: '#333', marginBottom: 4 }}>
          {vote.proposal}
        </div>
        <div style={{ fontSize: 11, color: '#666' }}>{vote.description}</div>
      </div>

      {/* 进度显示 */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 20 }}>
        {/* 环形图 */}
        <div style={{ position: 'relative' }}>
          <VotingRing progress={progress} />
          <div
            style={{
              position: 'absolute',
              top: '50%',
              left: '50%',
              transform: 'translate(-50%, -50%)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 16, fontWeight: 600, color: '#333' }}>
              {approvedCount}/{vote.requiredApprovals}
            </div>
            <div style={{ fontSize: 9, color: '#999' }}>Required</div>
          </div>
        </div>

        {/* 投票详情 */}
        <div style={{ flex: 1 }}>
          <div style={{ display: 'flex', gap: 16, marginBottom: 12 }}>
            <div>
              <div style={{ fontSize: 18, fontWeight: 600, color: '#4CAF50' }}>{approvedCount}</div>
              <div style={{ fontSize: 10, color: '#666' }}>Approved</div>
            </div>
            <div>
              <div style={{ fontSize: 18, fontWeight: 600, color: '#F44336' }}>{rejectedCount}</div>
              <div style={{ fontSize: 10, color: '#666' }}>Rejected</div>
            </div>
            <div>
              <div style={{ fontSize: 18, fontWeight: 600, color: '#9E9E9E' }}>
                {vote.votes.filter((v) => v.status === 'pending').length}
              </div>
              <div style={{ fontSize: 10, color: '#666' }}>Pending</div>
            </div>
          </div>

          {/* 投票者列表 */}
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {vote.votes.map((v) => {
              const config = VOTE_STATUS_CONFIG[v.status];
              return (
                <span
                  key={v.agentId}
                  style={{
                    fontSize: 10,
                    padding: '2px 6px',
                    borderRadius: 4,
                    backgroundColor: `${config.color}20`,
                    color: config.color,
                  }}
                  title={v.reason || v.agentName}
                >
                  {config.icon} {v.agentName}
                </span>
              );
            })}
          </div>
        </div>
      </div>

      {/* 操作按钮 */}
      {vote.status === 'active' && onVoteAction && (
        <div
          style={{
            display: 'flex',
            gap: 8,
            marginTop: 16,
            paddingTop: 12,
            borderTop: '1px solid #e0e0e0',
          }}
        >
          <button
            onClick={() => onVoteAction(vote.id, 'approve')}
            style={{
              flex: 1,
              padding: '8px 16px',
              fontSize: 12,
              fontWeight: 500,
              border: 'none',
              borderRadius: 6,
              backgroundColor: '#4CAF50',
              color: '#fff',
              cursor: 'pointer',
            }}
          >
            ✅ Approve
          </button>
          <button
            onClick={() => onVoteAction(vote.id, 'reject')}
            style={{
              flex: 1,
              padding: '8px 16px',
              fontSize: 12,
              fontWeight: 500,
              border: 'none',
              borderRadius: 6,
              backgroundColor: '#F44336',
              color: '#fff',
              cursor: 'pointer',
            }}
          >
            ❌ Reject
          </button>
        </div>
      )}
    </div>
  );
};

/**
 * 多智能体协作看板
 */
export const AgentBoard: React.FC<AgentBoardProps> = ({
  agents,
  blackboard,
  currentVote,
  onAgentSelect,
  onAgentToggleExpand,
  onBlackboardEntryClick,
  onVoteAction,
  className,
  style,
}) => {
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);

  const handleAgentSelect = (agentId: string) => {
    setSelectedAgentId(agentId);
    onAgentSelect?.(agentId);
  };

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        backgroundColor: '#f5f5f5',
        ...style,
      }}
    >
      {/* 顶部：Agent卡片 */}
      <div
        style={{
          padding: 16,
          backgroundColor: '#fff',
          borderBottom: '1px solid #e0e0e0',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            marginBottom: 12,
          }}
        >
          <span style={{ fontSize: 18 }}>🤖</span>
          <span style={{ fontSize: 14, fontWeight: 600, color: '#333' }}>Active Agents</span>
          <span style={{ fontSize: 12, color: '#666' }}>({agents.length})</span>
        </div>

        <div
          style={{
            display: 'flex',
            gap: 12,
            overflowX: 'auto',
            paddingBottom: 8,
          }}
        >
          {agents.length === 0 ? (
            <div style={{ color: '#999', fontSize: 13, padding: 20 }}>No active agents</div>
          ) : (
            agents.map((agent) => (
              <AgentCard
                key={agent.id}
                agent={agent}
                isSelected={selectedAgentId === agent.id}
                onSelect={handleAgentSelect}
                onToggleExpand={onAgentToggleExpand}
              />
            ))
          )}
        </div>
      </div>

      {/* 中间：黑板区域 */}
      <div style={{ flex: 1, padding: 16, overflow: 'hidden', display: 'flex' }}>
        <BlackboardArea entries={blackboard} onEntryClick={onBlackboardEntryClick} />
      </div>

      {/* 底部：投票进度 */}
      {currentVote && (
        <div style={{ padding: 16, paddingTop: 0 }}>
          <VotingProgress vote={currentVote} onVoteAction={onVoteAction} />
        </div>
      )}

      {/* CSS动画 */}
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.5; }
        }
      `}</style>
    </div>
  );
};

export default AgentBoard;
