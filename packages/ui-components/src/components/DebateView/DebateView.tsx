/**
 * 辩论式校验界面组件
 * Generator与Critic对抗模式可视化
 */

import React, { useState, useMemo } from 'react';
import {
  DebateViewProps,
  DebateBubbleProps,
  DebateTimelineProps,
  ConflictPanelProps,
  DebateMessage,
  DebateRound,
  ConflictPoint,
  DEBATE_ROLE_CONFIG,
  ROUND_STATUS_CONFIG,
  CONFLICT_SEVERITY_CONFIG,
  formatDebateTime,
  countConflicts,
} from './types';

/**
 * 高亮冲突文本
 */
const HighlightedText: React.FC<{
  text: string;
  conflicts?: ConflictPoint[];
  onConflictClick?: (conflictId: string) => void;
}> = ({ text, conflicts, onConflictClick }) => {
  if (!conflicts || conflicts.length === 0) {
    return <span>{text}</span>;
  }

  const sortedConflicts = [...conflicts].sort((a, b) => a.position.start - b.position.start);
  const parts: React.ReactNode[] = [];
  let lastEnd = 0;

  sortedConflicts.forEach((conflict, index) => {
    if (conflict.position.start > lastEnd) {
      parts.push(<span key={`text-${index}`}>{text.slice(lastEnd, conflict.position.start)}</span>);
    }

    const config = CONFLICT_SEVERITY_CONFIG[conflict.severity];
    parts.push(
      <span
        key={`conflict-${conflict.id}`}
        style={{
          backgroundColor: `${config.color}30`,
          borderBottom: `2px solid ${config.color}`,
          cursor: 'pointer',
          position: 'relative',
        }}
        onClick={() => onConflictClick?.(conflict.id)}
        title={conflict.description}
      >
        {text.slice(conflict.position.start, conflict.position.end)}
        <span
          style={{
            position: 'absolute',
            top: -8,
            right: -4,
            fontSize: 10,
          }}
        >
          {config.icon}
        </span>
      </span>
    );

    lastEnd = conflict.position.end;
  });

  if (lastEnd < text.length) {
    parts.push(<span key="text-end">{text.slice(lastEnd)}</span>);
  }

  return <>{parts}</>;
};

/**
 * 辩论消息气泡
 */
export const DebateBubble: React.FC<DebateBubbleProps> = ({ message, onConflictClick }) => {
  const config = DEBATE_ROLE_CONFIG[message.role];
  const isGenerator = message.role === 'generator';

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: isGenerator ? 'row' : 'row-reverse',
        gap: 12,
        marginBottom: 16,
      }}
    >
      {/* 头像 */}
      <div
        style={{
          width: 40,
          height: 40,
          borderRadius: '50%',
          backgroundColor: `${config.color}20`,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 20,
          flexShrink: 0,
        }}
      >
        {config.icon}
      </div>

      {/* 消息内容 */}
      <div
        style={{
          flex: 1,
          maxWidth: '80%',
        }}
      >
        {/* 头部 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            marginBottom: 6,
            flexDirection: isGenerator ? 'row' : 'row-reverse',
          }}
        >
          <span style={{ fontSize: 12, fontWeight: 600, color: config.color }}>
            {config.label}
          </span>
          <span style={{ fontSize: 10, color: '#999' }}>
            {formatDebateTime(message.timestamp)}
          </span>
          {message.conflicts && message.conflicts.length > 0 && (
            <span
              style={{
                fontSize: 10,
                padding: '2px 6px',
                borderRadius: 10,
                backgroundColor: '#ffebee',
                color: '#F44336',
              }}
            >
              {message.conflicts.length} conflicts
            </span>
          )}
        </div>

        {/* 气泡 */}
        <div
          style={{
            padding: '12px 16px',
            backgroundColor: isGenerator ? '#e3f2fd' : '#fff3e0',
            borderRadius: isGenerator ? '4px 16px 16px 16px' : '16px 4px 16px 16px',
            border: `1px solid ${isGenerator ? '#bbdefb' : '#ffe0b2'}`,
            fontSize: 13,
            lineHeight: 1.6,
            color: '#333',
          }}
        >
          <HighlightedText
            text={message.content}
            conflicts={message.conflicts}
            onConflictClick={onConflictClick}
          />
        </div>

        {/* 精炼标记 */}
        {message.refinedFrom && (
          <div
            style={{
              marginTop: 6,
              fontSize: 10,
              color: '#9C27B0',
              display: 'flex',
              alignItems: 'center',
              gap: 4,
            }}
          >
            <span>🔄</span>
            <span>Refined version</span>
          </div>
        )}
      </div>
    </div>
  );
};

