import { EventEmitter } from 'events';
import { exec } from 'child_process';
import { promisify } from 'util';
import * as path from 'path';
import * as fs from 'fs';
const execAsync = promisify(exec);
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
    baseDir: '.codeflow/worktrees',
    worktreePrefix: 'worker',
    maxWorktrees: 10,
    autoCleanup: true,
    lockTimeout: 3600000, // 1 hour
};
/**
 * Worktree 管理器
 * 管理 Git Worktree 的生命周期，支持并行开发
 */
export class WorktreeManager extends EventEmitter {
    constructor(repoRoot, config = {}) {
        super();
        this.worktrees = new Map();
        // 统一使用正斜杠
        this.repoRoot = path.resolve(repoRoot).replace(/\\/g, '/');
        this.config = { ...DEFAULT_CONFIG, ...config };
    }
    /**
     * 初始化管理器
     */
    async initialize() {
        // 验证是否是 Git 仓库
        if (!await this.isGitRepo()) {
            throw new Error(`Not a git repository: ${this.repoRoot}`);
        }
        // 确保 worktree 目录存在
        const worktreeDir = path.join(this.repoRoot, this.config.baseDir);
        if (!fs.existsSync(worktreeDir)) {
            fs.mkdirSync(worktreeDir, { recursive: true });
        }
        // 加载现有 worktrees
        await this.refresh();
    }
    /**
     * 检查是否是 Git 仓库
     */
    async isGitRepo() {
        try {
            await this.execGit('rev-parse --is-inside-work-tree');
            return true;
        }
        catch {
            return false;
        }
    }
    /**
     * 创建新的 Worktree
     */
    async createWorktree(name, options = {}) {
        // 检查是否超过最大数量
        if (this.worktrees.size >= this.config.maxWorktrees) {
            if (this.config.autoCleanup) {
                await this.cleanupPrunable();
            }
            if (this.worktrees.size >= this.config.maxWorktrees) {
                throw new Error(`Maximum worktrees (${this.config.maxWorktrees}) reached`);
            }
        }
        const worktreePath = path.join(this.repoRoot, this.config.baseDir, name);
        // 检查路径是否已存在
        if (fs.existsSync(worktreePath)) {
            throw new Error(`Worktree path already exists: ${worktreePath}`);
        }
        // 构建 git worktree add 命令
        const args = ['worktree', 'add'];
        if (options.force) {
            args.push('--force');
        }
        if (options.lock) {
            args.push('--lock');
        }
        if (options.detach) {
            args.push('--detach');
        }
        args.push(`"${worktreePath}"`);
        if (options.createBranch && options.branch) {
            args.push('-b', options.branch);
            if (options.baseBranch) {
                args.push(options.baseBranch);
            }
        }
        else if (options.branch) {
            args.push(options.branch);
        }
        else {
            // 创建新分支，基于当前 HEAD
            const branchName = `${this.config.worktreePrefix}-${name}-${Date.now()}`;
            args.push('-b', branchName);
        }
        try {
            await this.execGit(args.join(' '));
            // 刷新并获取新创建的 worktree 信息
            await this.refresh();
            const info = this.findWorktreeByPath(worktreePath);
            if (info) {
                this.emit('worktree:created', info);
                return info;
            }
            throw new Error('Worktree created but not found in list');
        }
        catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            this.emit('worktree:error', err, 'create');
            throw err;
        }
    }
    /**
     * 移除 Worktree
     */
    async removeWorktree(pathOrName, force = false) {
        const worktreePath = this.resolveWorktreePath(pathOrName);
        const info = this.findWorktreeByPath(worktreePath);
        if (!info) {
            return false;
        }
        // 不能移除主 worktree
        if (info.isMain) {
            throw new Error('Cannot remove main worktree');
        }
        // 如果被锁定且不强制，则失败
        if (info.isLocked && !force) {
            throw new Error(`Worktree is locked: ${worktreePath}`);
        }
        try {
            const args = ['worktree', 'remove'];
            if (force) {
                // 需要两个 --force 来移除锁定的 worktree
                args.push('--force', '--force');
            }
            args.push(`"${info.path}"`);
            await this.execGit(args.join(' '));
            this.worktrees.delete(info.path);
            this.emit('worktree:removed', info.path);
            return true;
        }
        catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            this.emit('worktree:error', err, 'remove');
            throw err;
        }
    }
    /**
     * 列出所有 Worktrees
     */
    async listWorktrees() {
        await this.refresh();
        return Array.from(this.worktrees.values());
    }
    /**
     * 获取 Worktree 信息
     */
    getWorktree(pathOrName) {
        const worktreePath = this.resolveWorktreePath(pathOrName);
        return this.findWorktreeByPath(worktreePath);
    }
    /**
     * 锁定 Worktree
     */
    async lockWorktree(pathOrName, reason) {
        const worktreePath = this.resolveWorktreePath(pathOrName);
        const info = this.findWorktreeByPath(worktreePath);
        if (!info) {
            return false;
        }
        try {
            const args = ['worktree', 'lock'];
            if (reason) {
                args.push('--reason', `"${reason}"`);
            }
            args.push(`"${info.path}"`);
            await this.execGit(args.join(' '));
            info.isLocked = true;
            this.emit('worktree:locked', info.path);
            return true;
        }
        catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            this.emit('worktree:error', err, 'lock');
            return false;
        }
    }
    /**
     * 解锁 Worktree
     */
    async unlockWorktree(pathOrName) {
        const worktreePath = this.resolveWorktreePath(pathOrName);
        const info = this.findWorktreeByPath(worktreePath);
        if (!info) {
            return false;
        }
        try {
            await this.execGit(`worktree unlock "${info.path}"`);
            info.isLocked = false;
            this.emit('worktree:unlocked', info.path);
            return true;
        }
        catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            this.emit('worktree:error', err, 'unlock');
            return false;
        }
    }
    /**
     * 清理可修剪的 Worktrees
     */
    async cleanupPrunable() {
        try {
            await this.execGit('worktree prune');
            const beforeCount = this.worktrees.size;
            await this.refresh();
            return beforeCount - this.worktrees.size;
        }
        catch {
            return 0;
        }
    }
    /**
     * 移除所有非主 Worktrees
     */
    async removeAllWorktrees(force = false) {
        let removed = 0;
        const worktrees = Array.from(this.worktrees.values());
        for (const info of worktrees) {
            if (!info.isMain) {
                try {
                    await this.removeWorktree(info.path, force);
                    removed++;
                }
                catch {
                    // Continue with other worktrees
                }
            }
        }
        return removed;
    }
    /**
     * 获取 Worktree 数量
     */
    getWorktreeCount() {
        return this.worktrees.size;
    }
    /**
     * 检查 Worktree 是否存在
     */
    hasWorktree(pathOrName) {
        const worktreePath = this.resolveWorktreePath(pathOrName);
        return this.findWorktreeByPath(worktreePath) !== undefined;
    }
    /**
     * 在 Worktree 中执行 Git 命令
     */
    async execInWorktree(pathOrName, command) {
        const worktreePath = this.resolveWorktreePath(pathOrName);
        const info = this.findWorktreeByPath(worktreePath);
        if (!info) {
            throw new Error(`Worktree not found: ${pathOrName}`);
        }
        try {
            const { stdout } = await execAsync(command, {
                cwd: info.path,
                encoding: 'utf-8',
            });
            return stdout.trim();
        }
        catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            throw err;
        }
    }
    /**
     * 获取 Worktree 的当前分支
     */
    async getWorktreeBranch(pathOrName) {
        try {
            const result = await this.execInWorktree(pathOrName, 'git rev-parse --abbrev-ref HEAD');
            return result || null;
        }
        catch {
            return null;
        }
    }
    /**
     * 获取 Worktree 的当前 commit
     */
    async getWorktreeCommit(pathOrName) {
        try {
            const result = await this.execInWorktree(pathOrName, 'git rev-parse HEAD');
            return result || null;
        }
        catch {
            return null;
        }
    }
    /**
     * 刷新 Worktree 列表
     */
    async refresh() {
        this.worktrees.clear();
        try {
            const output = await this.execGit('worktree list --porcelain');
            // 按空行分割条目（处理 Windows 和 Unix 换行符）
            const entries = output.split(/\r?\n\r?\n+/).filter(e => e.trim());
            for (const entry of entries) {
                const info = this.parseWorktreeEntry(entry);
                if (info) {
                    // 使用规范化路径作为 key
                    const normalizedPath = this.normalizePath(info.path);
                    this.worktrees.set(normalizedPath, { ...info, path: normalizedPath });
                }
            }
        }
        catch {
            // If worktree list fails, just clear the map
        }
    }
    /**
     * 解析 Worktree 条目
     */
    parseWorktreeEntry(entry) {
        const lines = entry.split(/\r?\n/).map(l => l.trim()).filter(l => l);
        const info = {
            isMain: false,
            isLocked: false,
            prunable: false,
        };
        for (const line of lines) {
            if (line.startsWith('worktree ')) {
                info.path = this.normalizePath(line.substring(9).trim());
            }
            else if (line.startsWith('HEAD ')) {
                info.commit = line.substring(5).trim();
            }
            else if (line.startsWith('branch ')) {
                info.branch = line.substring(7).trim().replace('refs/heads/', '');
            }
            else if (line === 'bare') {
                info.isMain = true;
            }
            else if (line === 'locked') {
                info.isLocked = true;
            }
            else if (line === 'prunable') {
                info.prunable = true;
            }
            else if (line === 'detached') {
                info.branch = 'HEAD (detached)';
            }
        }
        // 主 worktree 是第一个（路径是仓库根目录）
        if (info.path && this.pathsEqual(info.path, this.repoRoot)) {
            info.isMain = true;
        }
        if (info.path && info.commit) {
            return info;
        }
        return null;
    }
    /**
     * 规范化路径（统一使用正斜杠）
     */
    normalizePath(p) {
        // 统一使用正斜杠，与 git 输出一致
        return path.resolve(p).replace(/\\/g, '/');
    }
    /**
     * 规范化路径用于比较（处理 Windows 短路径名）
     */
    normalizePathForCompare(p) {
        // 统一使用正斜杠并转小写
        return p.replace(/\\/g, '/').toLowerCase();
    }
    /**
     * 检查两个路径是否等价（处理 Windows 短路径名如 ADMINI~1）
     */
    pathsEqual(p1, p2) {
        const n1 = this.normalizePathForCompare(p1);
        const n2 = this.normalizePathForCompare(p2);
        if (n1 === n2)
            return true;
        // 在 Windows 上处理短路径名
        if (process.platform === 'win32') {
            const parts1 = n1.split('/');
            const parts2 = n2.split('/');
            if (parts1.length !== parts2.length)
                return false;
            for (let i = 0; i < parts1.length; i++) {
                const p1Part = parts1[i];
                const p2Part = parts2[i];
                if (p1Part === p2Part)
                    continue;
                // 检查是否是短路径名匹配（如 admini~1 vs administrator）
                const shortMatch1 = p1Part.match(/^(.+)~\d+$/);
                const shortMatch2 = p2Part.match(/^(.+)~\d+$/);
                if (shortMatch1 && p2Part.startsWith(shortMatch1[1]))
                    continue;
                if (shortMatch2 && p1Part.startsWith(shortMatch2[1]))
                    continue;
                return false;
            }
            return true;
        }
        return false;
    }
    /**
     * 在 worktrees Map 中查找路径（处理 Windows 短路径名）
     */
    findWorktreeByPath(targetPath) {
        // 先尝试直接查找
        const direct = this.worktrees.get(targetPath);
        if (direct)
            return direct;
        // 在 Windows 上使用路径比较
        if (process.platform === 'win32') {
            for (const [storedPath, info] of this.worktrees) {
                if (this.pathsEqual(storedPath, targetPath)) {
                    return info;
                }
            }
        }
        return undefined;
    }
    /**
     * 解析 Worktree 路径
     */
    resolveWorktreePath(pathOrName) {
        // 如果是绝对路径，直接规范化返回
        if (path.isAbsolute(pathOrName)) {
            return this.normalizePath(pathOrName);
        }
        // 如果是相对路径且存在，解析为绝对路径
        const relativePath = path.join(this.repoRoot, pathOrName);
        if (fs.existsSync(relativePath)) {
            return this.normalizePath(relativePath);
        }
        // 否则假设是名称，构建完整路径
        return this.normalizePath(path.join(this.repoRoot, this.config.baseDir, pathOrName));
    }
    /**
     * 执行 Git 命令
     */
    async execGit(command) {
        try {
            const { stdout } = await execAsync(`git ${command}`, {
                cwd: this.repoRoot,
                encoding: 'utf-8',
            });
            return stdout.trim();
        }
        catch (error) {
            if (error && typeof error === 'object' && 'stderr' in error) {
                throw new Error(error.stderr);
            }
            throw error;
        }
    }
}
//# sourceMappingURL=WorktreeManager.js.map