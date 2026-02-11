/**
 * 上下文预算管理器实现
 */
import { DEFAULT_CONTEXT_BUDGET, } from './types.js';
export class ContextBudgetManager {
    constructor(totalTokens = DEFAULT_CONTEXT_BUDGET.totalTokens) {
        this.budget = {
            ...DEFAULT_CONTEXT_BUDGET,
            totalTokens,
            remainingTokens: totalTokens,
        };
    }
    allocate(category, tokens) {
        if (!this.canAllocate(tokens)) {
            return false;
        }
        const allocation = this.budget.allocations.find(a => a.category === category);
        if (allocation) {
            allocation.tokens += tokens;
        }
        else {
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
    release(category, tokens) {
        const allocation = this.budget.allocations.find(a => a.category === category);
        if (allocation) {
            // 只能释放已使用的 tokens，不能超过 usedTokens
            const maxReleasable = Math.min(allocation.tokens, this.budget.usedTokens);
            const released = Math.min(maxReleasable, tokens);
            if (released > 0) {
                allocation.tokens -= released;
                this.budget.usedTokens -= released;
                this.budget.remainingTokens += released;
            }
        }
    }
    getBudget() {
        return { ...this.budget };
    }
    canAllocate(tokens) {
        return this.budget.remainingTokens >= tokens;
    }
    compress(targetTokens) {
        let compressed = 0;
        const toCompress = this.budget.usedTokens - targetTokens;
        if (toCompress <= 0)
            return 0;
        // 按优先级排序（高优先级最后压缩）
        const compressible = this.budget.allocations
            .filter(a => a.compressible && a.tokens > 0)
            .sort((a, b) => b.priority - a.priority);
        for (const allocation of compressible) {
            if (compressed >= toCompress)
                break;
            const canCompress = Math.min(allocation.tokens * 0.5, // 最多压缩 50%
            toCompress - compressed);
            allocation.tokens -= canCompress;
            compressed += canCompress;
        }
        this.budget.usedTokens -= compressed;
        this.budget.remainingTokens += compressed;
        return compressed;
    }
    reset() {
        this.budget = {
            ...DEFAULT_CONTEXT_BUDGET,
            totalTokens: this.budget.totalTokens,
            remainingTokens: this.budget.totalTokens,
        };
    }
}
//# sourceMappingURL=ContextBudgetManager.js.map