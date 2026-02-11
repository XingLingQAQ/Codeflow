export interface ShadowScaffoldConfig {
    version: string;
    projectRoot: string;
    autoSync: boolean;
    intentProjection: {
        enabled: boolean;
        languages: string[];
    };
    registry: {
        apiRegistry: boolean;
        modelDictionary: boolean;
    };
}
export declare class ShadowScaffold {
    private readonly shadowRoot;
    initialize(projectRoot: string): Promise<void>;
    private buildDefaultConfig;
    private updateGitignore;
}
//# sourceMappingURL=ShadowScaffold.d.ts.map