/**
 * 辩论时间轴
 */
export const DebateTimeline: React.FC<DebateTimelineProps> = ({
  rounds,
  currentRoundIndex,
  onRoundSelect,
}) => {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 4,
        padding: '12px 16px',
        backgroundColor: '#fafafa',
        borderRadius: 8,
        overflowX: 'auto',
      }}
    >
      {rounds.map((round, index) => {
        const statusConfig = ROUND_STATUS_CONFIG[round.status];
        const isActive = index === currentRoundIndex;
        const isPast = index < currentRoundIndex;

        return (
          <React.Fragment key={round.id}>
            {/* 轮次节点 */}
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                cursor: 'pointer',
              }}
              onClick={() => onRoundSelect?.(index)}
            >
              <div
                style={{
                  width: 32,
                  height: 32,
                  borderRadius: '50%',
                  backgroundColor: isActive ? statusConfig.color : isPast ? '#4CAF50' : '#e0e0e0',
                  color: '#fff',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 12,
                  fontWeight: 600,
                  border: isActive ? `3px solid ${statusConfig.color}40` : 'none',
                }}
              >
                {isPast ? '✓' : index + 1}
              </div>
              <div
                style={{
                  marginTop: 4,
                  fontSize: 10,
                  color: isActive ? statusConfig.color : '#666',
                  fontWeight: isActive ? 600 : 400,
                }}
              >
                Round {index + 1}
              </div>
            </div>

            {/* 连接线 */}
            {index < rounds.length - 1 && (
              <div
                style={{
                  width: 40,
                  height: 2,
                  backgroundColor: isPast ? '#4CAF50' : '#e0e0e0',
                  marginBottom: 20,
                }}
              />
            )}
          </React.Fragment>
        );
      })}
    </div>
  );
};

/**
 * 冲突详情面板
 */
