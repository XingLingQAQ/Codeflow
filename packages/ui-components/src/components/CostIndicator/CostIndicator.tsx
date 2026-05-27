/**
 * CostIndicator - 成本显示组件
 * 实时显示当前会话成本，支持预算告警和成本明细
 */

import React, { useState, useMemo } from 'react';
import {
  CostIndicatorProps,
  CostBreakdownProps,
  CostChartProps,
  BudgetAlertProps,
  formatCost,
  formatTokens,
  calculateBudgetPercentage,
  getBudgetStatus,
  PROVIDER_COLORS,
} from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';

/**
 * 成本明细组件
 */
export const CostBreakdown: React.FC<CostBreakdownProps> = ({ breakdown, total, currency }) => {
  return (
    <div style={{ padding: spacing[4] }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: spacing[3],
        }}
      >
        <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: colors.slate[500], textTransform: 'uppercase' }}>
          Cost Breakdown
        </span>
        <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
          {formatCost(total, currency)}
        </span>
      </div>

      {/* 进度条 */}
      <div
        style={{
          height: 8,
          borderRadius: borderRadius.full,
          backgroundColor: colors.slate[100],
          overflow: 'hidden',
          display: 'flex',
          marginBottom: spacing[4],
        }}
      >
        {breakdown.map((item, index) => (
          <div
            key={item.modelId}
            style={{
              width: `${item.percentage}%`,
              height: '100%',
              backgroundColor: item.color,
              transition: transitions.normal,
            }}
            title={`${item.modelName}: ${formatCost(item.cost, currency)}`}
          />
        ))}
      </div>

      {/* 明细列表 */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[2] }}>
        {breakdown.map((item) => (
          <div
            key={item.modelId}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: spacing[3],
              padding: spacing[2],
              backgroundColor: colors.slate[50],
              borderRadius: borderRadius.lg,
            }}
          >
            <div
              style={{
                width: 8,
                height: 8,
                borderRadius: '50%',
                backgroundColor: item.color,
              }}
            />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }}>
                  {item.modelName}
                </span>
                <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
                  {formatCost(item.cost, currency)}
                </span>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3], marginTop: 2 }}>
                <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
                  In: {formatTokens(item.inputTokens)}
                </span>
                <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
                  Out: {formatTokens(item.outputTokens)}
                </span>
                <span style={{ fontSize: fontSize.xs, color: colors.slate[400] }}>
                  {item.percentage.toFixed(1)}%
                </span>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

/**
 * 成本图表组件
 */
export const CostChart: React.FC<CostChartProps> = ({ history, budget, currency }) => {
  const chartData = useMemo(() => {
    if (history.length === 0) return [];

    // 累计成本
    let cumulative = 0;
    return history.map((point) => {
      cumulative += point.cost;
      return { ...point, cumulative };
    });
  }, [history]);

  const maxCost = Math.max(budget, ...chartData.map((d) => d.cumulative));
  const chartHeight = 120;
  const chartWidth = 280;

  return (
    <div style={{ padding: spacing[4] }}>
      <div style={{ marginBottom: spacing[3] }}>
        <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: colors.slate[500], textTransform: 'uppercase' }}>
          Cost Trend
        </span>
      </div>

      <svg width={chartWidth} height={chartHeight} style={{ overflow: 'visible' }}>
        {/* 预算线 */}
        <line
          x1={0}
          y1={chartHeight - (budget / maxCost) * chartHeight}
          x2={chartWidth}
          y2={chartHeight - (budget / maxCost) * chartHeight}
          stroke={colors.error.main}
          strokeWidth={1}
          strokeDasharray="4 2"
        />
        <text
          x={chartWidth - 4}
          y={chartHeight - (budget / maxCost) * chartHeight - 4}
          fontSize={9}
          fill={colors.error.main}
          textAnchor="end"
        >
          Budget
        </text>

        {/* 成本曲线 */}
        {chartData.length > 1 && (
          <path
            d={chartData
              .map((point, i) => {
                const x = (i / (chartData.length - 1)) * chartWidth;
                const y = chartHeight - (point.cumulative / maxCost) * chartHeight;
                return `${i === 0 ? 'M' : 'L'} ${x} ${y}`;
              })
              .join(' ')}
            fill="none"
            stroke={colors.primary[500]}
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        )}

        {/* 数据点 */}
        {chartData.map((point, i) => {
          const x = chartData.length > 1 ? (i / (chartData.length - 1)) * chartWidth : chartWidth / 2;
          const y = chartHeight - (point.cumulative / maxCost) * chartHeight;
          return (
            <circle
              key={i}
              cx={x}
              cy={y}
              r={3}
              fill={colors.primary[500]}
              stroke="#fff"
              strokeWidth={1.5}
            />
          );
        })}
      </svg>

      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          marginTop: spacing[2],
          fontSize: fontSize.xs,
          color: colors.slate[400],
        }}
      >
        <span>Start</span>
        <span>Now</span>
      </div>
    </div>
  );
};

