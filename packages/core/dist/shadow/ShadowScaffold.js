import * as fs from 'fs';
import * as path from 'path';
export class ShadowScaffold {
    constructor() {
        this.shadowRoot = '.codeflow';
    }
    async initialize(projectRoot) {
        const normalizedProjectRoot = path.resolve(projectRoot);
        const shadowPath = path.join(normalizedProjectRoot, this.shadowRoot);
        // 1. 创建目录结构（幂等）
        await fs.promises.mkdir(path.join(shadowPath, 'domain'), { recursive: true });
        await fs.promises.mkdir(path.join(shadowPath, 'governance'), { recursive: true });
        await fs.promises.mkdir(path.join(shadowPath, 'registry'), { recursive: true });
        // 2. 创建/更新配置文件
        const config = this.buildDefaultConfig(normalizedProjectRoot);
        const configPath = path.join(shadowPath, 'config.json');
        await fs.promises.writeFile(configPath, `${JSON.stringify(config, null, 2)}\n`, 'utf-8');
        // 3. 更新 .gitignore 确保 .codeflow 被追踪
        await this.updateGitignore(normalizedProjectRoot);
    }
    buildDefaultConfig(projectRoot) {
        return {
            version: '1.0.0',
            projectRoot,
            autoSync: true,
            intentProjection: {
                enabled: true,
                languages: ['typescript', 'javascript', 'go', 'python'],
            },
            registry: {
                apiRegistry: true,
                modelDictionary: true,
            },
        };
    }
    async updateGitignore(projectRoot) {
        const gitignorePath = path.join(projectRoot, '.gitignore');
        let content = '';
        try {
            content = await fs.promises.readFile(gitignorePath, 'utf-8');
        }
        catch (error) {
            if (error.code !== 'ENOENT') {
                throw error;
            }
        }
        const lines = content
            .split(/\r?\n/)
            .map((line) => line.trim())
            .filter((line) => line.length > 0 && line !== '.codeflow' && line !== '.codeflow/');
        const marker = '# CodeFlow shadow system (tracked)';
        if (!lines.includes(marker)) {
            lines.push(marker);
        }
        const next = `${lines.join('\n')}\n`;
        await fs.promises.writeFile(gitignorePath, next, 'utf-8');
    }
}
//# sourceMappingURL=ShadowScaffold.js.map