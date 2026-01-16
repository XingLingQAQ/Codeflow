/**
 * AST 上下文构建器实现
 * 基于简化的 AST 解析（不依赖 Tree-sitter WASM）
 */

import {
  IASTParser,
  IContextBuilder,
  SupportedLanguage,
  ParseResult,
  ASTNode,
  Position,
  ContextSelection,
  ContextBuildOptions,
  SymbolInfo,
  SymbolKind,
  DEFAULT_CONTEXT_OPTIONS,
  LANGUAGE_CONFIGS,
  getLanguageFromExtension,
} from './types.js';

/**
 * 简化的 AST 解析器
 * 使用正则表达式进行基础解析（生产环境应集成 Tree-sitter）
 */
export class SimpleASTParser implements IASTParser {
  private nodeIdCounter = 0;

  async parse(code: string, language: SupportedLanguage): Promise<ParseResult> {
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
    } catch (error) {
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

  getLanguage(filename: string): SupportedLanguage | null {
    return getLanguageFromExtension(filename);
  }

  isSupported(language: string): boolean {
    return language in LANGUAGE_CONFIGS;
  }

  private parseCode(code: string, language: SupportedLanguage): ASTNode {
    const lines = code.split('\n');
    const rootNode = this.createNode('program', code, { row: 0, column: 0 });

    const config = LANGUAGE_CONFIGS[language];
    const children: ASTNode[] = [];

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

  private createNode(
    type: string,
    text: string,
    startPosition: Position,
    endPosition?: Position
  ): ASTNode {
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

  private extractImports(code: string, language: SupportedLanguage): ASTNode[] {
    const nodes: ASTNode[] = [];
    const lines = code.split('\n');

    const patterns: Record<string, RegExp> = {
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
    if (!pattern) return nodes;

    lines.forEach((line, row) => {
      const trimmed = line.trim();
      if (pattern.test(trimmed)) {
        nodes.push(this.createNode('import_statement', trimmed, { row, column: 0 }));
      }
    });

    return nodes;
  }

  private extractFunctions(code: string, language: SupportedLanguage): ASTNode[] {
    const nodes: ASTNode[] = [];
    const lines = code.split('\n');

    const patterns: Record<string, RegExp> = {
      javascript: /^(async\s+)?function\s+(\w+)|^(const|let|var)\s+(\w+)\s*=\s*(async\s+)?\(/,
      typescript: /^(async\s+)?function\s+(\w+)|^(const|let|var)\s+(\w+)\s*=\s*(async\s+)?\(/,
      python: /^(async\s+)?def\s+(\w+)\s*\(/,
      rust: /^(pub\s+)?(async\s+)?fn\s+(\w+)/,
      go: /^func\s+(\(\w+\s+\*?\w+\)\s+)?(\w+)\s*\(/,
      java: /^(public|private|protected)?\s*(static)?\s*\w+\s+(\w+)\s*\(/,
      c: /^\w+\s+(\w+)\s*\([^)]*\)\s*\{/,
      cpp: /^\w+\s+(\w+)\s*\([^)]*\)\s*\{/,
      csharp: /^(public|private|protected)?\s*(static)?\s*\w+\s+(\w+)\s*\(/,
    };

    const pattern = patterns[language];
    if (!pattern) return nodes;

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

  private extractClasses(code: string, language: SupportedLanguage): ASTNode[] {
    const nodes: ASTNode[] = [];
    const lines = code.split('\n');

    const patterns: Record<string, RegExp> = {
      javascript: /^class\s+(\w+)/,
      typescript: /^(export\s+)?(abstract\s+)?class\s+(\w+)|^interface\s+(\w+)/,
      python: /^class\s+(\w+)/,
      rust: /^(pub\s+)?struct\s+(\w+)|^impl\s+(\w+)/,
      go: /^type\s+(\w+)\s+struct/,
      java: /^(public\s+)?(abstract\s+)?class\s+(\w+)|^interface\s+(\w+)/,
      csharp: /^(public\s+)?(abstract\s+)?class\s+(\w+)|^interface\s+(\w+)/,
    };

    const pattern = patterns[language];
    if (!pattern) return nodes;

    lines.forEach((line, row) => {
      const trimmed = line.trim();
      if (pattern.test(trimmed)) {
        const classText = this.extractBlock(lines, row);
        nodes.push(this.createNode('class_declaration', classText, { row, column: 0 }));
      }
    });

    return nodes;
  }

  private extractBlock(lines: string[], startRow: number): string {
    let braceCount = 0;
    let started = false;
    const blockLines: string[] = [];

    for (let i = startRow; i < lines.length; i++) {
      const line = lines[i];
      blockLines.push(line);

      for (const char of line) {
        if (char === '{') {
          braceCount++;
          started = true;
        } else if (char === '}') {
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
export class ContextBuilder implements IContextBuilder {
  private parser: IASTParser;

  constructor(parser?: IASTParser) {
    this.parser = parser || new SimpleASTParser();
  }

  async buildContext(
    code: string,
    language: SupportedLanguage,
    selection: { start: Position; end: Position },
    options: Partial<ContextBuildOptions> = {}
  ): Promise<ContextSelection> {
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
    const selectedNodes = this.findNodesInRange(
      parseResult.rootNode,
      selection.start,
      selection.end
    );

    // 扩展到函数/类边界
    let expandedNodes = selectedNodes;
    if (opts.expandToFunction) {
      expandedNodes = this.expandToFunctions(parseResult.rootNode, selectedNodes);
    }
    if (opts.expandToClass) {
      expandedNodes = this.expandToClasses(parseResult.rootNode, expandedNodes);
    }

    // 构建上下文文本
    const contextParts: string[] = [];

    // 添加导入语句
    if (opts.includeImports) {
      const imports = parseResult.rootNode.children.filter(
        n => n.type === 'import_statement'
      );
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

  async extractSymbols(
    code: string,
    language: SupportedLanguage
  ): Promise<SymbolInfo[]> {
    const parseResult = await this.parser.parse(code, language);

    if (!parseResult.success || !parseResult.rootNode) {
      return [];
    }

    const symbols: SymbolInfo[] = [];

    // 提取函数符号
    const functions = parseResult.rootNode.children.filter(
      n => n.type === 'function_declaration'
    );
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
    const classes = parseResult.rootNode.children.filter(
      n => n.type === 'class_declaration'
    );
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

  findNodeAtPosition(rootNode: ASTNode, position: Position): ASTNode | null {
    if (!this.isPositionInNode(position, rootNode)) {
      return null;
    }

    // 深度优先搜索最具体的节点
    for (const child of rootNode.children) {
      const found = this.findNodeAtPosition(child, position);
      if (found) return found;
    }

    return rootNode;
  }

  getNodePath(node: ASTNode): ASTNode[] {
    const path: ASTNode[] = [node];
    let current = node;

    while (current.parent) {
      path.unshift(current.parent);
      current = current.parent;
    }

    return path;
  }

  // ==================== Private Methods ====================

  private findNodesInRange(
    rootNode: ASTNode,
    start: Position,
    end: Position
  ): ASTNode[] {
    const nodes: ASTNode[] = [];

    for (const child of rootNode.children) {
      if (this.isNodeInRange(child, start, end)) {
        nodes.push(child);
      }
    }

    return nodes;
  }

  private isNodeInRange(node: ASTNode, start: Position, end: Position): boolean {
    return (
      (node.startPosition.row >= start.row || node.endPosition.row >= start.row) &&
      (node.startPosition.row <= end.row || node.endPosition.row <= end.row)
    );
  }

  private isPositionInNode(position: Position, node: ASTNode): boolean {
    if (position.row < node.startPosition.row) return false;
    if (position.row > node.endPosition.row) return false;
    if (position.row === node.startPosition.row && position.column < node.startPosition.column) return false;
    if (position.row === node.endPosition.row && position.column > node.endPosition.column) return false;
    return true;
  }

  private expandToFunctions(rootNode: ASTNode, nodes: ASTNode[]): ASTNode[] {
    const functions = rootNode.children.filter(n => n.type === 'function_declaration');
    const expanded = new Set<ASTNode>(nodes);

    for (const node of nodes) {
      for (const func of functions) {
        if (this.isNodeInRange(node, func.startPosition, func.endPosition)) {
          expanded.add(func);
        }
      }
    }

    return Array.from(expanded);
  }

  private expandToClasses(rootNode: ASTNode, nodes: ASTNode[]): ASTNode[] {
    const classes = rootNode.children.filter(n => n.type === 'class_declaration');
    const expanded = new Set<ASTNode>(nodes);

    for (const node of nodes) {
      for (const cls of classes) {
        if (this.isNodeInRange(node, cls.startPosition, cls.endPosition)) {
          expanded.add(cls);
        }
      }
    }

    return Array.from(expanded);
  }

  private extractFunctionName(text: string, language: SupportedLanguage): string | null {
    const patterns: Record<string, RegExp> = {
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
    if (!pattern) return null;

    const match = text.match(pattern);
    return match ? (match[1] || match[2]) : null;
  }

  private extractClassName(text: string, language: SupportedLanguage): string | null {
    const patterns: Record<string, RegExp> = {
      javascript: /class\s+(\w+)/,
      typescript: /(?:class|interface)\s+(\w+)/,
      python: /class\s+(\w+)/,
      rust: /(?:struct|impl|trait)\s+(\w+)/,
      go: /type\s+(\w+)\s+struct/,
      java: /(?:class|interface)\s+(\w+)/,
      csharp: /(?:class|interface)\s+(\w+)/,
    };

    const pattern = patterns[language];
    if (!pattern) return null;

    const match = text.match(pattern);
    return match ? match[1] : null;
  }
}
