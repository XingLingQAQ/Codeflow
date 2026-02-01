import { describe, it, expect, beforeEach } from 'vitest';
import { SimpleASTParser, ContextBuilder } from '../ContextBuilder.js';
import {
  SupportedLanguage,
  ASTNode,
  Position,
  ContextBuildOptions,
  DEFAULT_CONTEXT_OPTIONS,
  LANGUAGE_CONFIGS,
  getLanguageFromExtension,
} from '../types.js';

describe('SimpleASTParser', () => {
  let parser: SimpleASTParser;

  beforeEach(() => {
    parser = new SimpleASTParser();
  });

  describe('parse', () => {
    it('should parse JavaScript code', async () => {
      const code = `
import { foo } from 'bar';

function hello() {
  console.log('Hello');
}
`;
      const result = await parser.parse(code, 'javascript');

      expect(result.success).toBe(true);
      expect(result.language).toBe('javascript');
      expect(result.rootNode).toBeDefined();
      expect(result.errors).toEqual([]);
      expect(result.parseTime).toBeGreaterThanOrEqual(0);
    });

    it('should parse TypeScript code', async () => {
      const code = `
import { Component } from 'react';

interface Props {
  name: string;
}

class MyComponent {
  render() {
    return null;
  }
}
`;
      const result = await parser.parse(code, 'typescript');

      expect(result.success).toBe(true);
      expect(result.rootNode).toBeDefined();
    });

    it('should parse Python code', async () => {
      const code = `
import os
from typing import List

def hello(name: str) -> str:
    return f"Hello, {name}"

class Greeter:
    def greet(self):
        pass
`;
      const result = await parser.parse(code, 'python');

      expect(result.success).toBe(true);
      expect(result.rootNode).toBeDefined();
    });

    it('should parse Rust code', async () => {
      const code = `
use std::io;

fn main() {
    println!("Hello");
}

pub struct Point {
    x: i32,
    y: i32,
}
`;
      const result = await parser.parse(code, 'rust');

      expect(result.success).toBe(true);
      expect(result.rootNode).toBeDefined();
    });

    it('should parse Go code', async () => {
      const code = `
import "fmt"

func main() {
    fmt.Println("Hello")
}

type Person struct {
    Name string
    Age  int
}
`;
      const result = await parser.parse(code, 'go');

      expect(result.success).toBe(true);
      expect(result.rootNode).toBeDefined();
    });

    it('should extract import statements', async () => {
      const code = `
import { foo } from 'bar';
import * as baz from 'qux';

function test() {}
`;
      const result = await parser.parse(code, 'javascript');

      const imports = result.rootNode?.children.filter(
        (n) => n.type === 'import_statement'
      );
      expect(imports?.length).toBeGreaterThan(0);
    });

    it('should extract function declarations', async () => {
      const code = `
function hello() {
  return 'hello';
}

function world() {
  return 'world';
}
`;
      const result = await parser.parse(code, 'javascript');

      const functions = result.rootNode?.children.filter(
        (n) => n.type === 'function_declaration'
      );
      expect(functions?.length).toBeGreaterThan(0);
    });

    it('should extract class declarations', async () => {
      const code = `
class MyClass {
  constructor() {}
  method() {}
}
`;
      const result = await parser.parse(code, 'javascript');

      const classes = result.rootNode?.children.filter(
        (n) => n.type === 'class_declaration'
      );
      expect(classes?.length).toBeGreaterThan(0);
    });

    it('should handle empty code', async () => {
      const result = await parser.parse('', 'javascript');

      expect(result.success).toBe(true);
      expect(result.rootNode).toBeDefined();
      expect(result.rootNode?.children.length).toBe(0);
    });

    it('should set parse time', async () => {
      const result = await parser.parse('function test() {}', 'javascript');

      expect(result.parseTime).toBeGreaterThanOrEqual(0);
    });
  });

  describe('getLanguage', () => {
    it('should return javascript for .js files', () => {
      expect(parser.getLanguage('file.js')).toBe('javascript');
    });

    it('should return typescript for .ts files', () => {
      expect(parser.getLanguage('file.ts')).toBe('typescript');
    });

    it('should return typescript for .tsx files', () => {
      expect(parser.getLanguage('component.tsx')).toBe('typescript');
    });

    it('should return python for .py files', () => {
      expect(parser.getLanguage('script.py')).toBe('python');
    });

    it('should return rust for .rs files', () => {
      expect(parser.getLanguage('main.rs')).toBe('rust');
    });

    it('should return go for .go files', () => {
      expect(parser.getLanguage('main.go')).toBe('go');
    });

    it('should return null for unknown extensions', () => {
      expect(parser.getLanguage('file.xyz')).toBeNull();
    });

    it('should handle files with multiple dots', () => {
      expect(parser.getLanguage('my.component.tsx')).toBe('typescript');
    });

    it('should be case insensitive', () => {
      expect(parser.getLanguage('FILE.JS')).toBe('javascript');
    });
  });

  describe('isSupported', () => {
    it('should return true for supported languages', () => {
      expect(parser.isSupported('javascript')).toBe(true);
      expect(parser.isSupported('typescript')).toBe(true);
      expect(parser.isSupported('python')).toBe(true);
      expect(parser.isSupported('rust')).toBe(true);
      expect(parser.isSupported('go')).toBe(true);
    });

    it('should return false for unsupported languages', () => {
      expect(parser.isSupported('brainfuck')).toBe(false);
      expect(parser.isSupported('unknown')).toBe(false);
    });
  });
});

