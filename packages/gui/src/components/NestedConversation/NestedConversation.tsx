/**
 * 嵌套子对话渲染组件
 * 支持多层嵌套的子智能体对话框
 */

import React, { useState, useCallback } from 'react';
import {
  NestedConversationBoxProps,
  NestedConversationContainerProps,
  SubMessageProps,
  ToolCallCardProps,
  SubConversation,
  AGENT_ROLE_CONFIG,
  STATUS_CONFIG,
  MESSAGE_TYPE_CONFIG,
  MAX_NESTING_DEPTH,
  calculateIndent,
  formatDuration,
} from './types';

/**
 * 工具调用卡片
 */
export const ToolCallCard: React.FC<ToolCallCardProps> = ({ toolCall }) => {
  const [isExpanded, setIsExpanded] = useState(false);

  const statusColors: Record<string, string> = {
    pending: '#9E9E9E',
    running: '#2196F3',
    completed: '#4CAF50',
    failed: '#F44336',
  };

  return (
    <div
      style={{
        padding: '10px 12px',
        backgroundColor: '#f8f9fa',
        border: '1px solid #e0e0e0',
        borderRadius: 6,
        marginTop: 8,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          cursor: 'pointer',
        }}
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 14 }}>🔧</span>
          <span style={{ fontSize: 13, fontWeight: 500, color: '#333' }}>
            {toolCall.name}
          </span>
          <span
            style={{
              fontSize: 10,
              padding: '2px 6px',
              borderRadius: 3,
              backgroundColor: statusColors[toolCall.status],
              color: '#fff',
            }}
          >
            {toolCall.status}
          </span>
        </div>
        <span style={{ fontSize: 12, color: '#999' }}>
          {isExpanded ? '▼' : '▶'}
        </span>
      </div>

      {isExpanded && (
        <div style={{ marginTop: 10 }}>
          {/* 参数 */}
          <div style={{ marginBottom: 8 }}>
            <span style={{ fontSize: 11, color: '#666', fontWeight: 500 }}>
              Arguments:
            </span>
            <pre
              style={{
                margin: '4px 0 0 0',
                padding: 8,
                backgroundColor: '#fff',
                border: '1px solid #e0e0e0',
                borderRadius: 4,
                fontSize: 11,
                overflow: 'auto',
                maxHeight: 100,
              }}
            >
              {JSON.stringify(toolCall.arguments, null, 2)}
            </pre>
          </div>

          {/* 结果 */}
          {toolCall.result !== undefined && (
            <div>
              <span style={{ fontSize: 11, color: '#666', fontWeight: 500 }}>
                Result:
              </span>
              <pre
                style={{
                  margin: '4px 0 0 0',
                  padding: 8,
                  backgroundColor: toolCall.status === 'failed' ? '#ffebee' : '#e8f5e9',
                  border: `1px solid ${toolCall.status === 'failed' ? '#ffcdd2' : '#c8e6c9'}`,
                  borderRadius: 4,
                  fontSize: 11,
                  overflow: 'auto',
                  maxHeight: 150,
                }}
              >
                {typeof toolCall.result === 'string'
                  ? toolCall.result
                  : JSON.stringify(toolCall.result, null, 2)}
              </pre>
            </div>
          )}

          {/* 耗时 */}
          {toolCall.startTime && (
            <div style={{ marginTop: 8, fontSize: 10, color: '#999' }}>
              Duration: {formatDuration(toolCall.startTime, toolCall.endTime)}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

/**
 * 子对话消息
 */
export const SubMessage: React.FC<SubMessageProps> = ({ message, agentRole }) => {
  const config = MESSAGE_TYPE_CONFIG[message.type];
  const agentConfig = AGENT_ROLE_CONFIG[agentRole];

  return (
    <div
      style={{
        padding: '8px 12px',
        marginBottom: 6,
        backgroundColor: message.type === 'error' ? '#ffebee' : '#fff',
        borderLeft: `3px solid ${config.color}`,
        borderRadius: '0 6px 6px 0',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          marginBottom: 4,
        }}
      >
        <span style={{ fontSize: 12 }}>{config.icon}</span>
        <span style={{ fontSize: 11, color: '#666', fontWeight: 500 }}>
          {config.label}
        </span>
        <span style={{ fontSize: 10, color: '#999' }}>
          {new Date(message.timestamp).toLocaleTimeString()}
        </span>
      </div>

      {/* 消息内容 */}
      {message.type !== 'tool_call' && (
        <div
          style={{
            fontSize: 13,
            color: message.type === 'error' ? '#c62828' : '#333',
            lineHeight: 1.5,
            whiteSpace: 'pre-wrap',
          }}
        >
          {message.content}
        </div>
      )}

      {/* 工具调用 */}
      {message.toolCall && <ToolCallCard toolCall={message.toolCall} />}
    </div>
  );
};

/**
 * 嵌套对话框
 */
export const NestedConversationBox: React.FC<NestedConversationBoxProps> = ({
  conversation,
  maxDepth = MAX_NESTING_DEPTH,
  onToggleExpand,
  onStop,
  onRetry,
  className,
  style,
}) => {
  const agentConfig = AGENT_ROLE_CONFIG[conversation.agentRole];
  const statusConfig = STATUS_CONFIG[conversation.status];
  const indent = calculateIndent(conversation.depth);
  const canNest = conversation.depth < maxDepth;

  const isRunning = conversation.status === 'running';
  const isFailed = conversation.status === 'failed';

  return (
    <div
      className={className}
      style={{
        marginLeft: indent,
        marginBottom: 12,
        ...style,
      }}
    >
      {/* 对话框容器 */}
      <div
        style={{
          backgroundColor: '#fafafa',
          border: `1px solid ${agentConfig.color}20`,
          borderLeft: `4px solid ${agentConfig.color}`,
          borderRadius: '0 8px 8px 0',
          overflow: 'hidden',
        }}
      >
        {/* 头部 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '10px 14px',
            backgroundColor: `${agentConfig.color}10`,
            borderBottom: '1px solid #e0e0e0',
            cursor: 'pointer',
          }}
          onClick={() => onToggleExpand?.(conversation.id)}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {/* 展开/折叠图标 */}
            <span
              style={{
                fontSize: 10,
                color: '#666',
                transform: conversation.isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                transition: 'transform 0.15s',
              }}
            >
              ▶
            </span>

            {/* 智能体图标和名称 */}
            <span style={{ fontSize: 16 }}>{agentConfig.icon}</span>
            <span style={{ fontSize: 13, fontWeight: 600, color: '#333' }}>
              {conversation.agentName}
            </span>

            {/* 状态标签 */}
            <span
              style={{
                fontSize: 10,
                padding: '2px 8px',
                borderRadius: 10,
                backgroundColor: statusConfig.color,
                color: '#fff',
              }}
            >
              {statusConfig.label}
            </span>

            {/* 消息数量 */}
            <span style={{ fontSize: 11, color: '#999' }}>
              {conversation.messages.length} messages
            </span>
          </div>

          {/* 操作按钮 */}
          <div style={{ display: 'flex', gap: 8 }}>
            {isRunning && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onStop?.(conversation.id);
                }}
                style={{
                  padding: '4px 10px',
                  fontSize: 11,
                  border: 'none',
                  borderRadius: 4,
                  backgroundColor: '#ffebee',
                  color: '#c62828',
                  cursor: 'pointer',
                }}
              >
                ⏹ Stop
              </button>
            )}
            {isFailed && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onRetry?.(conversation.id);
                }}
                style={{
                  padding: '4px 10px',
                  fontSize: 11,
                  border: 'none',
                  borderRadius: 4,
                  backgroundColor: '#e3f2fd',
                  color: '#1976D2',
                  cursor: 'pointer',
                }}
              >
                🔄 Retry
              </button>
            )}

            {/* 耗时 */}
            <span style={{ fontSize: 11, color: '#999' }}>
              {formatDuration(conversation.startTime, conversation.endTime)}
            </span>
          </div>
        </div>

        {/* 内容区 */}
        {conversation.isExpanded && (
          <div style={{ padding: 12 }}>
            {/* 错误信息 */}
            {conversation.error && (
              <div
                style={{
                  padding: '10px 12px',
                  marginBottom: 12,
                  backgroundColor: '#ffebee',
                  border: '1px solid #ffcdd2',
                  borderRadius: 6,
                  fontSize: 12,
                  color: '#c62828',
                }}
              >
                ❌ {conversation.error}
              </div>
            )}

            {/* 消息列表 */}
            {conversation.messages.length === 0 ? (
              <div
                style={{
                  padding: 20,
                  textAlign: 'center',
                  color: '#999',
                  fontSize: 13,
                }}
              >
                {isRunning ? 'Waiting for response...' : 'No messages'}
              </div>
            ) : (
              conversation.messages.map((msg) => (
                <SubMessage
                  key={msg.id}
                  message={msg}
                  agentRole={conversation.agentRole}
                />
              ))
            )}

            {/* 子对话 */}
            {canNest && conversation.children.length > 0 && (
              <div style={{ marginTop: 12 }}>
                {conversation.children.map((child) => (
                  <NestedConversationBox
                    key={child.id}
                    conversation={child}
                    maxDepth={maxDepth}
                    onToggleExpand={onToggleExpand}
                    onStop={onStop}
                    onRetry={onRetry}
                  />
                ))}
              </div>
            )}

            {/* 深度限制提示 */}
            {!canNest && conversation.children.length > 0 && (
              <div
                style={{
                  padding: '10px 12px',
                  marginTop: 12,
                  backgroundColor: '#fff3e0',
                  border: '1px solid #ffe0b2',
                  borderRadius: 6,
                  fontSize: 12,
                  color: '#e65100',
                }}
              >
                ⚠️ {conversation.children.length} nested conversation(s) hidden (max depth: {maxDepth})
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

/**
 * 嵌套对话容器
 */
export const NestedConversationContainer: React.FC<NestedConversationContainerProps> = ({
  conversations,
  maxDepth = MAX_NESTING_DEPTH,
  onToggleExpand,
  onStop,
  onRetry,
  className,
  style,
}) => {
  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        ...style,
      }}
    >
      {conversations.length === 0 ? (
        <div
          style={{
            padding: 40,
            textAlign: 'center',
            color: '#999',
            fontSize: 13,
          }}
        >
          No sub-conversations
        </div>
      ) : (
        conversations.map((conv) => (
          <NestedConversationBox
            key={conv.id}
            conversation={conv}
            maxDepth={maxDepth}
            onToggleExpand={onToggleExpand}
            onStop={onStop}
            onRetry={onRetry}
          />
        ))
      )}
    </div>
  );
};

export default NestedConversationContainer;
