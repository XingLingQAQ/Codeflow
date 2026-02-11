/**
 * 上下文预算管理器实现
 */
import { IContextBudgetManager, ContextBudget, BudgetAllocation } from './types.js';
export declare class ContextBudgetManager implements IContextBudgetManager {
    private budget;
    constructor(totalTokens?: number);
    allocate(category: BudgetAllocation['category'], tokens: number): boolean;
    release(category: BudgetAllocation['category'], tokens: number): void;
    getBudget(): ContextBudget;
    canAllocate(tokens: number): boolean;
    compress(targetTokens: number): number;
    reset(): void;
}
//# sourceMappingURL=ContextBudgetManager.d.ts.map