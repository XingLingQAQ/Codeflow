/**
 * InterfaceExtractor - MS-240 增强接口摘要
 *
 * 使用 AST 解析提取源文件中的详细接口信息，
 * 包括方法签名、参数约束、返回类型、副作用和文档注释。
 */

import { SimpleASTParser } from '../ast/ContextBuilder.js';
import { SupportedLanguage, ASTNode } from '../ast/types.js';

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

/**
 * 语言到文件扩展名映射
 */
const EXTENSION_TO_LANGUAGE: Record<string, SupportedLanguage> = {
  '.ts': 'typescript',
  '.tsx': 'typescript',
  '.js': 'javascript',
  '.jsx': 'javascript',
  '.py': 'python',
  '.go': 'go',
  '.rs': 'rust',
  '.java': 'java',
};

export class InterfaceExtractor {
  private readonly astParser: SimpleASTParser;

  constructor(astParser?: SimpleASTParser) {
    this.astParser = astParser || new SimpleASTParser();
  }

  /**
   * 从源文件内容中提取接口信息
   */
  async extractInterfaces(
    sourceCode: string,
    language: SupportedLanguage
  ): Promise<InterfaceSummary[]> {
    const parseResult = await this.astParser.parse(sourceCode, language);

    if (!parseResult.success || !parseResult.rootNode) {
      return [];
    }

    const summaries: InterfaceSummary[] = [];

    const classNodes = parseResult.rootNode.children.filter(
      (n) => n.type === 'class_declaration'
    );
    for (const node of classNodes) {
      const summary = this.extractClassSummary(node.text, language);
      if (summary) {
        summaries.push(summary);
      }
    }

    const functionNodes = parseResult.rootNode.children.filter(
      (n) => n.type === 'function_declaration'
    );
    for (const node of functionNodes) {
      const summary = this.extractFunctionSummary(node.text, language);
      if (summary) {
        summaries.push(summary);
      }
    }

    return summaries;
  }

  /**
   * 从文件路径推断语言
   */
  getLanguageFromPath(filePath: string): SupportedLanguage | null {
    const ext = filePath.slice(filePath.lastIndexOf('.'));
    return EXTENSION_TO_LANGUAGE[ext] || null;
  }

  private extractClassSummary(
    text: string,
    language: SupportedLanguage
  ): InterfaceSummary | null {
    const nameMatch = this.extractClassName(text, language);
    if (!nameMatch) return null;

    const isInterface = this.isInterfaceDeclaration(text, language);
    const extendsInfo = this.extractExtends(text, language);
    const implementsInfo = this.extractImplements(text, language);
    const docstring = this.extractDocstring(text);
    const methods = this.extractMethods(text, language);
    const properties = this.extractProperties(text, language);
    const sideEffects = this.detectSideEffects(text);

    return {
      name: nameMatch,
      kind: isInterface ? 'interface' : 'class',
      methods,
      properties,
      sideEffects,
      docstring,
      extends: extendsInfo,
      implements: implementsInfo,
    };
  }

  private extractFunctionSummary(
    text: string,
    language: SupportedLanguage
  ): InterfaceSummary | null {
    const name = this.extractFunctionName(text, language);
    if (!name) return null;

    const method = this.parseMethodSignature(text, language, name);
    const docstring = this.extractDocstring(text);
    const sideEffects = this.detectSideEffects(text);

    return {
      name,
      kind: 'function',
      methods: method ? [method] : [],
      properties: [],
      sideEffects,
      docstring,
    };
  }

  private extractClassName(text: string, language: SupportedLanguage): string | null {
    const patterns: Record<string, RegExp> = {
      typescript: /(?:export\s+)?(?:abstract\s+)?(?:class|interface)\s+(\w+)/,
      javascript: /(?:export\s+)?class\s+(\w+)/,
      python: /class\s+(\w+)/,
      go: /type\s+(\w+)\s+struct/,
      java: /(?:public\s+)?(?:abstract\s+)?(?:class|interface)\s+(\w+)/,
      rust: /(?:pub\s+)?struct\s+(\w+)/,
    };

    const pattern = patterns[language];
    if (!pattern) return null;

    const match = text.match(pattern);
    return match ? match[1] : null;
  }

  private isInterfaceDeclaration(text: string, language: SupportedLanguage): boolean {
    if (language === 'typescript' || language === 'java') {
      return /\binterface\s+\w+/.test(text);
    }
    return false;
  }

  private extractExtends(text: string, language: SupportedLanguage): string | undefined {
    if (language === 'typescript' || language === 'javascript' || language === 'java') {
      const match = text.match(/extends\s+(\w+)/);
      return match ? match[1] : undefined;
    }
    return undefined;
  }

