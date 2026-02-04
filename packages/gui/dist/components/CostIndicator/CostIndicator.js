import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
/**
 * CostIndicator - 成本显示组件
 * 实时显示当前会话成本，支持预算告警和成本明细
 */
import { useState, useMemo } from 'react';
import { formatCost, formatTokens, calculateBudgetPercentage, getBudgetStatus, } from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
/**
 * 成本明细组件
 */
export const CostBreakdown = ({ breakdown, total, currency }) => {
    return (_jsxs("div", { style: { padding: spacing[4] }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: spacing[3],
                }, children: [_jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: colors.slate[500], textTransform: 'uppercase' }, children: "Cost Breakdown" }), _jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: formatCost(total, currency) })] }), _jsx("div", { style: {
                    height: 8,
                    borderRadius: borderRadius.full,
                    backgroundColor: colors.slate[100],
                    overflow: 'hidden',
                    display: 'flex',
                    marginBottom: spacing[4],
                }, children: breakdown.map((item, index) => (_jsx("div", { style: {
                        width: `${item.percentage}%`,
                        height: '100%',
                        backgroundColor: item.color,
                        transition: transitions.normal,
                    }, title: `${item.modelName}: ${formatCost(item.cost, currency)}` }, item.modelId))) }), _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[2] }, children: breakdown.map((item) => (_jsxs("div", { style: {
                        display: 'flex',
                        alignItems: 'center',
                        gap: spacing[3],
                        padding: spacing[2],
                        backgroundColor: colors.slate[50],
                        borderRadius: borderRadius.lg,
                    }, children: [_jsx("div", { style: {
                                width: 8,
                                height: 8,
                                borderRadius: '50%',
                                backgroundColor: item.color,
                            } }), _jsxs("div", { style: { flex: 1, minWidth: 0 }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between' }, children: [_jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }, children: item.modelName }), _jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: formatCost(item.cost, currency) })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3], marginTop: 2 }, children: [_jsxs("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: ["In: ", formatTokens(item.inputTokens)] }), _jsxs("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: ["Out: ", formatTokens(item.outputTokens)] }), _jsxs("span", { style: { fontSize: fontSize.xs, color: colors.slate[400] }, children: [item.percentage.toFixed(1), "%"] })] })] })] }, item.modelId))) })] }));
};
/**
 * 成本图表组件
 */
export const CostChart = ({ history, budget, currency }) => {
    const chartData = useMemo(() => {
        if (history.length === 0)
            return [];
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
    return (_jsxs("div", { style: { padding: spacing[4] }, children: [_jsx("div", { style: { marginBottom: spacing[3] }, children: _jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: colors.slate[500], textTransform: 'uppercase' }, children: "Cost Trend" }) }), _jsxs("svg", { width: chartWidth, height: chartHeight, style: { overflow: 'visible' }, children: [_jsx("line", { x1: 0, y1: chartHeight - (budget / maxCost) * chartHeight, x2: chartWidth, y2: chartHeight - (budget / maxCost) * chartHeight, stroke: colors.error.main, strokeWidth: 1, strokeDasharray: "4 2" }), _jsx("text", { x: chartWidth - 4, y: chartHeight - (budget / maxCost) * chartHeight - 4, fontSize: 9, fill: colors.error.main, textAnchor: "end", children: "Budget" }), chartData.length > 1 && (_jsx("path", { d: chartData
                            .map((point, i) => {
                            const x = (i / (chartData.length - 1)) * chartWidth;
                            const y = chartHeight - (point.cumulative / maxCost) * chartHeight;
                            return `${i === 0 ? 'M' : 'L'} ${x} ${y}`;
                        })
                            .join(' '), fill: "none", stroke: colors.primary[500], strokeWidth: 2, strokeLinecap: "round", strokeLinejoin: "round" })), chartData.map((point, i) => {
                        const x = chartData.length > 1 ? (i / (chartData.length - 1)) * chartWidth : chartWidth / 2;
                        const y = chartHeight - (point.cumulative / maxCost) * chartHeight;
                        return (_jsx("circle", { cx: x, cy: y, r: 3, fill: colors.primary[500], stroke: "#fff", strokeWidth: 1.5 }, i));
                    })] }), _jsxs("div", { style: {
                    display: 'flex',
                    justifyContent: 'space-between',
                    marginTop: spacing[2],
                    fontSize: fontSize.xs,
                    color: colors.slate[400],
                }, children: [_jsx("span", { children: "Start" }), _jsx("span", { children: "Now" })] })] }));
};
/**
 * 预算告警组件
 */
export const BudgetAlert = ({ current, budget, currency, onDismiss, onAdjustBudget, }) => {
    const percentage = calculateBudgetPercentage(current, budget);
    const status = getBudgetStatus(percentage);
    if (status.status === 'safe')
        return null;
    return (_jsxs("div", { style: {
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
        }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'flex-start', gap: spacing[3] }, children: [_jsx("div", { style: {
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
                        }, children: "\u26A0\uFE0F" }), _jsxs("div", { style: { flex: 1 }, children: [_jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: 4 }, children: status.label }), _jsxs("div", { style: { fontSize: fontSize.xs, color: colors.slate[600], marginBottom: spacing[3] }, children: ["You've used ", formatCost(current, currency), " of your ", formatCost(budget, currency), " budget (", percentage.toFixed(0), "%)"] }), _jsxs("div", { style: { display: 'flex', gap: spacing[2] }, children: [onAdjustBudget && (_jsx("button", { onClick: onAdjustBudget, style: {
                                            padding: `${spacing[1.5]}px ${spacing[3]}px`,
                                            fontSize: fontSize.xs,
                                            fontWeight: fontWeight.bold,
                                            borderRadius: borderRadius.lg,
                                            backgroundColor: colors.slate[800],
                                            color: '#fff',
                                            border: 'none',
                                            cursor: 'pointer',
                                        }, children: "Adjust Budget" })), onDismiss && (_jsx("button", { onClick: onDismiss, style: {
                                            padding: `${spacing[1.5]}px ${spacing[3]}px`,
                                            fontSize: fontSize.xs,
                                            fontWeight: fontWeight.medium,
                                            borderRadius: borderRadius.lg,
                                            backgroundColor: 'transparent',
                                            color: colors.slate[600],
                                            border: `1px solid ${colors.slate[300]}`,
                                            cursor: 'pointer',
                                        }, children: "Dismiss" }))] })] })] }), _jsx("style", { children: `
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
      ` })] }));
};
/**
 * 成本指示器主组件
 */
