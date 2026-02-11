/**
 * InterfaceExtractor - MS-240 增强接口摘要
 *
 * 使用 AST 解析提取源文件中的详细接口信息，
 * 包括方法签名、参数约束、返回类型、副作用和文档注释。
 */
import { SimpleASTParser } from '../ast/ContextBuilder.js';
import { SupportedLanguage } from '../ast/types.js';
/**
 * 参数信息
 */
export interface ParameterInfo {
    name: string;
    type: string;
    required: boolean;
    defaultValue?: string;
    description?: string;
}
/**
 * 方法签名
 */
export interface MethodSignature {
    name: string;
    parameters: ParameterInfo[];
    returnType: string;
    isAsync: boolean;
    visibility: 'public' | 'private' | 'protected';
    docstring: string;
}
/**
 * 副作用类型
 */
export type SideEffectType = 'database' | 'api' | 'file' | 'network' | 'console' | 'unknown';
/**
 * 副作用信息
 */
export interface SideEffect {
    type: SideEffectType;
    description: string;
}
/**
 * 接口摘要
 */
export interface InterfaceSummary {
    name: string;
    kind: 'class' | 'interface' | 'function' | 'module';
    methods: MethodSignature[];
    properties: ParameterInfo[];
    sideEffects: SideEffect[];
    docstring: string;
    extends?: string;
    implements?: string[];
}
export declare class InterfaceExtractor {
    private readonly astParser;
    constructor(astParser?: SimpleASTParser);
    /**
     * 从源文件内容中提取接口信息
     */
    extractInterfaces(sourceCode: string, language: SupportedLanguage): Promise<InterfaceSummary[]>;
    /**
     * 从文件路径推断语言
     */
    getLanguageFromPath(filePath: string): SupportedLanguage | null;
    private extractClassSummary;
    private extractFunctionSummary;
    private extractClassName;
    private isInterfaceDeclaration;
    private extractExtends;
    private extractImplements;
    private extractDocstring;
    private extractMethods;
    private parseMethodFromMatch;
    private parseMethodSignature;
    private parseParameters;
    private splitParameters;
    private parseSingleParameter;
    private extractProperties;
    private extractFunctionName;
    /**
     * 检测代码中的副作用
     */
    private detectSideEffects;
}
//# sourceMappingURL=InterfaceExtractor.d.ts.map