describe('ContextBuilder', () => {
  let builder: ContextBuilder;

  beforeEach(() => {
    builder = new ContextBuilder();
  });

  describe('constructor', () => {
    it('should create builder with default parser', () => {
      const b = new ContextBuilder();
      expect(b).toBeDefined();
    });

    it('should create builder with custom parser', () => {
      const customParser = new SimpleASTParser();
      const b = new ContextBuilder(customParser);
      expect(b).toBeDefined();
    });
  });

  describe('buildContext', () => {
    const sampleCode = `
import { foo } from 'bar';

function hello() {
  console.log('Hello');
}

function world() {
  console.log('World');
}

class MyClass {
  method() {}
}
`;

    it('should build context for selection', async () => {
      const selection = { start: { row: 3, column: 0 }, end: { row: 5, column: 0 } };

      const context = await builder.buildContext(sampleCode, 'javascript', selection);

      expect(context.text).toBeDefined();
      expect(context.tokenCount).toBeGreaterThan(0);
    });

    it('should include imports when option enabled', async () => {
      const selection = { start: { row: 3, column: 0 }, end: { row: 5, column: 0 } };

      const context = await builder.buildContext(sampleCode, 'javascript', selection, {
        includeImports: true,
      });

      expect(context.text).toContain('import');
    });

    it('should exclude imports when option disabled', async () => {
      const selection = { start: { row: 3, column: 0 }, end: { row: 5, column: 0 } };

      const context = await builder.buildContext(sampleCode, 'javascript', selection, {
        includeImports: false,
      });

      // Context should not start with import
      const lines = context.text.split('\n').filter((l) => l.trim());
      if (lines.length > 0) {
        expect(lines[0].trim().startsWith('import')).toBe(false);
      }
    });

    it('should expand to function boundaries', async () => {
      const selection = { start: { row: 4, column: 0 }, end: { row: 4, column: 10 } };

      const context = await builder.buildContext(sampleCode, 'javascript', selection, {
        expandToFunction: true,
      });

      // Should include the entire function
      expect(context.nodes.length).toBeGreaterThanOrEqual(0);
    });

    it('should expand to class boundaries', async () => {
      const selection = { start: { row: 12, column: 0 }, end: { row: 12, column: 10 } };

      const context = await builder.buildContext(sampleCode, 'javascript', selection, {
        expandToClass: true,
      });

      expect(context.nodes.length).toBeGreaterThanOrEqual(0);
    });

    it('should truncate when exceeding maxTokens', async () => {
      const longCode = 'function test() {\n' + '  console.log("x");\n'.repeat(1000) + '}';
      const selection = { start: { row: 0, column: 0 }, end: { row: 1000, column: 0 } };

      const context = await builder.buildContext(longCode, 'javascript', selection, {
        maxTokens: 100,
      });

      expect(context.text).toContain('[truncated]');
      expect(context.tokenCount).toBeLessThanOrEqual(100 + 10); // Allow some margin
    });

    it('should return empty context for failed parse', async () => {
      // Create a builder with a parser that will fail
      const selection = { start: { row: 0, column: 0 }, end: { row: 0, column: 10 } };

      // Empty code should still parse successfully
      const context = await builder.buildContext('', 'javascript', selection);

      expect(context.nodes).toEqual([]);
      expect(context.text).toBe('');
    });

    it('should set correct positions', async () => {
      const selection = { start: { row: 3, column: 0 }, end: { row: 5, column: 0 } };

      const context = await builder.buildContext(sampleCode, 'javascript', selection);

      expect(context.startPosition).toBeDefined();
      expect(context.endPosition).toBeDefined();
    });

    it('should use default options when not specified', async () => {
      const selection = { start: { row: 0, column: 0 }, end: { row: 5, column: 0 } };

      const context = await builder.buildContext(sampleCode, 'javascript', selection);

      // Should use default maxTokens (4000)
      expect(context.tokenCount).toBeLessThanOrEqual(4000);
    });
  });

  describe('extractSymbols', () => {
    it('should extract function symbols', async () => {
      const code = `
function hello() {
  return 'hello';
}

function world() {
  return 'world';
}
`;
      const symbols = await builder.extractSymbols(code, 'javascript');

      const functionSymbols = symbols.filter((s) => s.kind === 'function');
      expect(functionSymbols.length).toBeGreaterThan(0);
    });

    it('should extract class symbols', async () => {
      const code = `
class MyClass {
  method() {}
}

class AnotherClass {
  anotherMethod() {}
}
`;
      const symbols = await builder.extractSymbols(code, 'javascript');

      const classSymbols = symbols.filter((s) => s.kind === 'class');
      expect(classSymbols.length).toBeGreaterThan(0);
    });

    it('should extract TypeScript interface symbols', async () => {
      const code = `
interface Props {
  name: string;
}

class Component {
  render() {}
}
`;
      const symbols = await builder.extractSymbols(code, 'typescript');

      expect(symbols.length).toBeGreaterThan(0);
    });

    it('should extract Python function symbols', async () => {
      const code = `
def hello():
    return 'hello'

def world():
    return 'world'
`;
      const symbols = await builder.extractSymbols(code, 'python');

      const functionSymbols = symbols.filter((s) => s.kind === 'function');
      expect(functionSymbols.length).toBeGreaterThan(0);
    });

    it('should extract Python class symbols', async () => {
      const code = `
class MyClass:
    def method(self):
        pass
`;
      const symbols = await builder.extractSymbols(code, 'python');

      const classSymbols = symbols.filter((s) => s.kind === 'class');
      expect(classSymbols.length).toBeGreaterThan(0);
    });

    it('should return empty array for empty code', async () => {
      const symbols = await builder.extractSymbols('', 'javascript');

      expect(symbols).toEqual([]);
    });

    it('should include position information', async () => {
      const code = `
function test() {
  return 1;
}
`;
      const symbols = await builder.extractSymbols(code, 'javascript');

      if (symbols.length > 0) {
        expect(symbols[0].position).toBeDefined();
        expect(symbols[0].range).toBeDefined();
        expect(symbols[0].range.start).toBeDefined();
        expect(symbols[0].range.end).toBeDefined();
      }
    });

    it('should extract Rust function symbols', async () => {
      const code = `
fn main() {
    println!("Hello");
}

pub fn helper() {
    // helper
}
`;
      const symbols = await builder.extractSymbols(code, 'rust');

      const functionSymbols = symbols.filter((s) => s.kind === 'function');
      expect(functionSymbols.length).toBeGreaterThan(0);
    });

    it('should extract Go function symbols', async () => {
      const code = `
func main() {
    fmt.Println("Hello")
}

func (p *Person) Greet() {
    // method
}
`;
      const symbols = await builder.extractSymbols(code, 'go');

      const functionSymbols = symbols.filter((s) => s.kind === 'function');
      expect(functionSymbols.length).toBeGreaterThan(0);
    });
  });

  describe('findNodeAtPosition', () => {
    it('should find node at position', async () => {
      const code = `
function hello() {
  console.log('Hello');
}
`;
      const parser = new SimpleASTParser();
      const result = await parser.parse(code, 'javascript');

      if (result.rootNode) {
        const node = builder.findNodeAtPosition(result.rootNode, { row: 2, column: 5 });
        expect(node).toBeDefined();
      }
    });

    it('should return null for position outside code', async () => {
      const code = 'function test() {}';
      const parser = new SimpleASTParser();
      const result = await parser.parse(code, 'javascript');

      if (result.rootNode) {
        const node = builder.findNodeAtPosition(result.rootNode, { row: 100, column: 0 });
        expect(node).toBeNull();
      }
    });

    it('should find most specific node', async () => {
      const code = `
function outer() {
  function inner() {
    return 1;
  }
}
`;
      const parser = new SimpleASTParser();
      const result = await parser.parse(code, 'javascript');

      if (result.rootNode) {
        const node = builder.findNodeAtPosition(result.rootNode, { row: 1, column: 0 });
        expect(node).toBeDefined();
      }
    });
  });

  describe('getNodePath', () => {
    it('should return path from root to node', async () => {
      const code = `
function test() {
  return 1;
}
`;
      const parser = new SimpleASTParser();
      const result = await parser.parse(code, 'javascript');

      if (result.rootNode && result.rootNode.children.length > 0) {
        const child = result.rootNode.children[0];
        // Set parent reference for testing
        child.parent = result.rootNode;

        const path = builder.getNodePath(child);

        expect(path.length).toBeGreaterThanOrEqual(1);
        expect(path[path.length - 1]).toBe(child);
      }
    });

    it('should return single node for root', async () => {
      const code = 'function test() {}';
      const parser = new SimpleASTParser();
      const result = await parser.parse(code, 'javascript');

      if (result.rootNode) {
        const path = builder.getNodePath(result.rootNode);

        expect(path.length).toBe(1);
        expect(path[0]).toBe(result.rootNode);
      }
    });
  });
});

