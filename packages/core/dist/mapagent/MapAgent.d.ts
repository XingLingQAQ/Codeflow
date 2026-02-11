/**
 * Map Agent 实现
 * 决策骨架提取与导图构建
 */
import { Message } from '../hooks/types.js';
import { ICliAdapter } from '../adapters/types.js';
import { IMapAgent, MapAgentConfig, ExtractionResult, CompressionMap } from './types.js';
export declare class MapAgent implements IMapAgent {
    private adapter?;
    private config;
    constructor(adapter?: ICliAdapter, config?: Partial<MapAgentConfig>);
    extract(messages: Message[]): Promise<ExtractionResult>;
    buildMap(messages: Message[], sessionId: string): Promise<CompressionMap>;
    mergeMap(existing: CompressionMap, newMap: CompressionMap): CompressionMap;
    private extractWithLLM;
    private extractLocally;
    private buildExtractionPrompt;
    private parseExtractionResponse;
    private inferEntityType;
    private calculateImportance;
    private calculateDecisionImportance;
    private inferRelationType;
}
//# sourceMappingURL=MapAgent.d.ts.map