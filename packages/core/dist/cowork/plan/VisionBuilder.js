/**
 * VisionBuilder - 交互式愿景构建器
 * 来自 Aha-Loop 的愿景构建能力
 */
import { EventEmitter } from 'events';
/**
 * 默认问题模板
 */
const DEFAULT_QUESTIONS = [
    {
        id: 'goal-1',
        category: 'goal',
        question: 'What is the primary goal of this project/feature?',
        required: true,
        followUp: ['What problem does it solve?', 'Who are the target users?'],
    },
    {
        id: 'goal-2',
        category: 'goal',
        question: 'What are the success criteria?',
        required: true,
    },
    {
        id: 'scope-1',
        category: 'scope',
        question: 'What functionality should be included?',
        required: true,
    },
    {
        id: 'scope-2',
        category: 'scope',
        question: 'What is explicitly out of scope?',
        required: false,
    },
    {
        id: 'constraint-1',
        category: 'constraint',
        question: 'Are there any technical constraints or requirements?',
        required: false,
    },
    {
        id: 'constraint-2',
        category: 'constraint',
        question: 'Are there any performance or scalability requirements?',
        required: false,
    },
    {
        id: 'priority-1',
        category: 'priority',
        question: 'What are the must-have features vs nice-to-have?',
        required: true,
    },
    {
        id: 'risk-1',
        category: 'risk',
        question: 'What are the potential risks or challenges?',
        required: false,
    },
];
/**
 * 中文问题模板
 */
const CHINESE_QUESTIONS = [
    {
        id: 'goal-1',
        category: 'goal',
        question: '这个项目/功能的主要目标是什么？',
        required: true,
        followUp: ['它解决什么问题？', '目标用户是谁？'],
    },
    {
        id: 'goal-2',
        category: 'goal',
        question: '成功的标准是什么？',
        required: true,
    },
    {
        id: 'scope-1',
        category: 'scope',
        question: '应该包含哪些功能？',
        required: true,
    },
    {
        id: 'scope-2',
        category: 'scope',
        question: '哪些内容明确不在范围内？',
        required: false,
    },
    {
        id: 'constraint-1',
        category: 'constraint',
        question: '是否有技术约束或要求？',
        required: false,
    },
    {
        id: 'constraint-2',
        category: 'constraint',
        question: '是否有性能或可扩展性要求？',
        required: false,
    },
    {
        id: 'priority-1',
        category: 'priority',
        question: '哪些是必须有的功能，哪些是可选的？',
        required: true,
    },
    {
        id: 'risk-1',
        category: 'risk',
        question: '潜在的风险或挑战是什么？',
        required: false,
    },
];
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
    maxQuestions: 20,
    requireAllCategories: false,
    language: 'en',
};
/**
 * VisionBuilder - 愿景构建器
 */
