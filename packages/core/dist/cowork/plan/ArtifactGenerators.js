/**
 * ArtifactGenerators - OpenSpec 风格的工件生成器
 * 支持 AI 驱动的内容生成
 */
import { EventEmitter } from 'events';
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
    language: 'en',
    maxTokens: 2000,
    temperature: 0.7,
};
/**
 * ProposalGenerator - 提案生成器
 */
export class ProposalGenerator extends EventEmitter {
    constructor(config = {}, aiGenerate) {
        super();
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.aiGenerate = aiGenerate;
    }
    /**
     * 生成提案内容
     */
    async generate(vision, constraints) {
        if (this.aiGenerate) {
            return this.generateWithAI(vision, constraints);
        }
        return this.generateStatic(vision, constraints);
    }
    /**
     * 使用 AI 生成
     */
    async generateWithAI(vision, constraints) {
        const prompt = this.buildProposalPrompt(vision, constraints);
        const response = await this.aiGenerate(prompt, this.config);
        return this.parseProposalResponse(response, vision, constraints);
    }
    /**
     * 静态生成（无 AI）
     */
    generateStatic(vision, constraints) {
        const mustConstraints = constraints.constraints.filter(c => c.priority === 'must');
        const technicalConstraints = constraints.constraints.filter(c => c.type === 'technical');
        return {
            why: vision.summary,
            what: vision.goals.join('\n'),
            how: this.generateHowSection(mustConstraints, technicalConstraints),
            impact: this.generateImpactSection(vision),
            risks: vision.risks,
            alternatives: [],
        };
    }
    /**
     * 构建提案 Prompt
     */
    buildProposalPrompt(vision, constraints) {
        const lang = this.config.language === 'zh' ? 'Chinese' : 'English';
        return `Generate a project proposal in ${lang} based on the following vision and constraints.

Vision:
- Title: ${vision.title}
- Summary: ${vision.summary}
- Goals: ${vision.goals.join(', ')}
- Scope: ${vision.scope.included.join(', ')}
- Risks: ${vision.risks.join(', ')}

Constraints:
${constraints.constraints.map(c => `- [${c.priority}] ${c.description}`).join('\n')}

Generate a proposal with the following sections:
1. WHY: Why is this project needed?
2. WHAT: What will be built/changed?
3. HOW: How will it be implemented?
4. IMPACT: What is the expected impact?
5. RISKS: What are the risks and mitigations?
6. ALTERNATIVES: What alternatives were considered?

Format as JSON with keys: why, what, how, impact, risks (array), alternatives (array)`;
    }
    /**
     * 解析 AI 响应
     */
    parseProposalResponse(response, vision, constraints) {
        try {
            const parsed = JSON.parse(response);
            return {
                why: parsed.why || vision.summary,
                what: parsed.what || vision.goals.join('\n'),
                how: parsed.how || '',
                impact: parsed.impact || '',
                risks: Array.isArray(parsed.risks) ? parsed.risks : vision.risks,
                alternatives: Array.isArray(parsed.alternatives) ? parsed.alternatives : [],
            };
        }
        catch {
            // 如果解析失败，返回静态生成的内容
            return this.generateStatic(vision, constraints);
        }
    }
    generateHowSection(mustConstraints, technicalConstraints) {
        let how = 'Implementation approach:\n\n';
        if (mustConstraints.length > 0) {
            how += '**Must-have requirements:**\n';
            how += mustConstraints.map(c => `- ${c.description}`).join('\n');
            how += '\n\n';
        }
        if (technicalConstraints.length > 0) {
            how += '**Technical approach:**\n';
            how += technicalConstraints.map(c => `- ${c.description}`).join('\n');
        }
        return how;
    }
    generateImpactSection(vision) {
        if (vision.scope.included.length > 0) {
            return '**Affected areas:**\n' + vision.scope.included.map(s => `- ${s}`).join('\n');
        }
        return '_Impact analysis pending_';
    }
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
}
/**
 * SpecGenerator - 规格生成器
 */