export const CostIndicator = ({ cost, onBudgetChange, onAlertDismiss, showBreakdown = true, showChart = true, compact = false, className, style, }) => {
    const [isExpanded, setIsExpanded] = useState(false);
    const percentage = calculateBudgetPercentage(cost.current, cost.budget);
    const status = getBudgetStatus(percentage);
    if (compact) {
        return (_jsxs("div", { className: className, style: {
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
            }, onClick: () => setIsExpanded(!isExpanded), children: [_jsx("span", { style: {
                        width: 6,
                        height: 6,
                        borderRadius: '50%',
                        backgroundColor: status.color,
                    } }), _jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[700] }, children: formatCost(cost.current, cost.currency) })] }));
    }
    return (_jsxs("div", { className: className, style: {
            backgroundColor: '#fff',
            borderRadius: borderRadius['2xl'],
            border: `1px solid ${colors.slate[200]}`,
            boxShadow: shadows.sm,
            overflow: 'hidden',
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: spacing[4],
                    borderBottom: isExpanded ? `1px solid ${colors.slate[100]}` : 'none',
                    cursor: 'pointer',
                }, onClick: () => setIsExpanded(!isExpanded), children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[3] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("span", { style: { fontSize: 16 }, children: "\uD83D\uDCB0" }), _jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: "Session Cost" })] }), _jsx("div", { style: {
                                    padding: `${spacing[0.5]}px ${spacing[2]}px`,
                                    borderRadius: borderRadius.full,
                                    backgroundColor: `${status.color}15`,
                                    color: status.color,
                                    fontSize: fontSize.xs,
                                    fontWeight: fontWeight.bold,
                                }, children: status.label })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'baseline', gap: spacing[2], marginBottom: spacing[3] }, children: [_jsx("span", { style: { fontSize: 24, fontWeight: fontWeight.bold, color: colors.slate[900] }, children: formatCost(cost.current, cost.currency) }), _jsxs("span", { style: { fontSize: fontSize.sm, color: colors.slate[500] }, children: ["/ ", formatCost(cost.budget, cost.currency)] })] }), _jsx("div", { style: {
                            height: 6,
                            borderRadius: borderRadius.full,
                            backgroundColor: colors.slate[100],
                            overflow: 'hidden',
                        }, children: _jsx("div", { style: {
                                width: `${percentage}%`,
                                height: '100%',
                                backgroundColor: status.color,
                                borderRadius: borderRadius.full,
                                transition: transitions.normal,
                            } }) }), _jsx("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            marginTop: spacing[2],
                            color: colors.slate[400],
                            fontSize: fontSize.xs,
                        }, children: _jsx("span", { style: { transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)', transition: transitions.fast }, children: "\u25BC" }) })] }), isExpanded && (_jsxs(_Fragment, { children: [showBreakdown && cost.breakdown.length > 0 && (_jsx(CostBreakdown, { breakdown: cost.breakdown, total: cost.current, currency: cost.currency })), showChart && cost.history.length > 0 && (_jsx("div", { style: { borderTop: `1px solid ${colors.slate[100]}` }, children: _jsx(CostChart, { history: cost.history, budget: cost.budget, currency: cost.currency }) }))] })), _jsx(BudgetAlert, { current: cost.current, budget: cost.budget, currency: cost.currency, onDismiss: onAlertDismiss, onAdjustBudget: onBudgetChange ? () => onBudgetChange(cost.budget * 1.5) : undefined })] }));
};
export default CostIndicator;
//# sourceMappingURL=CostIndicator.js.map