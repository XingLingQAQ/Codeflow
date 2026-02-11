/**
 * AST 上下文构建器类型定义
 * 支持 Tree-sitter 解析引擎
 */
/**
 * 支持的语言
 */
export type SupportedLanguage = 'javascript' | 'typescript' | 'python' | 'rust' | 'go' | 'java' | 'c' | 'cpp' | 'csharp' | 'ruby' | 'php' | 'swift' | 'kotlin' | 'scala' | 'html' | 'css' | 'json' | 'yaml' | 'markdown';
/**
 * AST 节点类型
 */
export interface ASTNode {
    id: string;
    type: string;
    text: string;
    startPosition: Position;
    endPosition: Position;
    startIndex: number;
    endIndex: number;
    children: ASTNode[];
    parent?: ASTNode;
    isNamed: boolean;
    fieldName?: string;
}
/**
 * 位置信息
 */
export interface Position {
    row: number;
    column: number;
}
/**
 * 解析结果
 */
export interface ParseResult {
    success: boolean;
    language: SupportedLanguage;
    rootNode: ASTNode | null;
    errors: ParseError[];
    parseTime: number;
}
/**
 * 解析错误
 */
export interface ParseError {
    message: string;
    position: Position;
    type: 'syntax' | 'semantic' | 'unknown';
}
/**
 * 上下文选择
 */
export interface ContextSelection {
    nodes: ASTNode[];
    text: string;
    startPosition: Position;
    endPosition: Position;
    tokenCount: number;
}
/**
 * 上下文构建选项
 */
export interface ContextBuildOptions {
    maxTokens: number;
    includeComments: boolean;
    includeImports: boolean;
    expandToFunction: boolean;
    expandToClass: boolean;
    includeRelatedSymbols: boolean;
}
/**
 * 符号信息
 */
export interface SymbolInfo {
    name: string;
    kind: SymbolKind;
    position: Position;
    range: {
        start: Position;
        end: Position;
    };
    children?: SymbolInfo[];
}
/**
 * 符号类型
 */
export type SymbolKind = 'function' | 'method' | 'class' | 'interface' | 'variable' | 'constant' | 'property' | 'parameter' | 'import' | 'export' | 'type' | 'enum' | 'module' | 'namespace';
/**
 * AST 解析器接口
 */
export interface IASTParser {
    parse(code: string, language: SupportedLanguage): Promise<ParseResult>;
    getLanguage(filename: string): SupportedLanguage | null;
    isSupported(language: string): boolean;
}
/**
 * 上下文构建器接口
 */
export interface IContextBuilder {
    buildContext(code: string, language: SupportedLanguage, selection: {
        start: Position;
        end: Position;
    }, options?: Partial<ContextBuildOptions>): Promise<ContextSelection>;
    extractSymbols(code: string, language: SupportedLanguage): Promise<SymbolInfo[]>;
    findNodeAtPosition(rootNode: ASTNode, position: Position): ASTNode | null;
    getNodePath(node: ASTNode): ASTNode[];
}
/**
 * 语言配置
 */
export interface LanguageConfig {
    language: SupportedLanguage;
    extensions: string[];
    commentPatterns: {
        line?: string;
        blockStart?: string;
        blockEnd?: string;
    };
    importPatterns: string[];
    functionPatterns: string[];
    classPatterns: string[];
}
/**
 * 默认上下文构建选项
 */
export declare const DEFAULT_CONTEXT_OPTIONS: ContextBuildOptions;
/**
 * 语言配置映射
 */
export declare const LANGUAGE_CONFIGS: Record<SupportedLanguage, LanguageConfig>;
/**
 * 根据文件扩展名获取语言
 */
export declare function getLanguageFromExtension(filename: string): SupportedLanguage | null;
//# sourceMappingURL=types.d.ts.map