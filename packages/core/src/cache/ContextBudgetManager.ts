/**
 * 上下文预算管理器实现
 */

import {
  IContextBudgetManager,
  ContextBudget,
  BudgetAllocation,
  DEFAULT_CONTEXT_BUDGET,
} from './types.js';

export class ContextBudgetManager implements IContextBudgetManager {
  private budget: ContextBudget;

  constructor(totalTokens: number = DEFAULT_CONTEXT_BUDGET.totalTokens) {
    this.budget = {
      ...DEFAULT_CONTEXT_BUDGET,
      totalTokens,
      remainingTokens: totalTokens,
    };
  }

  allocate(category: BudgetAllocation['category'], tokens: number): boolean {
    if (!this.canAllocate(tokens)) {
      return false;
    }

    const allocation = this.budget.allocations.find(a => a.category === category);
    if (allocation) {
      allocation.tokens += tokens;
    } else {
      this.budget.allocations.push({
        category,
        tokens,
        priority: 2,
        compressible: true,
      });
    }

    this.budget.usedTokens += tokens;
    this.budget.remainingTokens -= tokens;
    return true;
  }

  release(category: BudgetAllocation['category'], tokens: number): void {
    const allocation = this.budget.allocations.find(a => a.category === category);
    if (allocation) {
      const released = Math.min(allocation.tokens, tokens);
      allocation.tokens -= released;
      this.budget.usedTokens -= released;
      this.budget.remainingTokens += released;
    }
  }

  getBudget(): ContextBudget {
    return { ...this.budget };
  }

  canAllocate(tokens: number): boolean {
    return this.budget.remainingTokens >= tokens;
  }

  compress(targetTokens: number): number {
    let compressed = 0;
    const toCompress = this.budget.usedTokens - targetTokens;

    if (toCompress <= 0) return 0;

    // 按优先级排序（高优先级最后压缩）
    const compressible = this.budget.allocations
      .filter(a => a.compressible && a.tokens > 0)
      .sort((a, b) => b.priority - a.priority);

    for (const allocation of compressible) {
      if (compressed >= toCompress) break;

      const canCompress = Math.min(
        allocation.tokens * 0.5, // 最多压缩 50%
        toCompress - compressed
      );

      allocation.tokens -= canCompress;
      compressed += canCompress;
    }

    this.budget.usedTokens -= compressed;
    this.budget.remainingTokens += compressed;

    return compressed;
  }

  reset(): void {
    this.budget = {
      ...DEFAULT_CONTEXT_BUDGET,
      totalTokens: this.budget.totalTokens,
      remainingTokens: this.budget.totalTokens,
    };
  }
}
