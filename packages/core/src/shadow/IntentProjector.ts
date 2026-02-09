import * as fs from 'fs';
import * as path from 'path';

import { SimpleASTParser } from '../ast/ContextBuilder.js';

export interface PublicMethodInfo {
  name: string;
  params: string[];
  returnType: string;
  docstring: string;
}

export interface ClassDefinitionInfo {
  name: string;
  extends: string;
  implements: string[];
}

export interface IntentProjectionResult {
  language: 'typescript' | 'javascript' | 'go' | 'python';
  publicMethods: PublicMethodInfo[];
  imports: string[];
  classDefinitions: ClassDefinitionInfo[];
}

const SUPPORTED_LANGUAGES = ['typescript', 'javascript', 'go', 'python'] as const;
type SupportedProjectionLanguage = (typeof SUPPORTED_LANGUAGES)[number];

const LANGUAGE_BY_EXTENSION: Record<string, SupportedProjectionLanguage> = {
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

export class IntentProjector {
  private readonly astParser: SimpleASTParser;

  constructor(astParser: SimpleASTParser = new SimpleASTParser()) {
    this.astParser = astParser;
  }

  async project(sourceFile: string): Promise<IntentProjectionResult> {
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

  private detectLanguage(sourceFile: string): SupportedProjectionLanguage {
    const ext = path.extname(sourceFile).toLowerCase();
    const language = LANGUAGE_BY_EXTENSION[ext];
    if (!language) {
      throw new Error(`不支持的文件类型: ${ext || '(无扩展名)'}，仅支持 ${SUPPORTED_LANGUAGES.join('/')}`);
    }

    return language;
  }

  private extractImports(nodes: Array<{ type: string; text: string }>): string[] {
    const imports = nodes
      .filter((node) => node.type === 'import_statement')
      .map((node) => node.text.trim())
      .filter((text) => text.length > 0);

    return Array.from(new Set(imports));
  }

  private extractPublicMethods(
    nodes: Array<{ type: string; text: string }>,
    language: SupportedProjectionLanguage
  ): PublicMethodInfo[] {
    const functionNodes = nodes.filter((node) => node.type === 'function_declaration');

    const methods = functionNodes
      .map((node) => this.parseMethodSignature(node.text, language))
      .filter((item): item is PublicMethodInfo => item !== null);

    const unique = new Map<string, PublicMethodInfo>();
    for (const method of methods) {
      const key = `${method.name}|${method.params.join(',')}|${method.returnType}`;
      if (!unique.has(key)) {
        unique.set(key, method);
      }
    }

    return Array.from(unique.values());
  }

  private parseMethodSignature(signatureText: string, language: SupportedProjectionLanguage): PublicMethodInfo | null {
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

  private parseTSLikeMethod(line: string): PublicMethodInfo | null {
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

  private parseGoMethod(line: string): PublicMethodInfo | null {
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

  private parsePythonMethod(line: string): PublicMethodInfo | null {
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

  private parseParams(rawParams: string): string[] {
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

  private parseGoParams(rawParams: string): string[] {
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

  private extractClassDefinitions(
    nodes: Array<{ type: string; text: string }>,
    language: SupportedProjectionLanguage
  ): ClassDefinitionInfo[] {
    const classNodes = nodes.filter((node) => node.type === 'class_declaration');
    const classes = classNodes
      .map((node) => this.parseClassSignature(node.text, language))
      .filter((item): item is ClassDefinitionInfo => item !== null);

    const unique = new Map<string, ClassDefinitionInfo>();
    for (const item of classes) {
      if (!unique.has(item.name)) {
        unique.set(item.name, item);
      }
    }

    return Array.from(unique.values());
  }

  private parseClassSignature(signatureText: string, language: SupportedProjectionLanguage): ClassDefinitionInfo | null {
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
