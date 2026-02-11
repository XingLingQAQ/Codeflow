export interface ModelField {
    name: string;
    type: string;
    required: boolean;
}
export interface ModelRelationship {
    entity: string;
    dto: string;
    type: 'map' | 'extend' | 'subset' | 'transform';
}
export interface ModelEntry {
    name: string;
    fields: ModelField[];
    relationships: ModelRelationship[];
    source: string;
    tags: string[];
    similarity?: number;
}
export interface ModelDictionaryConfig {
    dictionaryPath: string;
    similarityThreshold: number;
}
interface DuplicateCheckResult {
    isDuplicate: boolean;
    similarModels: ModelEntry[];
}
export declare class ModelDictionary {
    private models;
    private relationships;
    private readonly config;
    constructor(config?: Partial<ModelDictionaryConfig>);
    register(entry: ModelEntry): Promise<DuplicateCheckResult>;
    search(query: string): ModelEntry[];
    checkDuplicate(entry: ModelEntry): DuplicateCheckResult;
    recordRelationship(entity: string, dto: string, type: ModelRelationship['type']): void;
    getRelationships(): ModelRelationship[];
    loadFromYaml(): Promise<void>;
    saveToYaml(): Promise<void>;
    getModels(): ModelEntry[];
    getModelCount(): number;
    private computeStructuralSimilarity;
    private computeFieldNameOverlap;
    private modelKeywords;
    private extractKeywords;
    private computeKeywordSimilarity;
    private parseYaml;
    private parseModelBlock;
    private parseRelationshipBlock;
    private serializeYaml;
}
export {};
//# sourceMappingURL=ModelDictionary.d.ts.map