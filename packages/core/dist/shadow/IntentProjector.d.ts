import { ICliAdapter } from '../adapters/types.js';
import { SimpleASTParser } from '../ast/ContextBuilder.js';
export interface PublicMethodInfo {
    name: string;
    params: string[];
    returnType: string;
    docstring: string;
}
export interface ClassDefinitionInfo {
    name: string;
    extends: string;
    implements: string[];
}
export interface IntentProjectionResult {
    language: 'typescript' | 'javascript' | 'go' | 'python';
    publicMethods: PublicMethodInfo[];
    imports: string[];
    classDefinitions: ClassDefinitionInfo[];
}
export interface IntentProjectionPromptInput {
    language: string;
    publicMethods: PublicMethodInfo[];
    imports: string[];
    classDefinitions: ClassDefinitionInfo[];
}
export declare class IntentProjector {
    private readonly astParser;
    private readonly llmAdapter?;
    constructor(astParser?: SimpleASTParser, llmAdapter?: Pick<ICliAdapter, 'send'>);
    project(sourceFile: string): Promise<IntentProjectionResult>;
    projectToIntentMarkdown(sourceFile: string): Promise<string>;
    projectFromStructured(input: IntentProjectionPromptInput): Promise<string>;
    private buildIntentPrompt;
    private detectLanguage;
    private extractImports;
    private extractPublicMethods;
    private parseMethodSignature;
    private parseTSLikeMethod;
    private parseGoMethod;
    private parsePythonMethod;
    private parseParams;
    private parseGoParams;
    private extractClassDefinitions;
    private parseClassSignature;
}
//# sourceMappingURL=IntentProjector.d.ts.map