  private extractImplements(text: string, language: SupportedLanguage): string[] | undefined {
    if (language === 'typescript' || language === 'java') {
      const match = text.match(/implements\s+([\w,\s]+?)(?:\s*\{)/);
      if (match) {
        return match[1].split(',').map((s) => s.trim()).filter(Boolean);
      }
    }
    return undefined;
  }

  private extractDocstring(text: string): string {
    const jsdocMatch = text.match(/\/\*\*([\s\S]*?)\*\//);
    if (jsdocMatch) {
      return jsdocMatch[1]
        .split('\n')
        .map((line) => line.replace(/^\s*\*\s?/, '').trim())
        .filter(Boolean)
        .join(' ');
    }

    const singleLineMatch = text.match(/\/\/\s*(.*)/);
    if (singleLineMatch) {
      return singleLineMatch[1].trim();
    }

    const pythonDocMatch = text.match(/"""([\s\S]*?)"""|'''([\s\S]*?)'''/);
    if (pythonDocMatch) {
      return (pythonDocMatch[1] || pythonDocMatch[2]).trim();
    }

    return '';
  }

  private extractMethods(text: string, language: SupportedLanguage): MethodSignature[] {
    const methods: MethodSignature[] = [];
    const lines = text.split('\n');

    const methodPatterns: Record<string, RegExp> = {
      typescript: /^\s*(public|private|protected)?\s*(async\s+)?(\w+)\s*\((.*?)\)(?:\s*:\s*(.+?))?\s*\{/,
      javascript: /^\s*(async\s+)?(\w+)\s*\((.*?)\)\s*\{/,
      python: /^\s+def\s+(\w+)\s*\(([^)]*)\)(?:\s*->\s*(.+?))?\s*:/,
      go: /^func\s+(?:\(\w+\s+\*?\w+\)\s+)?(\w+)\s*\(([^)]*)\)(?:\s*(?:\(([^)]*)\)|(\w+)))?\s*\{/,
    };

    const pattern = methodPatterns[language];
    if (!pattern) return methods;

    for (const line of lines) {
      const match = line.match(pattern);
      if (!match) continue;

      const method = this.parseMethodFromMatch(match, language);
      if (method) {
        methods.push(method);
      }
    }

    return methods;
  }

  private parseMethodFromMatch(
    match: RegExpMatchArray,
    language: SupportedLanguage
  ): MethodSignature | null {
    if (language === 'typescript') {
      const visibility = (match[1] || 'public') as 'public' | 'private' | 'protected';
      const isAsync = !!match[2];
      const name = match[3];
      const paramsStr = match[4] || '';
      const returnType = match[5]?.trim() || 'void';

      if (!name || name === 'constructor') return null;

      return {
        name,
        parameters: this.parseParameters(paramsStr, language),
        returnType,
        isAsync,
        visibility,
        docstring: '',
      };
    }

    if (language === 'javascript') {
      const isAsync = !!match[1];
      const name = match[2];
      const paramsStr = match[3] || '';

      if (!name || name === 'constructor') return null;

      return {
        name,
        parameters: this.parseParameters(paramsStr, language),
        returnType: 'unknown',
        isAsync,
        visibility: 'public',
        docstring: '',
      };
    }

    if (language === 'python') {
      const name = match[1];
      const paramsStr = match[2] || '';
      const returnType = match[3]?.trim() || 'None';

      if (!name || name.startsWith('_')) {
        if (name === '__init__') {
          return {
            name,
            parameters: this.parseParameters(paramsStr, language),
            returnType: 'None',
            isAsync: false,
            visibility: 'public',
            docstring: '',
          };
        }
        return null;
      }

      return {
        name,
        parameters: this.parseParameters(paramsStr, language),
        returnType,
        isAsync: false,
        visibility: 'public',
        docstring: '',
      };
    }

    if (language === 'go') {
      const name = match[1];
      const paramsStr = match[2] || '';
      const returnType = (match[3] || match[4] || '').trim() || 'void';

      if (!name) return null;

      const isPublic = name[0] === name[0].toUpperCase();

      return {
        name,
        parameters: this.parseParameters(paramsStr, language),
        returnType,
        isAsync: false,
        visibility: isPublic ? 'public' : 'private',
        docstring: '',
      };
    }

    return null;
  }

  private parseMethodSignature(
    text: string,
    language: SupportedLanguage,
    name: string
  ): MethodSignature | null {
    const isAsync = /\basync\b/.test(text);
    const paramsMatch = text.match(new RegExp(`${name}\\s*\\(([^)]*)\\)`));
    const paramsStr = paramsMatch ? paramsMatch[1] : '';

    let returnType = 'void';
    if (language === 'typescript') {
      const retMatch = text.match(/\)\s*:\s*([^{]+)/);
      if (retMatch) returnType = retMatch[1].trim();
    } else if (language === 'python') {
      const retMatch = text.match(/->\s*(.+?):/);
      if (retMatch) returnType = retMatch[1].trim();
    }

    return {
      name,
      parameters: this.parseParameters(paramsStr, language),
      returnType,
      isAsync,
      visibility: 'public',
      docstring: this.extractDocstring(text),
    };
  }

  private parseParameters(paramsStr: string, language: SupportedLanguage): ParameterInfo[] {
    if (!paramsStr.trim()) return [];

    const params: ParameterInfo[] = [];
    const parts = this.splitParameters(paramsStr);

    for (const part of parts) {
      const trimmed = part.trim();
      if (!trimmed || trimmed === 'self' || trimmed === 'cls') continue;

      const param = this.parseSingleParameter(trimmed, language);
      if (param) {
        params.push(param);
      }
    }

    return params;
  }

  private splitParameters(paramsStr: string): string[] {
    const parts: string[] = [];
    let depth = 0;
    let current = '';

    for (const char of paramsStr) {
      if (char === '<' || char === '(' || char === '[' || char === '{') depth++;
      if (char === '>' || char === ')' || char === ']' || char === '}') depth--;

      if (char === ',' && depth === 0) {
        parts.push(current);
        current = '';
      } else {
        current += char;
      }
    }

    if (current.trim()) {
      parts.push(current);
    }

    return parts;
  }

  private parseSingleParameter(param: string, language: SupportedLanguage): ParameterInfo | null {
    if (language === 'typescript') {
      const match = param.match(/^(\w+)(\?)?(?:\s*:\s*(.+?))?(?:\s*=\s*(.+))?$/);
      if (match) {
        return {
          name: match[1],
          type: match[3]?.trim() || 'unknown',
          required: !match[2] && !match[4],
          defaultValue: match[4]?.trim(),
        };
      }
    }

    if (language === 'python') {
      const match = param.match(/^(\w+)(?:\s*:\s*(.+?))?(?:\s*=\s*(.+))?$/);
      if (match) {
        return {
          name: match[1],
          type: match[2]?.trim() || 'Any',
          required: !match[3],
          defaultValue: match[3]?.trim(),
        };
      }
    }

    if (language === 'go') {
      const match = param.match(/^(\w+)\s+(.+)$/);
      if (match) {
        return {
          name: match[1],
          type: match[2].trim(),
          required: true,
        };
      }
    }

    return {
      name: param.trim(),
      type: 'unknown',
      required: true,
    };
  }

  private extractProperties(text: string, language: SupportedLanguage): ParameterInfo[] {
    const properties: ParameterInfo[] = [];

    if (language === 'typescript') {
      const propPattern = /^\s*(public|private|protected|readonly)?\s*(\w+)(\?)?(?:\s*:\s*(.+?))?(?:\s*=\s*(.+?))?;/gm;
      let match;
      while ((match = propPattern.exec(text)) !== null) {
        properties.push({
          name: match[2],
          type: match[4]?.trim() || 'unknown',
          required: !match[3] && !match[5],
          defaultValue: match[5]?.trim(),
        });
      }
    }

    return properties;
  }

  private extractFunctionName(text: string, language: SupportedLanguage): string | null {
    const patterns: Record<string, RegExp> = {
      typescript: /(?:export\s+)?(?:async\s+)?function\s+(\w+)/,
      javascript: /(?:export\s+)?(?:async\s+)?function\s+(\w+)/,
      python: /def\s+(\w+)/,
      go: /func\s+(\w+)/,
      rust: /(?:pub\s+)?fn\s+(\w+)/,
    };

    const pattern = patterns[language];
    if (!pattern) return null;

    const match = text.match(pattern);
    return match ? match[1] : null;
  }

  /**
   * 检测代码中的副作用
   */
  private detectSideEffects(text: string): SideEffect[] {
    const effects: SideEffect[] = [];

    const dbPatterns = [
      /\b(?:sql|query|exec|insert|update|delete|select)\b/i,
      /\b(?:database|db|sqlite|postgres|mysql|mongo)\b/i,
    ];
    if (dbPatterns.some((p) => p.test(text))) {
      effects.push({ type: 'database', description: '数据库操作' });
    }

    const apiPatterns = [
      /\b(?:fetch|axios|http|request|api)\b/i,
      /\b(?:get|post|put|patch|delete)\s*\(/i,
    ];
    if (apiPatterns.some((p) => p.test(text))) {
      effects.push({ type: 'api', description: 'API/HTTP 调用' });
    }

    const filePatterns = [
      /\b(?:readFile|writeFile|readdir|mkdir|unlink|rename)\b/i,
      /\b(?:fs\.|os\.)\b/i,
      /\bopen\s*\([^)]*['"]/i,
    ];
    if (filePatterns.some((p) => p.test(text))) {
      effects.push({ type: 'file', description: '文件系统操作' });
    }

    const consolePatterns = [/\bconsole\.\w+/i, /\bfmt\.Print/i, /\bprint\(/i];
    if (consolePatterns.some((p) => p.test(text))) {
      effects.push({ type: 'console', description: '控制台输出' });
    }

    return effects;
  }
}
