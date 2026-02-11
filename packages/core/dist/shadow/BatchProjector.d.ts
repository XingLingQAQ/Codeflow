import { IntentProjector } from './IntentProjector.js';
export interface BatchProjectionItem {
    sourceFile: string;
    outputFile: string;
    success: boolean;
    error?: string;
}
export interface BatchProjectionResult {
    total: number;
    succeeded: number;
    failed: number;
    items: BatchProjectionItem[];
}
type ProgressReporter = (message: string) => void;
export declare class BatchProjector {
    private readonly projector;
    private readonly reportProgress;
    constructor(projector: IntentProjector, reportProgress?: ProgressReporter);
    projectDirectory(dir: string, projectRoot: string): Promise<BatchProjectionResult>;
    projectFile(filePath: string, projectRoot: string): Promise<string>;
    private resolveOutputPath;
    private collectSourceFiles;
}
export {};
//# sourceMappingURL=BatchProjector.d.ts.map