export class SpecGenerator extends EventEmitter {
    constructor(config = {}, aiGenerate) {
        super();
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.aiGenerate = aiGenerate;
    }
    /**
     * 从需求生成规格
     */
    async generate(requirement, context) {
        if (this.aiGenerate) {
            return this.generateWithAI(requirement, context);
        }
        return this.generateStatic(requirement);
    }
    /**
     * 批量生成规格
     */
    async generateBatch(requirements, context) {
        const results = [];
        for (const req of requirements) {
            const spec = await this.generate(req, context);
            results.push(spec);
        }
        return results;
    }
    /**
     * 使用 AI 生成
     */
    async generateWithAI(requirement, context) {
        const prompt = this.buildSpecPrompt(requirement, context);
        const response = await this.aiGenerate(prompt, this.config);
        return this.parseSpecResponse(response, requirement);
    }
    /**
     * 静态生成
     */
    generateStatic(requirement) {
        // 从需求文本提取场景
        const scenarios = this.extractScenarios(requirement);
        return {
            requirement,
            scenarios,
            acceptanceCriteria: scenarios.map(s => s.then),
        };
    }
    /**
     * 从需求提取场景
     */
    extractScenarios(requirement) {
        const scenarios = [];
        const sentences = requirement.split(/[.。]/).filter(s => s.trim());
        // 为每个主要需求点创建一个场景
        sentences.forEach((sentence, index) => {
            if (sentence.trim().length > 10) {
                scenarios.push({
                    id: `scenario-${index + 1}`,
                    name: `Scenario ${index + 1}`,
                    given: 'The system is in a valid state',
                    when: sentence.trim(),
                    then: `The expected behavior is achieved: ${sentence.trim()}`,
                });
            }
        });
        // 至少返回一个场景
        if (scenarios.length === 0) {
            scenarios.push({
                id: 'scenario-1',
                name: 'Main Scenario',
                given: 'The system is ready',
                when: requirement,
                then: 'The requirement is fulfilled',
            });
        }
        return scenarios;
    }
    /**
     * 构建规格 Prompt
     */
    buildSpecPrompt(requirement, context) {
        const lang = this.config.language === 'zh' ? 'Chinese' : 'English';
        let prompt = `Generate a specification in ${lang} for the following requirement using Given-When-Then format.

Requirement: ${requirement}
`;
        if (context?.vision) {
            prompt += `\nProject Context: ${context.vision.summary}`;
        }
        if (context?.constraints) {
            const relevant = context.constraints.constraints.slice(0, 5);
            prompt += `\nRelevant Constraints:\n${relevant.map(c => `- ${c.description}`).join('\n')}`;
        }
        prompt += `

Generate scenarios in JSON format:
{
  "requirement": "refined requirement statement",
  "scenarios": [
    {
      "id": "scenario-1",
      "name": "scenario name",
      "given": "precondition",
      "when": "action",
      "then": "expected result"
    }
  ],
  "acceptanceCriteria": ["criterion 1", "criterion 2"]
}`;
        return prompt;
    }
    /**
     * 解析 AI 响应
     */
    parseSpecResponse(response, requirement) {
        try {
            const parsed = JSON.parse(response);
            return {
                requirement: parsed.requirement || requirement,
                scenarios: Array.isArray(parsed.scenarios) ? parsed.scenarios : [],
                acceptanceCriteria: Array.isArray(parsed.acceptanceCriteria) ? parsed.acceptanceCriteria : [],
            };
        }
        catch {
            return this.generateStatic(requirement);
        }
    }
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
}
/**
 * DesignGenerator - 设计生成器
 */
