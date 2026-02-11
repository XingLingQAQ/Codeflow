/**
 * 三元组提取器
 * 从文本中提取 S-P-O 三元组
 */
import { Triple, ITripleExtractor } from './types.js';
import { ICliAdapter } from '../adapters/types.js';
export interface TripleExtractorConfig {
    adapter?: ICliAdapter;
    minConfidence: number;
    maxTriplesPerExtraction: number;
    enableRuleBasedExtraction: boolean;
}
export declare class TripleExtractor implements ITripleExtractor {
    private config;
    private adapter?;
    constructor(config?: Partial<TripleExtractorConfig>);
    extract(text: string, context: {
        sessionId: string;
        messageIndex?: number;
        agentRole?: string;
        gitCommitHash?: string;
    }): Promise<Triple[]>;
    private extractByRules;
    private extractCodeRelations;
    private extractDecisionRelations;
    private extractFileRelations;
    private extractByLLM;
    private normalizePredicateFromLLM;
    private createTriple;
    private deduplicateTriples;
}
//# sourceMappingURL=TripleExtractor.d.ts.map