/**
 * 预算告警组件
 */
export const BudgetAlert: React.FC<BudgetAlertProps> = ({
  current,
  budget,
  currency,
  onDismiss,
  onAdjustBudget,
}) => {
  const percentage = calculateBudgetPercentage(current, budget);
  const status = getBudgetStatus(percentage);

  if (status.status === 'safe') return null;

  return (
    <div
      style={{
        position: 'fixed',
        top: spacing[4],
        right: spacing[4],
        zIndex: 1000,
        maxWidth: 360,
        padding: spacing[4],
        backgroundColor: status.status === 'danger' ? colors.error.light : colors.warning.light,
        borderRadius: borderRadius.xl,
        border: `1px solid ${status.status === 'danger' ? '#fca5a5' : '#fcd34d'}`,
        boxShadow: shadows.lg,
        animation: 'slideIn 0.3s ease',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: spacing[3] }}>
        <div
          style={{
            width: 32,
            height: 32,
            borderRadius: borderRadius.lg,
            backgroundColor: status.color,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            fontSize: 16,
            flexShrink: 0,
          }}
        >
          ⚠️
        </div>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: 4 }}>
            {status.label}
          </div>
          <div style={{ fontSize: fontSize.xs, color: colors.slate[600], marginBottom: spacing[3] }}>
            You've used {formatCost(current, currency)} of your {formatCost(budget, currency)} budget ({percentage.toFixed(0)}%)
          </div>
          <div style={{ display: 'flex', gap: spacing[2] }}>
            {onAdjustBudget && (
              <button
                onClick={onAdjustBudget}
                style={{
                  padding: `${spacing[1.5]}px ${spacing[3]}px`,
                  fontSize: fontSize.xs,
                  fontWeight: fontWeight.bold,
                  borderRadius: borderRadius.lg,
                  backgroundColor: colors.slate[800],
                  color: '#fff',
                  border: 'none',
                  cursor: 'pointer',
                }}
              >
                Adjust Budget
              </button>
            )}
            {onDismiss && (
              <button
                onClick={onDismiss}
                style={{
                  padding: `${spacing[1.5]}px ${spacing[3]}px`,
                  fontSize: fontSize.xs,
                  fontWeight: fontWeight.medium,
                  borderRadius: borderRadius.lg,
                  backgroundColor: 'transparent',
                  color: colors.slate[600],
                  border: `1px solid ${colors.slate[300]}`,
                  cursor: 'pointer',
                }}
              >
                Dismiss
              </button>
            )}
          </div>
        </div>
      </div>
      <style>{`
        @keyframes slideIn {
          from {
            opacity: 0;
            transform: translateX(20px);
          }
          to {
            opacity: 1;
            transform: translateX(0);
          }
        }
      `}</style>
    </div>
  );
};

/**
 * 成本指示器主组件
 */