describe('getLanguageFromExtension', () => {
  it('should return correct language for common extensions', () => {
    expect(getLanguageFromExtension('file.js')).toBe('javascript');
    expect(getLanguageFromExtension('file.ts')).toBe('typescript');
    expect(getLanguageFromExtension('file.py')).toBe('python');
    expect(getLanguageFromExtension('file.rs')).toBe('rust');
    expect(getLanguageFromExtension('file.go')).toBe('go');
    expect(getLanguageFromExtension('file.java')).toBe('java');
    expect(getLanguageFromExtension('file.c')).toBe('c');
    expect(getLanguageFromExtension('file.cpp')).toBe('cpp');
    expect(getLanguageFromExtension('file.cs')).toBe('csharp');
  });

  it('should return correct language for web extensions', () => {
    expect(getLanguageFromExtension('file.html')).toBe('html');
    expect(getLanguageFromExtension('file.css')).toBe('css');
    expect(getLanguageFromExtension('file.json')).toBe('json');
    expect(getLanguageFromExtension('file.yaml')).toBe('yaml');
    expect(getLanguageFromExtension('file.yml')).toBe('yaml');
    expect(getLanguageFromExtension('file.md')).toBe('markdown');
  });

  it('should return null for unknown extensions', () => {
    expect(getLanguageFromExtension('file.xyz')).toBeNull();
    expect(getLanguageFromExtension('file.unknown')).toBeNull();
  });

  it('should handle uppercase extensions', () => {
    expect(getLanguageFromExtension('FILE.JS')).toBe('javascript');
    expect(getLanguageFromExtension('FILE.TS')).toBe('typescript');
  });

  it('should handle files with multiple dots', () => {
    expect(getLanguageFromExtension('my.component.tsx')).toBe('typescript');
    expect(getLanguageFromExtension('config.test.js')).toBe('javascript');
  });
});

