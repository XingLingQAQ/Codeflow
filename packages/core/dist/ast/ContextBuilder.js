/**
 * AST 上下文构建器实现
 * 基于简化的 AST 解析（不依赖 Tree-sitter WASM）
 */
import { DEFAULT_CONTEXT_OPTIONS, LANGUAGE_CONFIGS, getLanguageFromExtension, } from './types.js';
/**
 * 简化的 AST 解析器
 * 使用正则表达式进行基础解析（生产环境应集成 Tree-sitter）
 */
export class SimpleASTParser {
    constructor() {
        this.nodeIdCounter = 0;
    }
    async parse(code, language) {
        const startTime = Date.now();
        try {
            const rootNode = this.parseCode(code, language);
            return {
                success: true,
                language,
                rootNode,
                errors: [],
                parseTime: Date.now() - startTime,
            };
        }
        catch (error) {
            return {
                success: false,
                language,
                rootNode: null,
                errors: [
                    {
                        message: error instanceof Error ? error.message : String(error),
                        position: { row: 0, column: 0 },
                        type: 'syntax',
                    },
                ],
                parseTime: Date.now() - startTime,
            };
        }
    }
    getLanguage(filename) {
        return getLanguageFromExtension(filename);
    }
    isSupported(language) {
        return language in LANGUAGE_CONFIGS;
    }
    parseCode(code, language) {
        const lines = code.split('\n');
        const rootNode = this.createNode('program', code, { row: 0, column: 0 });
        const config = LANGUAGE_CONFIGS[language];
        const children = [];
        // 解析导入语句
        const imports = this.extractImports(code, language);
        children.push(...imports);
        // 解析函数/方法
        const functions = this.extractFunctions(code, language);
        children.push(...functions);
        // 解析类/结构体
        const classes = this.extractClasses(code, language);
        children.push(...classes);
        rootNode.children = children;
        return rootNode;
    }
    createNode(type, text, startPosition, endPosition) {
        const lines = text.split('\n');
        const endPos = endPosition || {
            row: startPosition.row + lines.length - 1,
            column: lines.length === 1
                ? startPosition.column + text.length
                : lines[lines.length - 1].length,
        };
        return {
            id: `node_${++this.nodeIdCounter}`,
            type,
            text,
            startPosition,
            endPosition: endPos,
            startIndex: 0,
            endIndex: text.length,
            children: [],
            isNamed: true,
        };
    }
    extractImports(code, language) {
        const nodes = [];
        const lines = code.split('\n');
        const patterns = {
            javascript: /^(import\s+.+|const\s+\w+\s*=\s*require\(.+\))/,
            typescript: /^(import\s+.+)/,
            python: /^(import\s+.+|from\s+.+\s+import\s+.+)/,
            rust: /^(use\s+.+;)/,
            go: /^(import\s+.+)/,
            java: /^(import\s+.+;)/,
            c: /^(#include\s+.+)/,
            cpp: /^(#include\s+.+)/,
            csharp: /^(using\s+.+;)/,
        };
        const pattern = patterns[language];
        if (!pattern)
            return nodes;
        lines.forEach((line, row) => {
            const trimmed = line.trim();
            if (pattern.test(trimmed)) {
                nodes.push(this.createNode('import_statement', trimmed, { row, column: 0 }));
            }
        });
        return nodes;
    }
    extractFunctions(code, language) {
        const nodes = [];
        const lines = code.split('\n');
        const patterns = {
            javascript: /^(?:export\s+)?(?:async\s+)?function\s+(\w+)|^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(/,
            typescript: /^(?:export\s+)?(?:async\s+)?function\s+(\w+)|^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(/,
            python: /^(async\s+)?def\s+(\w+)\s*\(/,
            rust: /^(pub\s+)?(async\s+)?fn\s+(\w+)/,
            go: /^func\s+(\(\w+\s+\*?\w+\)\s+)?(\w+)\s*\(/,
            java: /^(public|private|protected)?\s*(static)?\s*\w+\s+(\w+)\s*\(/,
            c: /^\w+\s+(\w+)\s*\([^)]*\)\s*\{/,
            cpp: /^\w+\s+(\w+)\s*\([^)]*\)\s*\{/,
            csharp: /^(public|private|protected)?\s*(static)?\s*\w+\s+(\w+)\s*\(/,
        };
        const pattern = patterns[language];
        if (!pattern)
            return nodes;
        lines.forEach((line, row) => {
            const trimmed = line.trim();
            if (pattern.test(trimmed)) {
                // 尝试找到函数体结束位置
                const funcText = this.extractBlock(lines, row);
                nodes.push(this.createNode('function_declaration', funcText, { row, column: 0 }));
            }
        });
        return nodes;
    }
    extractClasses(code, language) {
        const nodes = [];
        const lines = code.split('\n');
        const patterns = {
            javascript: /^class\s+(\w+)/,
            typescript: /^(export\s+)?(abstract\s+)?class\s+(\w+)|^interface\s+(\w+)/,
            python: /^class\s+(\w+)/,
            rust: /^(pub\s+)?struct\s+(\w+)|^impl\s+(\w+)/,
            go: /^type\s+(\w+)\s+struct/,
            java: /^(public\s+)?(abstract\s+)?class\s+(\w+)|^interface\s+(\w+)/,
            csharp: /^(public\s+)?(abstract\s+)?class\s+(\w+)|^interface\s+(\w+)/,
        };
        const pattern = patterns[language];
        if (!pattern)
            return nodes;
        lines.forEach((line, row) => {
            const trimmed = line.trim();
            if (pattern.test(trimmed)) {
                const classText = this.extractBlock(lines, row);
                nodes.push(this.createNode('class_declaration', classText, { row, column: 0 }));
            }
        });
        return nodes;
    }
    extractBlock(lines, startRow) {
        let braceCount = 0;
        let started = false;
        const blockLines = [];
        for (let i = startRow; i < lines.length; i++) {
            const line = lines[i];
            blockLines.push(line);
            for (const char of line) {
                if (char === '{') {
                    braceCount++;
                    started = true;
                }
                else if (char === '}') {
                    braceCount--;
                }
            }
            if (started && braceCount === 0) {
                break;
            }
        }
        return blockLines.join('\n');
    }
}
/**
 * 上下文构建器
 */
export class ContextBuilder {
    constructor(parser) {
        this.parser = parser || new SimpleASTParser();
    }
    async buildContext(code, language, selection, options = {}) {
        const opts = { ...DEFAULT_CONTEXT_OPTIONS, ...options };
        const parseResult = await this.parser.parse(code, language);
        if (!parseResult.success || !parseResult.rootNode) {
            return {
                nodes: [],
                text: '',
                startPosition: selection.start,
                endPosition: selection.end,
                tokenCount: 0,
            };
        }
        // 找到选择范围内的节点
        const selectedNodes = this.findNodesInRange(parseResult.rootNode, selection.start, selection.end);
        // 扩展到函数/类边界
        let expandedNodes = selectedNodes;
        if (opts.expandToFunction) {
            expandedNodes = this.expandToFunctions(parseResult.rootNode, selectedNodes);
        }
        if (opts.expandToClass) {
            expandedNodes = this.expandToClasses(parseResult.rootNode, expandedNodes);
        }
        // 构建上下文文本
        const contextParts = [];
        // 添加导入语句
        if (opts.includeImports) {
            const imports = parseResult.rootNode.children.filter(n => n.type === 'import_statement');
            contextParts.push(...imports.map(n => n.text));
        }
        // 添加选中的节点
        contextParts.push(...expandedNodes.map(n => n.text));
        const text = contextParts.join('\n\n');
        const tokenCount = Math.ceil(text.length / 4);
        // 截断到最大 token 数
        let finalText = text;
        if (tokenCount > opts.maxTokens) {
            const maxChars = opts.maxTokens * 4;
            finalText = text.slice(0, maxChars) + '\n// [truncated]';
        }
        return {
            nodes: expandedNodes,
            text: finalText,
            startPosition: expandedNodes[0]?.startPosition || selection.start,
            endPosition: expandedNodes[expandedNodes.length - 1]?.endPosition || selection.end,
            tokenCount: Math.ceil(finalText.length / 4),
        };
    }
    async extractSymbols(code, language) {
        const parseResult = await this.parser.parse(code, language);
        if (!parseResult.success || !parseResult.rootNode) {
            return [];
        }
        const symbols = [];
        // 提取函数符号
        const functions = parseResult.rootNode.children.filter(n => n.type === 'function_declaration');
        for (const func of functions) {
            const name = this.extractFunctionName(func.text, language);
            if (name) {
                symbols.push({
                    name,
                    kind: 'function',
                    position: func.startPosition,
                    range: { start: func.startPosition, end: func.endPosition },
                });
            }
        }
        // 提取类符号
        const classes = parseResult.rootNode.children.filter(n => n.type === 'class_declaration');
        for (const cls of classes) {
            const name = this.extractClassName(cls.text, language);
            if (name) {
                symbols.push({
                    name,
                    kind: 'class',
                    position: cls.startPosition,
                    range: { start: cls.startPosition, end: cls.endPosition },
                });
            }
        }
        return symbols;
    }
    findNodeAtPosition(rootNode, position) {
        if (!this.isPositionInNode(position, rootNode)) {
            return null;
        }
        // 深度优先搜索最具体的节点
        for (const child of rootNode.children) {
            const found = this.findNodeAtPosition(child, position);
            if (found)
                return found;
        }
        return rootNode;
    }
    getNodePath(node) {
        const path = [node];
        let current = node;
        while (current.parent) {
            path.unshift(current.parent);
            current = current.parent;
        }
        return path;
    }
    // ==================== Private Methods ====================
    findNodesInRange(rootNode, start, end) {
        const nodes = [];
        for (const child of rootNode.children) {
            if (this.isNodeInRange(child, start, end)) {
                nodes.push(child);
            }
        }
        return nodes;
    }
    isNodeInRange(node, start, end) {
        return ((node.startPosition.row >= start.row || node.endPosition.row >= start.row) &&
            (node.startPosition.row <= end.row || node.endPosition.row <= end.row));
    }
    isPositionInNode(position, node) {
        if (position.row < node.startPosition.row)
            return false;
        if (position.row > node.endPosition.row)
            return false;
        if (position.row === node.startPosition.row && position.column < node.startPosition.column)
            return false;
        if (position.row === node.endPosition.row && position.column > node.endPosition.column)
            return false;
        return true;
    }
    expandToFunctions(rootNode, nodes) {
        const functions = rootNode.children.filter(n => n.type === 'function_declaration');
        const expanded = new Set(nodes);
        for (const node of nodes) {
            for (const func of functions) {
                if (this.isNodeInRange(node, func.startPosition, func.endPosition)) {
                    expanded.add(func);
                }
            }
        }
        return Array.from(expanded);
    }
    expandToClasses(rootNode, nodes) {
        const classes = rootNode.children.filter(n => n.type === 'class_declaration');
        const expanded = new Set(nodes);
        for (const node of nodes) {
            for (const cls of classes) {
                if (this.isNodeInRange(node, cls.startPosition, cls.endPosition)) {
                    expanded.add(cls);
                }
            }
        }
        return Array.from(expanded);
    }
    extractFunctionName(text, language) {
        const patterns = {
            javascript: /function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=/,
            typescript: /function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=/,
            python: /def\s+(\w+)/,
            rust: /fn\s+(\w+)/,
            go: /func\s+(?:\(\w+\s+\*?\w+\)\s+)?(\w+)/,
            java: /\w+\s+(\w+)\s*\(/,
            c: /\w+\s+(\w+)\s*\(/,
            cpp: /\w+\s+(\w+)\s*\(/,
            csharp: /\w+\s+(\w+)\s*\(/,
        };
        const pattern = patterns[language];
        if (!pattern)
            return null;
        const match = text.match(pattern);
        return match ? (match[1] || match[2]) : null;
    }
    extractClassName(text, language) {
        const patterns = {
            javascript: /class\s+(\w+)/,
            typescript: /(?:class|interface)\s+(\w+)/,
            python: /class\s+(\w+)/,
            rust: /(?:struct|impl|trait)\s+(\w+)/,
            go: /type\s+(\w+)\s+struct/,
            java: /(?:class|interface)\s+(\w+)/,
            csharp: /(?:class|interface)\s+(\w+)/,
        };
        const pattern = patterns[language];
        if (!pattern)
            return null;
        const match = text.match(pattern);
        return match ? match[1] : null;
    }
}
//# sourceMappingURL=ContextBuilder.js.map