export class DesignGenerator extends EventEmitter {
    constructor(config = {}, aiGenerate) {
        super();
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.aiGenerate = aiGenerate;
    }
    /**
     * 生成设计文档
     */
    async generate(specs, constraints) {
        if (this.aiGenerate) {
            return this.generateWithAI(specs, constraints);
        }
        return this.generateStatic(specs, constraints);
    }
    /**
     * 使用 AI 生成
     */
    async generateWithAI(specs, constraints) {
        const prompt = this.buildDesignPrompt(specs, constraints);
        const response = await this.aiGenerate(prompt, this.config);
        return this.parseDesignResponse(response, specs, constraints);
    }
    /**
     * 静态生成
     */
    generateStatic(specs, constraints) {
        // 从规格提取组件
        const components = this.extractComponents(specs);
        const interfaces = this.extractInterfaces(specs);
        const technicalConstraints = constraints.constraints.filter(c => c.type === 'technical');
        return {
            overview: this.generateOverview(specs),
            components,
            interfaces,
            dataFlow: this.generateDataFlow(components),
            dependencies: technicalConstraints.map(c => c.description),
        };
    }
    /**
     * 从规格提取组件
     */
    extractComponents(specs) {
        const components = [];
        const seen = new Set();
        specs.forEach((spec, index) => {
            const name = `Component${index + 1}`;
            if (!seen.has(name)) {
                seen.add(name);
                components.push({
                    name,
                    responsibility: spec.requirement,
                    dependencies: [],
                });
            }
        });
        return components;
    }
    /**
     * 从规格提取接口
     */
    extractInterfaces(specs) {
        const interfaces = [];
        specs.forEach((spec, index) => {
            const methods = spec.scenarios.map(s => `handle${s.name.replace(/\s+/g, '')}()`);
            interfaces.push({
                name: `IComponent${index + 1}`,
                methods,
                description: spec.requirement,
            });
        });
        return interfaces;
    }
    /**
     * 生成概述
     */
    generateOverview(specs) {
        const requirements = specs.map(s => s.requirement).join('\n- ');
        return `This design addresses the following requirements:\n- ${requirements}`;
    }
    /**
     * 生成数据流
     */
    generateDataFlow(components) {
        if (components.length === 0)
            return '_Data flow to be defined_';
        return components.map(c => c.name).join(' -> ');
    }
    /**
     * 构建设计 Prompt
     */
    buildDesignPrompt(specs, constraints) {
        const lang = this.config.language === 'zh' ? 'Chinese' : 'English';
        return `Generate a technical design document in ${lang} for the following specifications.

Specifications:
${specs.map((s, i) => `${i + 1}. ${s.requirement}`).join('\n')}

Technical Constraints:
${constraints.constraints.filter(c => c.type === 'technical').map(c => `- ${c.description}`).join('\n')}

Generate design in JSON format:
{
  "overview": "design overview",
  "components": [{"name": "ComponentName", "responsibility": "what it does", "dependencies": []}],
  "interfaces": [{"name": "IInterface", "methods": ["method1()"], "description": "interface description"}],
  "dataFlow": "data flow description",
  "dependencies": ["external dependency 1"]
}`;
    }
    /**
     * 解析 AI 响应
     */
    parseDesignResponse(response, specs, constraints) {
        try {
            const parsed = JSON.parse(response);
            return {
                overview: parsed.overview || '',
                components: Array.isArray(parsed.components) ? parsed.components : [],
                interfaces: Array.isArray(parsed.interfaces) ? parsed.interfaces : [],
                dataFlow: parsed.dataFlow || '',
                dependencies: Array.isArray(parsed.dependencies) ? parsed.dependencies : [],
            };
        }
        catch {
            return this.generateStatic(specs, constraints);
        }
    }
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
}
/**
 * TaskGenerator - 任务生成器
 */
