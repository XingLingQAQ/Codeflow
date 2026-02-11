/**
 * SpecLibrary - 分层规范库
 * 实现 Trellis 风格的分层规范库，支持 .codeflow/spec/ 目录结构
 */
import { EventEmitter } from 'events';
/**
 * 规范领域
 */
export type SpecDomain = 'frontend' | 'backend' | 'guides' | 'common' | 'custom';
/**
 * 规范类型
 */
export type SpecType = 'rule' | 'pattern' | 'template' | 'checklist' | 'reference';
/**
 * 规范优先级
 */
export type SpecPriority = 'critical' | 'high' | 'medium' | 'low';
/**
 * 规范元数据
 */
export interface SpecMetadata {
    id: string;
    name: string;
    domain: SpecDomain;
    type: SpecType;
    priority: SpecPriority;
    tags: string[];
    version: string;
    author?: string;
    description?: string;
    createdAt: number;
    updatedAt: number;
}
/**
 * 规范文档
 */
export interface SpecDocument {
    metadata: SpecMetadata;
    content: string;
    path: string;
    checksum?: string;
}
/**
 * 规范验证结果
 */
export interface SpecValidationResult {
    valid: boolean;
    errors: SpecValidationError[];
    warnings: SpecValidationWarning[];
}
/**
 * 验证错误
 */
export interface SpecValidationError {
    field: string;
    message: string;
    line?: number;
}
/**
 * 验证警告
 */
export interface SpecValidationWarning {
    field: string;
    message: string;
    suggestion?: string;
}
/**
 * 规范库配置
 */
export interface SpecLibraryConfig {
    rootDir: string;
    domains: SpecDomain[];
    autoReload: boolean;
    validateOnLoad: boolean;
    cacheSpecs: boolean;
}
/**
 * SpecLoader - 规范加载器
 */
export declare class SpecLoader extends EventEmitter {
    private config;
    private cache;
    private watchers;
    constructor(config?: Partial<SpecLibraryConfig>);
    /**
     * 加载单个规范
     */
    load(specPath: string): Promise<SpecDocument | null>;
    /**
     * 加载目录下所有规范
     */
    loadDirectory(dirPath: string): Promise<SpecDocument[]>;
    /**
     * 加载指定领域的所有规范
     */
    loadDomain(domain: SpecDomain): Promise<SpecDocument[]>;
    /**
     * 加载所有规范
     */
    loadAll(): Promise<SpecDocument[]>;
    /**
     * 查找规范文件
     */
    private findSpecFiles;
    /**
     * 解析元数据
     */
    private parseMetadata;
    /**
     * 提取内容（去除 frontmatter）
     */
    private extractContent;
    /**
     * 推断领域
     */
    private inferDomain;
    /**
     * 计算校验和
     */
    private calculateChecksum;
    /**
     * 启用热加载
     */
    enableHotReload(): void;
    /**
     * 监听目录变化
     */
    private watchDirectory;
    /**
     * 停止热加载
     */
    disableHotReload(): void;
    /**
     * 清除缓存
     */
    clearCache(): void;
    /**
     * 获取缓存的规范
     */
    getCachedSpecs(): SpecDocument[];
}
/**
 * SpecValidator - 规范验证器
 */
export declare class SpecValidator extends EventEmitter {
    /**
     * 验证规范文档
     */
    validate(doc: SpecDocument): SpecValidationResult;
    /**
     * 批量验证
     */
    validateBatch(docs: SpecDocument[]): Map<string, SpecValidationResult>;
    private validateMetadata;
    private validateContent;
}
/**
 * SpecLibrary - 规范库管理器
 */
export declare class SpecLibrary extends EventEmitter {
    private config;
    private loader;
    private validator;
    private specs;
    private initialized;
    constructor(config?: Partial<SpecLibraryConfig>);
    /**
     * 初始化规范库
     */
    initialize(): Promise<void>;
    /**
     * 确保目录结构存在
     */
    private ensureDirectoryStructure;
    /**
     * 获取规范
     */
    getSpec(id: string): SpecDocument | undefined;
    /**
     * 获取所有规范
     */
    getAllSpecs(): SpecDocument[];
    /**
     * 按领域获取规范
     */
    getSpecsByDomain(domain: SpecDomain): SpecDocument[];
    /**
     * 按类型获取规范
     */
    getSpecsByType(type: SpecType): SpecDocument[];
    /**
     * 按标签获取规范
     */
    getSpecsByTag(tag: string): SpecDocument[];
    /**
     * 按优先级获取规范
     */
    getSpecsByPriority(priority: SpecPriority): SpecDocument[];
    /**
     * 搜索规范
     */
    searchSpecs(query: string): SpecDocument[];
    /**
     * 添加规范
     */
    addSpec(domain: SpecDomain, name: string, content: string, metadata?: Partial<SpecMetadata>): Promise<SpecDocument>;
    /**
     * 更新规范
     */
    updateSpec(id: string, content: string, metadata?: Partial<SpecMetadata>): Promise<SpecDocument | null>;
    /**
     * 删除规范
     */
    deleteSpec(id: string): boolean;
    /**
     * 生成 frontmatter
     */
    private generateFrontmatter;
    /**
     * 重新加载所有规范
     */
    reload(): Promise<void>;
    /**
     * 获取统计信息
     */
    getStats(): {
        total: number;
        byDomain: Record<SpecDomain, number>;
        byType: Record<SpecType, number>;
        byPriority: Record<SpecPriority, number>;
    };
    /**
     * 关闭规范库
     */
    close(): void;
}
//# sourceMappingURL=SpecLibrary.d.ts.map