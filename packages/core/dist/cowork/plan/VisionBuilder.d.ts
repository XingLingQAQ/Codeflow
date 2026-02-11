/**
 * VisionBuilder - 交互式愿景构建器
 * 来自 Aha-Loop 的愿景构建能力
 */
import { EventEmitter } from 'events';
import { VisionQuestion, VisionAnswer, VisionDocument, VisionBuilderConfig } from './types.js';
/**
 * VisionBuilder - 愿景构建器
 */
export declare class VisionBuilder extends EventEmitter {
    private config;
    private questions;
    private answers;
    private currentQuestionIndex;
    private customQuestions;
    private isBuilding;
    constructor(config?: Partial<VisionBuilderConfig>);
    /**
     * 开始构建愿景
     */
    start(): VisionQuestion | null;
    /**
     * 获取当前问题
     */
    getCurrentQuestion(): VisionQuestion | null;
    /**
     * 提交回答并获取下一个问题
     */
    submitAnswer(answer: string): VisionQuestion | null;
    /**
     * 添加后续问题
     */
    private addFollowUpQuestion;
    /**
     * 添加自定义问题
     */
    addQuestion(question: Omit<VisionQuestion, 'id'>): string;
    /**
     * 跳过当前问题
     */
    skipQuestion(): VisionQuestion | null;
    /**
     * 检查是否可以完成
     */
    canComplete(): boolean;
    /**
     * 获取缺失的必填问题
     */
    getMissingRequired(): VisionQuestion[];
    /**
     * 完成愿景构建
     */
    complete(title: string): VisionDocument;
    /**
     * 从回答构建愿景文档
     */
    private buildVisionDocument;
    /**
     * 查找问题
     */
    private findQuestion;
    /**
     * 获取进度
     */
    getProgress(): {
        current: number;
        total: number;
        percentage: number;
    };
    /**
     * 获取所有回答
     */
    getAnswers(): VisionAnswer[];
    /**
     * 是否正在构建
     */
    get building(): boolean;
    /**
     * 重置
     */
    reset(): void;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<VisionBuilderConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): VisionBuilderConfig;
    /**
     * 发送事件
     */
    private emitEvent;
}
//# sourceMappingURL=VisionBuilder.d.ts.map