export class TaskGenerator extends EventEmitter {
    constructor(config = {}, aiGenerate) {
        super();
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.aiGenerate = aiGenerate;
    }
    /**
     * 生成任务列表
     */
    async generate(design, specs) {
        if (this.aiGenerate) {
            return this.generateWithAI(design, specs);
        }
        return this.generateStatic(design, specs);
    }
    /**
     * 使用 AI 生成
     */
    async generateWithAI(design, specs) {
        const prompt = this.buildTaskPrompt(design, specs);
        const response = await this.aiGenerate(prompt, this.config);
        return this.parseTaskResponse(response, design, specs);
    }
    /**
     * 静态生成
     */
    generateStatic(design, specs) {
        const tasks = [];
        const dependencies = [];
        let taskIndex = 0;
        // 为每个组件创建实现任务
        design.components.forEach((component, i) => {
            tasks.push({
                id: `task-${++taskIndex}`,
                title: `Implement ${component.name}`,
                description: component.responsibility,
                priority: i === 0 ? 'high' : 'medium',
                status: 'pending',
            });
        });
        // 为每个规格创建测试任务
        specs.forEach((spec, i) => {
            const testTaskId = `task-${++taskIndex}`;
            tasks.push({
                id: testTaskId,
                title: `Test: ${spec.requirement.substring(0, 50)}...`,
                description: `Write tests for: ${spec.requirement}`,
                priority: 'medium',
                status: 'pending',
            });
            // 测试任务依赖于实现任务
            if (i < design.components.length) {
                dependencies.push({
                    taskId: testTaskId,
                    dependsOn: [`task-${i + 1}`],
                });
            }
        });
        // 添加集成任务
        tasks.push({
            id: `task-${++taskIndex}`,
            title: 'Integration and Final Testing',
            description: 'Integrate all components and perform end-to-end testing',
            priority: 'high',
            status: 'pending',
        });
        // 集成任务依赖于所有其他任务
        dependencies.push({
            taskId: `task-${taskIndex}`,
            dependsOn: tasks.slice(0, -1).map(t => t.id),
        });
        return {
            tasks,
            dependencies,
            estimatedDuration: tasks.length * 2,
        };
    }
    /**
     * 构建任务 Prompt
     */
    buildTaskPrompt(design, specs) {
        const lang = this.config.language === 'zh' ? 'Chinese' : 'English';
        return `Generate an implementation task list in ${lang} for the following design.

Design Overview: ${design.overview}

Components:
${design.components.map(c => `- ${c.name}: ${c.responsibility}`).join('\n')}

Specifications to implement:
${specs.map((s, i) => `${i + 1}. ${s.requirement}`).join('\n')}

Generate tasks in JSON format:
{
  "tasks": [
    {
      "id": "task-1",
      "title": "task title",
      "description": "detailed description",
      "priority": "high|medium|low",
      "status": "pending",
      "files": ["optional/file/paths"]
    }
  ],
  "dependencies": [
    {"taskId": "task-2", "dependsOn": ["task-1"]}
  ],
  "estimatedDuration": 10
}`;
    }
    /**
     * 解析 AI 响应
     */
    parseTaskResponse(response, design, specs) {
        try {
            const parsed = JSON.parse(response);
            return {
                tasks: Array.isArray(parsed.tasks) ? parsed.tasks : [],
                dependencies: Array.isArray(parsed.dependencies) ? parsed.dependencies : [],
                estimatedDuration: parsed.estimatedDuration || 0,
            };
        }
        catch {
            return this.generateStatic(design, specs);
        }
    }
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
}
/**
 * ArtifactGeneratorFactory - 生成器工厂
 */
export class ArtifactGeneratorFactory {
    constructor(config = {}, aiGenerate) {
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.aiGenerate = aiGenerate;
    }
    createProposalGenerator() {
        return new ProposalGenerator(this.config, this.aiGenerate);
    }
    createSpecGenerator() {
        return new SpecGenerator(this.config, this.aiGenerate);
    }
    createDesignGenerator() {
        return new DesignGenerator(this.config, this.aiGenerate);
    }
    createTaskGenerator() {
        return new TaskGenerator(this.config, this.aiGenerate);
    }
    /**
     * 一次性生成所有工件内容
     */
    async generateAll(vision, constraints, requirements) {
        const proposalGen = this.createProposalGenerator();
        const specGen = this.createSpecGenerator();
        const designGen = this.createDesignGenerator();
        const taskGen = this.createTaskGenerator();
        // 生成提案
        const proposal = await proposalGen.generate(vision, constraints);
        // 生成规格
        const reqs = requirements || vision.goals;
        const specs = await specGen.generateBatch(reqs, { vision, constraints });
        // 生成设计
        const design = await designGen.generate(specs, constraints);
        // 生成任务
        const tasks = await taskGen.generate(design, specs);
        return { proposal, specs, design, tasks };
    }
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
    setAICallback(callback) {
        this.aiGenerate = callback;
    }
}
//# sourceMappingURL=ArtifactGenerators.js.map