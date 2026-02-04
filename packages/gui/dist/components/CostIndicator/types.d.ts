/**
 * CostIndicator Types - 成本显示组件类型定义
 */
export interface CostData {
    current: number;
    budget: number;
    currency: string;
    breakdown: CostBreakdown[];
    history: CostHistoryPoint[];
}
export interface CostBreakdown {
    modelId: string;
    modelName: string;
    provider: string;
    inputTokens: number;
    outputTokens: number;
    cost: number;
    percentage: number;
    color: string;
}
export interface CostHistoryPoint {
    timestamp: number;
    cost: number;
    modelId: string;
}
export interface CostIndicatorProps {
    cost: CostData;
    onBudgetChange?: (budget: number) => void;
    onAlertDismiss?: () => void;
    showBreakdown?: boolean;
    showChart?: boolean;
    compact?: boolean;
    className?: string;
    style?: React.CSSProperties;
}
export interface CostBreakdownProps {
    breakdown: CostBreakdown[];
    total: number;
    currency: string;
}
export interface CostChartProps {
    history: CostHistoryPoint[];
    budget: number;
    currency: string;
}
export interface BudgetAlertProps {
    current: number;
    budget: number;
    currency: string;
    onDismiss?: () => void;
    onAdjustBudget?: () => void;
}
export declare const formatCost: (cost: number, currency?: string) => string;
export declare const formatTokens: (tokens: number) => string;
export declare const calculateBudgetPercentage: (current: number, budget: number) => number;
export declare const getBudgetStatus: (percentage: number) => {
    status: "safe" | "warning" | "danger";
    color: string;
    label: string;
};
export declare const PROVIDER_COLORS: Record<string, string>;
export declare const MODEL_COST_CONFIG: Record<string, {
    input: number;
    output: number;
}>;
//# sourceMappingURL=types.d.ts.map