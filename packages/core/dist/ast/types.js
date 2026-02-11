/**
 * AST 上下文构建器类型定义
 * 支持 Tree-sitter 解析引擎
 */
/**
 * 默认上下文构建选项
 */
export const DEFAULT_CONTEXT_OPTIONS = {
    maxTokens: 4000,
    includeComments: true,
    includeImports: true,
    expandToFunction: true,
    expandToClass: false,
    includeRelatedSymbols: false,
};
/**
 * 语言配置映射
 */
export const LANGUAGE_CONFIGS = {
    javascript: {
        language: 'javascript',
        extensions: ['.js', '.mjs', '.cjs'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_statement', 'require'],
        functionPatterns: ['function_declaration', 'arrow_function', 'method_definition'],
        classPatterns: ['class_declaration'],
    },
    typescript: {
        language: 'typescript',
        extensions: ['.ts', '.tsx', '.mts', '.cts'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_statement'],
        functionPatterns: ['function_declaration', 'arrow_function', 'method_definition'],
        classPatterns: ['class_declaration', 'interface_declaration'],
    },
    python: {
        language: 'python',
        extensions: ['.py', '.pyw'],
        commentPatterns: { line: '#', blockStart: '"""', blockEnd: '"""' },
        importPatterns: ['import_statement', 'import_from_statement'],
        functionPatterns: ['function_definition'],
        classPatterns: ['class_definition'],
    },
    rust: {
        language: 'rust',
        extensions: ['.rs'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['use_declaration'],
        functionPatterns: ['function_item'],
        classPatterns: ['struct_item', 'impl_item', 'trait_item'],
    },
    go: {
        language: 'go',
        extensions: ['.go'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_declaration'],
        functionPatterns: ['function_declaration', 'method_declaration'],
        classPatterns: ['type_declaration'],
    },
    java: {
        language: 'java',
        extensions: ['.java'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_declaration'],
        functionPatterns: ['method_declaration', 'constructor_declaration'],
        classPatterns: ['class_declaration', 'interface_declaration'],
    },
    c: {
        language: 'c',
        extensions: ['.c', '.h'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['preproc_include'],
        functionPatterns: ['function_definition'],
        classPatterns: ['struct_specifier'],
    },
    cpp: {
        language: 'cpp',
        extensions: ['.cpp', '.cc', '.cxx', '.hpp', '.hh', '.hxx'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['preproc_include'],
        functionPatterns: ['function_definition'],
        classPatterns: ['class_specifier', 'struct_specifier'],
    },
    csharp: {
        language: 'csharp',
        extensions: ['.cs'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['using_directive'],
        functionPatterns: ['method_declaration'],
        classPatterns: ['class_declaration', 'interface_declaration'],
    },
    ruby: {
        language: 'ruby',
        extensions: ['.rb'],
        commentPatterns: { line: '#', blockStart: '=begin', blockEnd: '=end' },
        importPatterns: ['require', 'require_relative'],
        functionPatterns: ['method'],
        classPatterns: ['class', 'module'],
    },
    php: {
        language: 'php',
        extensions: ['.php'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['use_declaration'],
        functionPatterns: ['function_definition', 'method_declaration'],
        classPatterns: ['class_declaration', 'interface_declaration'],
    },
    swift: {
        language: 'swift',
        extensions: ['.swift'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_declaration'],
        functionPatterns: ['function_declaration'],
        classPatterns: ['class_declaration', 'struct_declaration', 'protocol_declaration'],
    },
    kotlin: {
        language: 'kotlin',
        extensions: ['.kt', '.kts'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_header'],
        functionPatterns: ['function_declaration'],
        classPatterns: ['class_declaration', 'interface_declaration'],
    },
    scala: {
        language: 'scala',
        extensions: ['.scala', '.sc'],
        commentPatterns: { line: '//', blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_declaration'],
        functionPatterns: ['function_definition'],
        classPatterns: ['class_definition', 'trait_definition', 'object_definition'],
    },
    html: {
        language: 'html',
        extensions: ['.html', '.htm'],
        commentPatterns: { blockStart: '<!--', blockEnd: '-->' },
        importPatterns: [],
        functionPatterns: [],
        classPatterns: [],
    },
    css: {
        language: 'css',
        extensions: ['.css'],
        commentPatterns: { blockStart: '/*', blockEnd: '*/' },
        importPatterns: ['import_statement'],
        functionPatterns: [],
        classPatterns: ['rule_set'],
    },
    json: {
        language: 'json',
        extensions: ['.json'],
        commentPatterns: {},
        importPatterns: [],
        functionPatterns: [],
        classPatterns: [],
    },
    yaml: {
        language: 'yaml',
        extensions: ['.yaml', '.yml'],
        commentPatterns: { line: '#' },
        importPatterns: [],
        functionPatterns: [],
        classPatterns: [],
    },
    markdown: {
        language: 'markdown',
        extensions: ['.md', '.markdown'],
        commentPatterns: {},
        importPatterns: [],
        functionPatterns: [],
        classPatterns: [],
    },
};
/**
 * 根据文件扩展名获取语言
 */
export function getLanguageFromExtension(filename) {
    const ext = filename.slice(filename.lastIndexOf('.')).toLowerCase();
    for (const [lang, config] of Object.entries(LANGUAGE_CONFIGS)) {
        if (config.extensions.includes(ext)) {
            return lang;
        }
    }
    return null;
}
//# sourceMappingURL=types.js.map