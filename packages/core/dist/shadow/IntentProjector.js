import * as fs from 'fs';
import * as path from 'path';
import { SimpleASTParser } from '../ast/ContextBuilder.js';
const SUPPORTED_LANGUAGES = ['typescript', 'javascript', 'go', 'python'];
const LANGUAGE_BY_EXTENSION = {
    '.ts': 'typescript',
    '.tsx': 'typescript',
    '.mts': 'typescript',
    '.cts': 'typescript',
    '.js': 'javascript',
    '.mjs': 'javascript',
    '.cjs': 'javascript',
    '.go': 'go',
    '.py': 'python',
};
const DEFAULT_INTENT_PROMPT = `你是代码意图投影助手。请根据输入的代码结构生成高密度意图文档。

要求：
1) 剥离实现细节，仅保留核心业务逻辑；
2) 描述数据流转方向；
3) 标注副作用（数据库/API/文件等）；
4) 输出 Markdown；
5) 输出内容需简洁且可被 AI 快速消费。`;
export class IntentProjector {
    constructor(astParser = new SimpleASTParser(), llmAdapter) {
        this.astParser = astParser;
        this.llmAdapter = llmAdapter;
    }
    async project(sourceFile) {
        const resolvedPath = path.resolve(sourceFile);
        const sourceCode = await fs.promises.readFile(resolvedPath, 'utf-8');
        const language = this.detectLanguage(resolvedPath);
        const parseResult = await this.astParser.parse(sourceCode, language);
        if (!parseResult.success || !parseResult.rootNode) {
            const parseMessage = parseResult.errors.map((error) => error.message).join('; ') || '未知解析错误';
            throw new Error(`AST 解析失败: ${path.basename(resolvedPath)} (${language}) - ${parseMessage}`);
        }
        return {
            language,
            publicMethods: this.extractPublicMethods(parseResult.rootNode.children, language),
            imports: this.extractImports(parseResult.rootNode.children),
            classDefinitions: this.extractClassDefinitions(parseResult.rootNode.children, language),
        };
    }
    async projectToIntentMarkdown(sourceFile) {
        const structured = await this.project(sourceFile);
        return this.projectFromStructured(structured);
    }
    async projectFromStructured(input) {
        if (!this.llmAdapter) {
            throw new Error('IntentProjector 未配置 llmAdapter，无法执行 LLM 意图转换');
        }
        const prompt = this.buildIntentPrompt(input);
        const response = await this.llmAdapter.send(prompt, {
            temperature: 0.3,
            maxTokens: 2000,
        });
        const markdown = (response.content || '').trim();
        if (!markdown) {
            throw new Error('LLM 未返回有效的意图文档内容');
        }
        return markdown;
    }
    buildIntentPrompt(input) {
        return `${DEFAULT_INTENT_PROMPT}\n\n代码结构(JSON)：\n${JSON.stringify(input, null, 2)}\n\n请直接输出 Markdown。`;
    }
    detectLanguage(sourceFile) {
        const ext = path.extname(sourceFile).toLowerCase();
        const language = LANGUAGE_BY_EXTENSION[ext];
        if (!language) {
            throw new Error(`不支持的文件类型: ${ext || '(无扩展名)'}，仅支持 ${SUPPORTED_LANGUAGES.join('/')}`);
        }
        return language;
    }
    extractImports(nodes) {
        const imports = nodes
            .filter((node) => node.type === 'import_statement')
            .map((node) => node.text.trim())
            .filter((text) => text.length > 0);
        return Array.from(new Set(imports));
    }
    extractPublicMethods(nodes, language) {
        const functionNodes = nodes.filter((node) => node.type === 'function_declaration');
        const methods = functionNodes
            .map((node) => this.parseMethodSignature(node.text, language))
            .filter((item) => item !== null);
        const unique = new Map();
        for (const method of methods) {
            const key = `${method.name}|${method.params.join(',')}|${method.returnType}`;
            if (!unique.has(key)) {
                unique.set(key, method);
            }
        }
        return Array.from(unique.values());
    }
    parseMethodSignature(signatureText, language) {
        const line = signatureText
            .split('\n')
            .map((part) => part.trim())
            .find((part) => part.length > 0);
        if (!line) {
            return null;
        }
        switch (language) {
            case 'typescript':
            case 'javascript':
                return this.parseTSLikeMethod(line);
            case 'go':
                return this.parseGoMethod(line);
            case 'python':
                return this.parsePythonMethod(line);
            default:
                return null;
        }
    }
    parseTSLikeMethod(line) {
        const normalized = line.replace(/\s+/g, ' ').trim();
        let matched = normalized.match(/^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(([^)]*)\)\s*(?::\s*([^\s{]+))?/);
        if (matched) {
            const [, name, rawParams, rawReturnType] = matched;
            return {
                name,
                params: this.parseParams(rawParams),
                returnType: rawReturnType?.trim() || 'unknown',
                docstring: '',
            };
        }
        matched = normalized.match(/^(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?\(([^)]*)\)\s*(?::\s*([^\s=]+))?\s*=>/);
        if (matched) {
            const [, name, rawParams, rawReturnType] = matched;
            return {
                name,
                params: this.parseParams(rawParams),
                returnType: rawReturnType?.trim() || 'unknown',
                docstring: '',
            };
        }
        matched = normalized.match(/^(\w+)\s*\(([^)]*)\)\s*(?::\s*([^\s{]+))?\s*\{/);
        if (matched) {
            const [, name, rawParams, rawReturnType] = matched;
            return {
                name,
                params: this.parseParams(rawParams),
                returnType: rawReturnType?.trim() || 'unknown',
                docstring: '',
            };
        }
        return null;
    }
    parseGoMethod(line) {
        const normalized = line.replace(/\s+/g, ' ').trim();
        const matched = normalized.match(/^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(([^)]*)\)\s*([^\s{][^{]*)?/);
        if (!matched) {
            return null;
        }
        const [, name, rawParams, rawReturnType] = matched;
        return {
            name,
            params: this.parseGoParams(rawParams),
            returnType: rawReturnType?.trim() || 'void',
            docstring: '',
        };
    }
    parsePythonMethod(line) {
        const normalized = line.replace(/\s+/g, ' ').trim();
        const matched = normalized.match(/^(?:async\s+)?def\s+(\w+)\s*\(([^)]*)\)\s*(?:->\s*([^:]+))?:?/);
        if (!matched) {
            return null;
        }
        const [, name, rawParams, rawReturnType] = matched;
        const params = this.parseParams(rawParams).filter((param) => param !== 'self');
        return {
            name,
            params,
            returnType: rawReturnType?.trim() || 'unknown',
            docstring: '',
        };
    }
    parseParams(rawParams) {
        if (!rawParams.trim()) {
            return [];
        }
        return rawParams
            .split(',')
            .map((part) => part.trim())
            .filter((part) => part.length > 0)
            .map((part) => part.split(':')[0]?.trim() || part)
            .filter((part) => part.length > 0);
    }
    parseGoParams(rawParams) {
        if (!rawParams.trim()) {
            return [];
        }
        return rawParams
            .split(',')
            .map((part) => part.trim())
            .filter((part) => part.length > 0)
            .map((part) => {
            const pieces = part.split(/\s+/);
            return pieces[0] || part;
        });
    }
    extractClassDefinitions(nodes, language) {
        const classNodes = nodes.filter((node) => node.type === 'class_declaration');
        const classes = classNodes
            .map((node) => this.parseClassSignature(node.text, language))
            .filter((item) => item !== null);
        const unique = new Map();
        for (const item of classes) {
            if (!unique.has(item.name)) {
                unique.set(item.name, item);
            }
        }
        return Array.from(unique.values());
    }
    parseClassSignature(signatureText, language) {
        const line = signatureText
            .split('\n')
            .map((part) => part.trim())
            .find((part) => part.length > 0);
        if (!line) {
            return null;
        }
        if (language === 'typescript' || language === 'javascript') {
            const match = line.match(/^(?:export\s+)?(?:abstract\s+)?class\s+(\w+)(?:\s+extends\s+(\w+))?(?:\s+implements\s+([^{]+))?/);
            if (match) {
                const [, name, extendsName, implementsPart] = match;
                const implementsList = implementsPart
                    ? implementsPart.split(',').map((part) => part.trim()).filter((part) => part.length > 0)
                    : [];
                return {
                    name,
                    extends: extendsName?.trim() || '',
                    implements: implementsList,
                };
            }
            const interfaceMatch = line.match(/^interface\s+(\w+)/);
            if (interfaceMatch) {
                return {
                    name: interfaceMatch[1],
                    extends: '',
                    implements: [],
                };
            }
            return null;
        }
        if (language === 'go') {
            const structMatch = line.match(/^type\s+(\w+)\s+struct/);
            if (!structMatch) {
                return null;
            }
            return {
                name: structMatch[1],
                extends: '',
                implements: [],
            };
        }
        if (language === 'python') {
            const classMatch = line.match(/^class\s+(\w+)(?:\(([^)]*)\))?/);
            if (!classMatch) {
                return null;
            }
            const [, name, baseClass] = classMatch;
            return {
                name,
                extends: baseClass?.trim() || '',
                implements: [],
            };
        }
        return null;
    }
}
//# sourceMappingURL=IntentProjector.js.map