/**
 * AST 上下文构建器实现
 * 基于简化的 AST 解析（不依赖 Tree-sitter WASM）
 */
import { IASTParser, IContextBuilder, SupportedLanguage, ParseResult, ASTNode, Position, ContextSelection, ContextBuildOptions, SymbolInfo } from './types.js';
/**
 * 简化的 AST 解析器
 * 使用正则表达式进行基础解析（生产环境应集成 Tree-sitter）
 */
export declare class SimpleASTParser implements IASTParser {
    private nodeIdCounter;
    parse(code: string, language: SupportedLanguage): Promise<ParseResult>;
    getLanguage(filename: string): SupportedLanguage | null;
    isSupported(language: string): boolean;
    private parseCode;
    private createNode;
    private extractImports;
    private extractFunctions;
    private extractClasses;
    private extractBlock;
}
/**
 * 上下文构建器
 */
export declare class ContextBuilder implements IContextBuilder {
    private parser;
    constructor(parser?: IASTParser);
    buildContext(code: string, language: SupportedLanguage, selection: {
        start: Position;
        end: Position;
    }, options?: Partial<ContextBuildOptions>): Promise<ContextSelection>;
    extractSymbols(code: string, language: SupportedLanguage): Promise<SymbolInfo[]>;
    findNodeAtPosition(rootNode: ASTNode, position: Position): ASTNode | null;
    getNodePath(node: ASTNode): ASTNode[];
    private findNodesInRange;
    private isNodeInRange;
    private isPositionInNode;
    private expandToFunctions;
    private expandToClasses;
    private extractFunctionName;
    private extractClassName;
}
//# sourceMappingURL=ContextBuilder.d.ts.map