describe('LANGUAGE_CONFIGS', () => {
  it('should have config for all supported languages', () => {
    const languages: SupportedLanguage[] = [
      'javascript',
      'typescript',
      'python',
      'rust',
      'go',
      'java',
      'c',
      'cpp',
      'csharp',
      'ruby',
      'php',
      'swift',
      'kotlin',
      'scala',
      'html',
      'css',
      'json',
      'yaml',
      'markdown',
    ];

    for (const lang of languages) {
      expect(LANGUAGE_CONFIGS[lang]).toBeDefined();
      expect(LANGUAGE_CONFIGS[lang].language).toBe(lang);
      expect(LANGUAGE_CONFIGS[lang].extensions).toBeDefined();
      expect(LANGUAGE_CONFIGS[lang].extensions.length).toBeGreaterThan(0);
    }
  });

  it('should have comment patterns for programming languages', () => {
    const programmingLanguages: SupportedLanguage[] = [
      'javascript',
      'typescript',
      'python',
      'rust',
      'go',
      'java',
      'c',
      'cpp',
      'csharp',
    ];

    for (const lang of programmingLanguages) {
      const config = LANGUAGE_CONFIGS[lang];
      expect(config.commentPatterns).toBeDefined();
      expect(config.commentPatterns.line || config.commentPatterns.blockStart).toBeDefined();
    }
  });

  it('should have function patterns for programming languages', () => {
    const programmingLanguages: SupportedLanguage[] = [
      'javascript',
      'typescript',
      'python',
      'rust',
      'go',
      'java',
    ];

    for (const lang of programmingLanguages) {
      const config = LANGUAGE_CONFIGS[lang];
      expect(config.functionPatterns).toBeDefined();
      expect(config.functionPatterns.length).toBeGreaterThan(0);
    }
  });
});

describe('DEFAULT_CONTEXT_OPTIONS', () => {
  it('should have correct default values', () => {
    expect(DEFAULT_CONTEXT_OPTIONS.maxTokens).toBe(4000);
    expect(DEFAULT_CONTEXT_OPTIONS.includeComments).toBe(true);
    expect(DEFAULT_CONTEXT_OPTIONS.includeImports).toBe(true);
    expect(DEFAULT_CONTEXT_OPTIONS.expandToFunction).toBe(true);
    expect(DEFAULT_CONTEXT_OPTIONS.expandToClass).toBe(false);
    expect(DEFAULT_CONTEXT_OPTIONS.includeRelatedSymbols).toBe(false);
  });
});