export class VisionBuilder extends EventEmitter {
    constructor(config = {}) {
        super();
        this.answers = new Map();
        this.currentQuestionIndex = 0;
        this.customQuestions = [];
        this.isBuilding = false;
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.questions = this.config.language === 'zh' ? [...CHINESE_QUESTIONS] : [...DEFAULT_QUESTIONS];
    }
    /**
     * 开始构建愿景
     */
    start() {
        this.isBuilding = true;
        this.currentQuestionIndex = 0;
        this.answers.clear();
        return this.getCurrentQuestion();
    }
    /**
     * 获取当前问题
     */
    getCurrentQuestion() {
        const allQuestions = [...this.questions, ...this.customQuestions];
        if (this.currentQuestionIndex >= allQuestions.length) {
            return null;
        }
        const question = allQuestions[this.currentQuestionIndex];
        this.emitEvent({ type: 'vision:question', question });
        return question;
    }
    /**
     * 提交回答并获取下一个问题
     */
    submitAnswer(answer) {
        const currentQuestion = this.getCurrentQuestion();
        if (!currentQuestion) {
            return null;
        }
        const visionAnswer = {
            questionId: currentQuestion.id,
            answer,
            timestamp: Date.now(),
        };
        this.answers.set(currentQuestion.id, visionAnswer);
        this.emitEvent({ type: 'vision:answer', answer: visionAnswer });
        // 检查是否需要添加后续问题
        if (currentQuestion.followUp && answer.trim().length > 0) {
            for (const followUp of currentQuestion.followUp) {
                this.addFollowUpQuestion(currentQuestion.category, followUp);
            }
        }
        this.currentQuestionIndex++;
        return this.getCurrentQuestion();
    }
    /**
     * 添加后续问题
     */
    addFollowUpQuestion(category, question) {
        const id = `followup-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
        this.customQuestions.push({
            id,
            category,
            question,
            required: false,
        });
    }
    /**
     * 添加自定义问题
     */
    addQuestion(question) {
        const id = `custom-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
        this.customQuestions.push({ ...question, id });
        return id;
    }
    /**
     * 跳过当前问题
     */
    skipQuestion() {
        const currentQuestion = this.getCurrentQuestion();
        if (!currentQuestion) {
            return null;
        }
        if (currentQuestion.required) {
            throw new Error(`Cannot skip required question: ${currentQuestion.id}`);
        }
        this.currentQuestionIndex++;
        return this.getCurrentQuestion();
    }
    /**
     * 检查是否可以完成
     */
    canComplete() {
        const allQuestions = [...this.questions, ...this.customQuestions];
        const requiredQuestions = allQuestions.filter(q => q.required);
        for (const question of requiredQuestions) {
            if (!this.answers.has(question.id)) {
                return false;
            }
        }
        if (this.config.requireAllCategories) {
            const categories = new Set(allQuestions.map(q => q.category));
            for (const category of categories) {
                const categoryQuestions = allQuestions.filter(q => q.category === category);
                const hasAnswer = categoryQuestions.some(q => this.answers.has(q.id));
                if (!hasAnswer) {
                    return false;
                }
            }
        }
        return true;
    }
    /**
     * 获取缺失的必填问题
     */
    getMissingRequired() {
        const allQuestions = [...this.questions, ...this.customQuestions];
        return allQuestions.filter(q => q.required && !this.answers.has(q.id));
    }
    /**
     * 完成愿景构建
     */
    complete(title) {
        if (!this.canComplete()) {
            const missing = this.getMissingRequired();
            throw new Error(`Cannot complete: missing required questions: ${missing.map(q => q.id).join(', ')}`);
        }
        const answersArray = Array.from(this.answers.values());
        const vision = this.buildVisionDocument(title, answersArray);
        this.isBuilding = false;
        this.emitEvent({ type: 'vision:complete', vision });
        return vision;
    }
    /**
     * 从回答构建愿景文档
     */
    buildVisionDocument(title, answers) {
        const goals = [];
        const scopeIncluded = [];
        const scopeExcluded = [];
        const constraints = [];
        const priorities = [];
        const risks = [];
        for (const answer of answers) {
            const question = this.findQuestion(answer.questionId);
            if (!question || !answer.answer.trim())
                continue;
            const lines = answer.answer.split('\n').filter(l => l.trim());
            switch (question.category) {
                case 'goal':
                    goals.push(...lines);
                    break;
                case 'scope':
                    if (question.id.includes('out') || question.question.toLowerCase().includes('out of scope')) {
                        scopeExcluded.push(...lines);
                    }
                    else {
                        scopeIncluded.push(...lines);
                    }
                    break;
                case 'constraint':
                    constraints.push(...lines);
                    break;
                case 'priority':
                    priorities.push(...lines);
                    break;
                case 'risk':
                    risks.push(...lines);
                    break;
            }
        }
        const summary = goals.length > 0 ? goals[0] : title;
        return {
            id: `vision-${Date.now()}`,
            title,
            summary,
            goals,
            scope: {
                included: scopeIncluded,
                excluded: scopeExcluded,
            },
            constraints,
            priorities,
            risks,
            answers,
            createdAt: Date.now(),
            updatedAt: Date.now(),
        };
    }
    /**
     * 查找问题
     */
    findQuestion(id) {
        return [...this.questions, ...this.customQuestions].find(q => q.id === id);
    }
    /**
     * 获取进度
     */
    getProgress() {
        const total = this.questions.length + this.customQuestions.length;
        const current = this.currentQuestionIndex;
        return {
            current,
            total,
            percentage: total > 0 ? Math.round((current / total) * 100) : 0,
        };
    }
    /**
     * 获取所有回答
     */
    getAnswers() {
        return Array.from(this.answers.values());
    }
    /**
     * 是否正在构建
     */
    get building() {
        return this.isBuilding;
    }
    /**
     * 重置
     */
    reset() {
        this.answers.clear();
        this.customQuestions = [];
        this.currentQuestionIndex = 0;
        this.isBuilding = false;
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        this.config = { ...this.config, ...config };
        if (config.language) {
            this.questions = config.language === 'zh' ? [...CHINESE_QUESTIONS] : [...DEFAULT_QUESTIONS];
        }
    }
    /**
     * 获取配置
     */
    getConfig() {
        return { ...this.config };
    }
    /**
     * 发送事件
     */
    emitEvent(event) {
        this.emit('event', event);
    }
}
//# sourceMappingURL=VisionBuilder.js.map