/**
 * CostIndicator - 成本显示组件
 * 实时显示当前会话成本，支持预算告警和成本明细
 */
import React from 'react';
import { CostIndicatorProps, CostBreakdownProps, CostChartProps, BudgetAlertProps } from './types';
/**
 * 成本明细组件
 */
export declare const CostBreakdown: React.FC<CostBreakdownProps>;
/**
 * 成本图表组件
 */
export declare const CostChart: React.FC<CostChartProps>;
/**
 * 预算告警组件
 */
export declare const BudgetAlert: React.FC<BudgetAlertProps>;
/**
 * 成本指示器主组件
 */
export declare const CostIndicator: React.FC<CostIndicatorProps>;
export default CostIndicator;
//# sourceMappingURL=CostIndicator.d.ts.map