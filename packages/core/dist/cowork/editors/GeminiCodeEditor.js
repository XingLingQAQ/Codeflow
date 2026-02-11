/**
 * Gemini Code Editor
 * 基于 GeminiAdapter 实现 ICodeEditor 接口
 * 复用 Claude Editor 的 Prompt 模板
 */
import { readFile, writeFile, copyFile, unlink, mkdir } from 'fs/promises';
import { existsSync } from 'fs';
import { join, dirname } from 'path';
/**
 * Prompt 模板（与 Claude Editor 一致）
 */
const EDIT_PROMPT_TEMPLATE = `You are a code editing assistant. Your task is to modify the given code according to the instruction.

## Rules:
1. Output ONLY the unified diff format
2. Use standard diff format with --- and +++ headers
3. Include line numbers in @@ markers
4. Do not include any explanation, just the diff

## Input File: {file}

## Current Content:
\`\`\`
{content}
\`\`\`

## Instruction:
{instruction}

## Output (unified diff only):`;
const PREVIEW_PROMPT_TEMPLATE = `You are a code editing assistant. Preview the changes that would be made to the given code.

## Rules:
1. Output ONLY the unified diff format
2. Use standard diff format with --- and +++ headers
3. Include line numbers in @@ markers
4. Do not actually apply changes, just show what would change

## Input File: {file}

## Current Content:
\`\`\`
{content}
\`\`\`

## Instruction:
{instruction}

## Output (unified diff only):`;
/**
 * Gemini Code Editor
 */