export const ConflictPanel: React.FC<ConflictPanelProps> = ({ conflict, onResolve, onClose }) => {
  const [resolution, setResolution] = useState(conflict.resolution || '');
  const severityConfig = CONFLICT_SEVERITY_CONFIG[conflict.severity];

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        right: 0,
        width: 400,
        height: '100%',
        backgroundColor: '#fff',
        boxShadow: '-4px 0 20px rgba(0,0,0,0.1)',
        zIndex: 1000,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {/* 头部 */}
      <div
        style={{
          padding: '16px 20px',
          borderBottom: '1px solid #e0e0e0',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 18 }}>{severityConfig.icon}</span>
          <span style={{ fontSize: 14, fontWeight: 600, color: '#333' }}>Conflict Details</span>
          <span
            style={{
              fontSize: 10,
              padding: '2px 8px',
              borderRadius: 10,
              backgroundColor: `${severityConfig.color}20`,
              color: severityConfig.color,
            }}
          >
            {severityConfig.label}
          </span>
        </div>
        <button
          onClick={onClose}
          style={{
            background: 'none',
            border: 'none',
            fontSize: 20,
            color: '#666',
            cursor: 'pointer',
          }}
        >
          ×
        </button>
      </div>

      {/* 内容 */}
      <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
        {/* 描述 */}
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }}>
            Description
          </div>
          <div style={{ fontSize: 13, color: '#333', lineHeight: 1.5 }}>{conflict.description}</div>
        </div>

        {/* Generator观点 */}
        <div style={{ marginBottom: 20 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 500,
              color: DEBATE_ROLE_CONFIG.generator.color,
              marginBottom: 6,
              display: 'flex',
              alignItems: 'center',
              gap: 4,
            }}
          >
            {DEBATE_ROLE_CONFIG.generator.icon} Generator's View
          </div>
          <div
            style={{
              padding: 12,
              backgroundColor: '#e3f2fd',
              borderRadius: 8,
              fontSize: 13,
              color: '#333',
              lineHeight: 1.5,
            }}
          >
            {conflict.generatorText}
          </div>
        </div>

        {/* Critic观点 */}
        <div style={{ marginBottom: 20 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 500,
              color: DEBATE_ROLE_CONFIG.critic.color,
              marginBottom: 6,
              display: 'flex',
              alignItems: 'center',
              gap: 4,
            }}
          >
            {DEBATE_ROLE_CONFIG.critic.icon} Critic's View
          </div>
          <div
            style={{
              padding: 12,
              backgroundColor: '#fff3e0',
              borderRadius: 8,
              fontSize: 13,
              color: '#333',
              lineHeight: 1.5,
            }}
          >
            {conflict.criticText}
          </div>
        </div>

        {/* 解决方案 */}
        <div>
          <div style={{ fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }}>
            Resolution
          </div>
          {conflict.resolved ? (
            <div
              style={{
                padding: 12,
                backgroundColor: '#e8f5e9',
                borderRadius: 8,
                fontSize: 13,
                color: '#2e7d32',
                lineHeight: 1.5,
              }}
            >
              ✅ {conflict.resolution}
            </div>
          ) : (
            <>
              <textarea
                value={resolution}
                onChange={(e) => setResolution(e.target.value)}
                placeholder="Enter resolution..."
                style={{
                  width: '100%',
                  minHeight: 100,
                  padding: 12,
                  border: '1px solid #e0e0e0',
                  borderRadius: 8,
                  fontSize: 13,
                  resize: 'vertical',
                }}
              />
              <button
                onClick={() => onResolve?.(conflict.id, resolution)}
                disabled={!resolution.trim()}
                style={{
                  marginTop: 12,
                  width: '100%',
                  padding: '10px 16px',
                  fontSize: 13,
                  fontWeight: 500,
                  border: 'none',
                  borderRadius: 8,
                  backgroundColor: resolution.trim() ? '#4CAF50' : '#e0e0e0',
                  color: '#fff',
                  cursor: resolution.trim() ? 'pointer' : 'not-allowed',
                }}
              >
                Mark as Resolved
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

/**
 * 辩论式校验界面
 */
export const DebateView: React.FC<DebateViewProps> = ({
  session,
  onSelectSolution,
  onExportReport,
  onConflictResolve,
  className,
  style,
}) => {
  const [selectedRoundIndex, setSelectedRoundIndex] = useState(session.currentRoundIndex);
  const [selectedConflict, setSelectedConflict] = useState<ConflictPoint | null>(null);

  const currentRound = session.rounds[selectedRoundIndex];
  const conflictStats = useMemo(() => countConflicts(session), [session]);

  const handleConflictClick = (conflictId: string) => {
    const conflict = session.rounds
      .flatMap((r) => [
        ...(r.generatorMessage?.conflicts || []),
        ...(r.criticMessage?.conflicts || []),
      ])
      .find((c) => c.id === conflictId);
    if (conflict) {
      setSelectedConflict(conflict);
    }
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
      {/* 头部 */}
      <div
        style={{
          padding: '16px 20px',
          backgroundColor: '#fff',
          borderBottom: '1px solid #e0e0e0',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: 12,
          }}
        >
          <div>
            <div style={{ fontSize: 16, fontWeight: 600, color: '#333' }}>{session.topic}</div>
            <div style={{ fontSize: 12, color: '#666', marginTop: 4 }}>{session.description}</div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              onClick={onExportReport}
              style={{
                padding: '8px 16px',
                fontSize: 12,
                border: '1px solid #e0e0e0',
                borderRadius: 6,
                backgroundColor: '#fff',
                color: '#333',
                cursor: 'pointer',
              }}
            >
              📄 Export Report
            </button>
          </div>
        </div>

        {/* 统计信息 */}
        <div style={{ display: 'flex', gap: 20 }}>
          <div>
            <span style={{ fontSize: 11, color: '#666' }}>Rounds: </span>
            <span style={{ fontSize: 13, fontWeight: 600, color: '#333' }}>
              {session.rounds.length}
            </span>
          </div>
          <div>
            <span style={{ fontSize: 11, color: '#666' }}>Conflicts: </span>
            <span style={{ fontSize: 13, fontWeight: 600, color: '#F44336' }}>
              {conflictStats.total}
            </span>
            <span style={{ fontSize: 11, color: '#4CAF50', marginLeft: 4 }}>
              ({conflictStats.resolved} resolved)
            </span>
          </div>
          <div>
            <span style={{ fontSize: 11, color: '#666' }}>Status: </span>
            <span
              style={{
                fontSize: 11,
                padding: '2px 8px',
                borderRadius: 10,
                backgroundColor:
                  session.status === 'active'
                    ? '#e3f2fd'
                    : session.status === 'completed'
                    ? '#e8f5e9'
                    : '#f5f5f5',
                color:
                  session.status === 'active'
                    ? '#1976D2'
                    : session.status === 'completed'
                    ? '#2e7d32'
                    : '#666',
              }}
            >
              {session.status.toUpperCase()}
            </span>
          </div>
        </div>
      </div>

      {/* 时间轴 */}
      <div style={{ padding: '12px 20px', backgroundColor: '#fff' }}>
        <DebateTimeline
          rounds={session.rounds}
          currentRoundIndex={selectedRoundIndex}
          onRoundSelect={setSelectedRoundIndex}
        />
      </div>

      {/* 主内容区：左右分栏 */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Generator侧 */}
        <div
          style={{
            flex: 1,
            borderRight: '1px solid #e0e0e0',
            display: 'flex',
            flexDirection: 'column',
          }}
        >
          <div
            style={{
              padding: '12px 20px',
              backgroundColor: `${DEBATE_ROLE_CONFIG.generator.color}10`,
              borderBottom: '1px solid #e0e0e0',
              display: 'flex',
              alignItems: 'center',
              gap: 8,
            }}
          >
            <span style={{ fontSize: 18 }}>{DEBATE_ROLE_CONFIG.generator.icon}</span>
            <span
              style={{
                fontSize: 14,
                fontWeight: 600,
                color: DEBATE_ROLE_CONFIG.generator.color,
              }}
            >
              Generator
            </span>
          </div>
          <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
            {currentRound?.generatorMessage ? (
              <>
                <DebateBubble
                  message={currentRound.generatorMessage}
                  onConflictClick={handleConflictClick}
                />
                {currentRound.refinedMessage && (
                  <DebateBubble
                    message={currentRound.refinedMessage}
                    onConflictClick={handleConflictClick}
                  />
                )}
              </>
            ) : (
              <div style={{ textAlign: 'center', color: '#999', padding: 40 }}>
                Waiting for generation...
              </div>
            )}

            {/* 选择方案按钮 */}
            {currentRound?.status === 'completed' && currentRound.generatorMessage && (
              <button
                onClick={() =>
                  onSelectSolution?.(currentRound.id, currentRound.generatorMessage!.id)
                }
                style={{
                  width: '100%',
                  padding: '10px 16px',
                  marginTop: 12,
                  fontSize: 12,
                  fontWeight: 500,
                  border: `2px solid ${DEBATE_ROLE_CONFIG.generator.color}`,
                  borderRadius: 8,
                  backgroundColor:
                    session.selectedSolution === currentRound.generatorMessage.id
                      ? DEBATE_ROLE_CONFIG.generator.color
                      : '#fff',
                  color:
                    session.selectedSolution === currentRound.generatorMessage.id
                      ? '#fff'
                      : DEBATE_ROLE_CONFIG.generator.color,
                  cursor: 'pointer',
                }}
              >
                {session.selectedSolution === currentRound.generatorMessage.id
                  ? '✓ Selected'
                  : 'Select This Solution'}
              </button>
            )}
          </div>
        </div>

        {/* Critic侧 */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <div
            style={{
              padding: '12px 20px',
              backgroundColor: `${DEBATE_ROLE_CONFIG.critic.color}10`,
              borderBottom: '1px solid #e0e0e0',
              display: 'flex',
              alignItems: 'center',
              gap: 8,
            }}
          >
            <span style={{ fontSize: 18 }}>{DEBATE_ROLE_CONFIG.critic.icon}</span>
            <span
              style={{
                fontSize: 14,
                fontWeight: 600,
                color: DEBATE_ROLE_CONFIG.critic.color,
              }}
            >
              Critic
            </span>
          </div>
          <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
            {currentRound?.criticMessage ? (
              <DebateBubble
                message={currentRound.criticMessage}
                onConflictClick={handleConflictClick}
              />
            ) : (
              <div style={{ textAlign: 'center', color: '#999', padding: 40 }}>
                Waiting for critique...
              </div>
            )}
          </div>
        </div>
      </div>

      {/* 冲突详情面板 */}
      {selectedConflict && (
        <ConflictPanel
          conflict={selectedConflict}
          onResolve={onConflictResolve}
          onClose={() => setSelectedConflict(null)}
        />
      )}
    </div>
  );
};

export default DebateView;
