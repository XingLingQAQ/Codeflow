export interface APIRegistryEntry {
    path: string;
    method: string;
    description: string;
    handler: string;
    tags: string[];
    similarity?: number;
}
export interface APIRegistryConfig {
    registryPath: string;
    similarityThreshold: number;
}
interface DuplicateCheckResult {
    isDuplicate: boolean;
    similarEntries: APIRegistryEntry[];
}
export declare class APIRegistry {
    private entries;
    private readonly config;
    constructor(config?: Partial<APIRegistryConfig>);
    register(entry: APIRegistryEntry): Promise<DuplicateCheckResult>;
    search(query: string): APIRegistryEntry[];
    checkDuplicate(entry: APIRegistryEntry): DuplicateCheckResult;
    loadFromYaml(): Promise<void>;
    saveToYaml(): Promise<void>;
    getEntries(): APIRegistryEntry[];
    getEntryCount(): number;
    private entryKeywords;
    private extractKeywords;
    private computeSimilarity;
    private parseYaml;
    private serializeYaml;
}
export {};
//# sourceMappingURL=APIRegistry.d.ts.map