export const CostIndicator: React.FC<CostIndicatorProps> = ({
  cost,
  onBudgetChange,
  onAlertDismiss,
  showBreakdown = true,
  showChart = true,
  compact = false,
  className,
  style,
}) => {
  const [isExpanded, setIsExpanded] = useState(false);
  const percentage = calculateBudgetPercentage(cost.current, cost.budget);
  const status = getBudgetStatus(percentage);

  if (compact) {
    return (
      <div
        className={className}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: spacing[2],
          padding: `${spacing[1.5]}px ${spacing[3]}px`,
          backgroundColor: colors.slate[50],
          borderRadius: borderRadius.full,
          border: `1px solid ${colors.slate[200]}`,
          cursor: 'pointer',
          transition: transitions.fast,
          ...style,
        }}
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <span
          style={{
            width: 6,
            height: 6,
            borderRadius: '50%',
            backgroundColor: status.color,
          }}
        />
        <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
          {formatCost(cost.current, cost.currency)}
        </span>
      </div>
    );
  }

  return (
    <div
      className={className}
      style={{
        backgroundColor: '#fff',
        borderRadius: borderRadius['2xl'],
        border: `1px solid ${colors.slate[200]}`,
        boxShadow: shadows.sm,
        overflow: 'hidden',
        ...style,
      }}
    >
      {/* 头部 */}
      <div
        style={{
          padding: spacing[4],
          borderBottom: isExpanded ? `1px solid ${colors.slate[100]}` : 'none',
          cursor: 'pointer',
        }}
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[3] }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
            <span style={{ fontSize: 16 }}>💰</span>
            <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
              Session Cost
            </span>
          </div>
          <div
            style={{
              padding: `${spacing[0.5]}px ${spacing[2]}px`,
              borderRadius: borderRadius.full,
              backgroundColor: `${status.color}15`,
              color: status.color,
              fontSize: fontSize.xs,
              fontWeight: fontWeight.bold,
            }}
          >
            {status.label}
          </div>
        </div>

        {/* 成本显示 */}
        <div style={{ display: 'flex', alignItems: 'baseline', gap: spacing[2], marginBottom: spacing[3] }}>
          <span style={{ fontSize: 24, fontWeight: fontWeight.bold, color: colors.slate[900] }}>
            {formatCost(cost.current, cost.currency)}
          </span>
          <span style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>
            / {formatCost(cost.budget, cost.currency)}
          </span>
        </div>

        {/* 进度条 */}
        <div
          style={{
            height: 6,
            borderRadius: borderRadius.full,
            backgroundColor: colors.slate[100],
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              width: `${percentage}%`,
              height: '100%',
              backgroundColor: status.color,
              borderRadius: borderRadius.full,
              transition: transitions.normal,
            }}
          />
        </div>

        {/* 展开指示器 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            marginTop: spacing[2],
            color: colors.slate[400],
            fontSize: fontSize.xs,
          }}
        >
          <span style={{ transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)', transition: transitions.fast }}>
            ▼
          </span>
        </div>
      </div>

      {/* 展开内容 */}
      {isExpanded && (
        <>
          {showBreakdown && cost.breakdown.length > 0 && (
            <CostBreakdown breakdown={cost.breakdown} total={cost.current} currency={cost.currency} />
          )}
          {showChart && cost.history.length > 0 && (
            <div style={{ borderTop: `1px solid ${colors.slate[100]}` }}>
              <CostChart history={cost.history} budget={cost.budget} currency={cost.currency} />
            </div>
          )}
        </>
      )}

      {/* 预算告警 */}
      <BudgetAlert
        current={cost.current}
        budget={cost.budget}
        currency={cost.currency}
        onDismiss={onAlertDismiss}
        onAdjustBudget={onBudgetChange ? () => onBudgetChange(cost.budget * 1.5) : undefined}
      />
    </div>
  );
};

export default CostIndicator;