export class GeminiCodeEditor {
    constructor(adapter, config = {}) {
        this.name = 'gemini-editor';
        this.backupStack = [];
        this.adapter = adapter;
        this.config = {
            autoBackup: true,
            backupDir: '.gemini-backups',
            model: 'gemini-2.0-flash-exp',
            maxTokens: 8192,
            temperature: 0.2,
            ...config,
        };
    }
    /**
     * 编辑单个文件
     */
    async edit(file, instruction) {
        const fullPath = this.resolvePath(file);
        // 读取文件内容
        let content = '';
        if (existsSync(fullPath)) {
            content = await readFile(fullPath, 'utf-8');
            // 备份原文件
            if (this.config.autoBackup) {
                await this.backup(fullPath);
            }
        }
        // 构建 prompt
        const prompt = EDIT_PROMPT_TEMPLATE.replace('{file}', file)
            .replace('{content}', content)
            .replace('{instruction}', instruction);
        try {
            const response = await this.adapter.send(prompt, {
                model: this.config.model,
                maxTokens: this.config.maxTokens,
                temperature: this.config.temperature,
            });
            // 解析 diff
            const diff = this.parseDiff(response.content, file);
            if (diff.hunks.length === 0) {
                return {
                    success: true,
                    file,
                    diff: this.emptyDiff(file),
                    message: 'No changes made',
                };
            }
            // 应用 diff
            await this.applyDiff(file, diff);
            return {
                success: true,
                file,
                diff,
            };
        }
        catch (error) {
            return {
                success: false,
                file,
                diff: this.emptyDiff(file),
                message: error instanceof Error ? error.message : 'Unknown error',
            };
        }
    }
    /**
     * 编辑多个文件
     */
    async editMultiple(files, instruction) {
        const results = [];
        for (const file of files) {
            const result = await this.edit(file, instruction);
            results.push(result);
        }
        return results;
    }
    /**
     * 预览修改（不实际写入）
     */
    async preview(file, instruction) {
        const fullPath = this.resolvePath(file);
        // 读取文件内容
        let content = '';
        if (existsSync(fullPath)) {
            content = await readFile(fullPath, 'utf-8');
        }
        // 构建 prompt
        const prompt = PREVIEW_PROMPT_TEMPLATE.replace('{file}', file)
            .replace('{content}', content)
            .replace('{instruction}', instruction);
        try {
            const response = await this.adapter.send(prompt, {
                model: this.config.model,
                maxTokens: this.config.maxTokens,
                temperature: this.config.temperature,
            });
            return this.parseDiff(response.content, file);
        }
        catch {
            return this.emptyDiff(file);
        }
    }
    /**
     * 应用 Diff
     */
    async applyDiff(file, diff) {
        const fullPath = this.resolvePath(file);
        // 备份原文件
        if (this.config.autoBackup && existsSync(fullPath)) {
            await this.backup(fullPath);
        }
        // 读取原文件
        const content = existsSync(fullPath) ? await readFile(fullPath, 'utf-8') : '';
        const lines = content.split('\n');
        // 应用 hunks（从后往前应用，避免行号偏移）
        const sortedHunks = [...diff.hunks].sort((a, b) => b.oldStart - a.oldStart);
        for (const hunk of sortedHunks) {
            const hunkLines = hunk.content.split('\n').filter((l) => l.length > 0);
            const newLines = [];
            for (const line of hunkLines) {
                if (line.startsWith('+')) {
                    newLines.push(line.slice(1));
                }
                else if (line.startsWith('-')) {
                    // 删除行，不添加
                }
                else if (line.startsWith(' ')) {
                    newLines.push(line.slice(1));
                }
                else {
                    newLines.push(line);
                }
            }
            // 替换对应行
            lines.splice(hunk.oldStart - 1, hunk.oldLines, ...newLines);
        }
        // 确保目录存在
        await mkdir(dirname(fullPath), { recursive: true });
        // 写入文件
        await writeFile(fullPath, lines.join('\n'), 'utf-8');
    }
    /**
     * 撤销上一次修改
     */
    async undo() {
        const lastBackup = this.backupStack.pop();
        if (!lastBackup) {
            throw new Error('No backup available to undo');
        }
        // 恢复文件
        await copyFile(lastBackup.backupPath, lastBackup.file);
        // 删除备份
        await unlink(lastBackup.backupPath);
    }
    /**
     * 获取备份栈
     */
    getBackupStack() {
        return [...this.backupStack];
    }
    /**
     * 清理所有备份
     */
    async clearBackups() {
        for (const backup of this.backupStack) {
            try {
                await unlink(backup.backupPath);
            }
            catch {
                // 忽略删除失败
            }
        }
        this.backupStack = [];
    }
    // ==================== 私有方法 ====================
    resolvePath(file) {
        if (this.config.cwd) {
            return join(this.config.cwd, file);
        }
        return file;
    }
    async backup(file) {
        const timestamp = Date.now();
        const backupDir = this.config.backupDir || '.gemini-backups';
        const backupPath = join(dirname(file), backupDir, `${timestamp}_${file.replace(/[/\\]/g, '_')}`);
        // 确保备份目录存在
        await mkdir(dirname(backupPath), { recursive: true });
        // 复制文件
        await copyFile(file, backupPath);
        // 记录备份
        this.backupStack.push({
            file,
            backupPath,
            timestamp,
        });
    }
    emptyDiff(file) {
        return {
            file,
            hunks: [],
            additions: 0,
            deletions: 0,
        };
    }
    /**
     * 解析 unified diff 格式
     */
    parseDiff(output, file) {
        const hunks = [];
        let additions = 0;
        let deletions = 0;
        const lines = output.split('\n');
        let currentHunk = null;
        let hunkContent = [];
        for (const line of lines) {
            if (line.match(/@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@/)) {
                // 保存前一个 hunk
                if (currentHunk) {
                    currentHunk.content = hunkContent.join('\n');
                    hunks.push(currentHunk);
                }
                // 解析新 hunk
                const match = line.match(/@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@/);
                if (match) {
                    currentHunk = {
                        oldStart: parseInt(match[1], 10),
                        oldLines: match[2] ? parseInt(match[2], 10) : 1,
                        newStart: parseInt(match[3], 10),
                        newLines: match[4] ? parseInt(match[4], 10) : 1,
                        content: '',
                    };
                    hunkContent = [];
                }
            }
            else if (currentHunk) {
                // 收集 hunk 内容
                if (line.startsWith('+') && !line.startsWith('+++')) {
                    additions++;
                    hunkContent.push(line);
                }
                else if (line.startsWith('-') && !line.startsWith('---')) {
                    deletions++;
                    hunkContent.push(line);
                }
                else if (line.startsWith(' ') || line === '') {
                    hunkContent.push(line);
                }
            }
        }
        // 保存最后一个 hunk
        if (currentHunk) {
            currentHunk.content = hunkContent.join('\n');
            hunks.push(currentHunk);
        }
        return {
            file,
            hunks,
            additions,
            deletions,
        };
    }
}
//# sourceMappingURL=GeminiCodeEditor.js.map