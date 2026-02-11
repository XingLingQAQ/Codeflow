/**
 * UserProfileService - 用户画像动态更新服务
 *
 * 基于最近原子记忆，使用 LLM 分析并自动更新用户画像。
 * 支持本地存储、后端同步、定期更新触发。
 */
const DEFAULT_CONFIG = {
    updateIntervalSec: 3600,
    recentMemoryLimit: 20,
    baseUrl: 'http://localhost:8080',
    timeoutMs: 10000,
};
const PROFILE_UPDATE_PROMPT = `你是用户画像分析助手。请根据用户最近的对话记忆，更新用户画像。

当前画像：
{CURRENT_PROFILE}

最近记忆：
{RECENT_MEMORIES}

请输出更新后的画像 JSON（不要输出 markdown 或额外解释）：
{
  "preferences": "用户偏好描述",
  "background": "用户背景描述",
  "expertise": ["专业领域1", "专业领域2"],
  "communicationStyle": "沟通风格描述",
  "goals": ["目标1", "目标2"]
}

约束：
- 保留已有信息，仅在有新证据时更新
- expertise 和 goals 最多各 10 项
- 若无足够信息更新某字段，保持原值
- 输出纯 JSON，不要包裹 markdown`;
/**
 * 内存画像存储（本地缓存）
 */
export class InMemoryProfileStorage {
    constructor() {
        this.profiles = new Map();
    }
    async load(userId) {
        return this.profiles.get(userId) || null;
    }
    async save(profile) {
        this.profiles.set(profile.userId, { ...profile });
    }
    async delete(userId) {
        this.profiles.delete(userId);
    }
}
/**
 * UserProfileService - 用户画像动态更新服务
 */
export class UserProfileService {
    constructor(llmAdapter, memoryService, storage, config = {}) {
        this.llmAdapter = llmAdapter;
        this.memoryService = memoryService;
        this.storage = storage || new InMemoryProfileStorage();
        this.config = { ...DEFAULT_CONFIG, ...config };
    }
    /**
     * 获取用户画像
     */
    async getProfile(userId) {
        return this.storage.load(userId);
    }
    /**
     * 基于最近记忆更新用户画像
     * 使用 LLM 分析最近记忆并更新画像各分区
     */
    async update(userId, sessionId) {
        const currentProfile = await this.storage.load(userId);
        const recentMemories = await this.fetchRecentMemories(sessionId);
        if (recentMemories.length === 0 && currentProfile) {
            return currentProfile;
        }
        const updatedSections = await this.analyzeWithLLM(currentProfile?.sections || this.emptyProfileSections(), recentMemories);
        const profile = {
            userId,
            lastUpdated: Math.floor(Date.now() / 1000),
            sections: updatedSections,
            metadata: {
                totalSessions: (currentProfile?.metadata.totalSessions || 0) + 1,
                totalMessages: (currentProfile?.metadata.totalMessages || 0) + recentMemories.length,
                lastActive: Math.floor(Date.now() / 1000),
            },
        };
        await this.storage.save(profile);
        return profile;
    }
    /**
     * 启动定期更新触发器
     */
    startPeriodicUpdate(userId, sessionId) {
        this.stopPeriodicUpdate();
        this.updateTimer = setInterval(() => {
            void this.update(userId, sessionId).catch(() => {
                // 定期更新失败不阻塞主流程
            });
        }, this.config.updateIntervalSec * 1000);
    }
    /**
     * 停止定期更新触发器
     */
    stopPeriodicUpdate() {
        if (this.updateTimer) {
            clearInterval(this.updateTimer);
            this.updateTimer = undefined;
        }
    }
    /**
     * 保存画像（直接写入存储）
     */
    async saveProfile(profile) {
        await this.storage.save(profile);
    }
    /**
     * 删除画像
     */
    async deleteProfile(userId) {
        await this.storage.delete(userId);
    }
    /**
     * 获取最近记忆
     */
    async fetchRecentMemories(sessionId) {
        try {
            const memories = await this.memoryService.getBySession(sessionId);
            return memories
                .slice(0, this.config.recentMemoryLimit)
                .map((m) => ({ content: m.content, tags: m.tags }));
        }
        catch {
            return [];
        }
    }
    /**
     * 使用 LLM 分析记忆并更新画像
     */
    async analyzeWithLLM(currentSections, recentMemories) {
        if (recentMemories.length === 0) {
            return currentSections;
        }
        const prompt = PROFILE_UPDATE_PROMPT
            .replace('{CURRENT_PROFILE}', JSON.stringify(currentSections, null, 2))
            .replace('{RECENT_MEMORIES}', recentMemories.map((m, i) => `${i + 1}. ${m.content} [${m.tags.join(', ')}]`).join('\n'));
        const response = await this.llmAdapter.send(prompt, {
            temperature: 0.3,
            maxTokens: 1000,
        });
        return this.parseProfileResponse(response, currentSections);
    }
    /**
     * 解析 LLM 响应为画像分区
     */
    parseProfileResponse(response, fallback) {
        const raw = response.content || '';
        const jsonCandidate = this.extractJsonObject(raw);
        if (!jsonCandidate) {
            return fallback;
        }
        try {
            const parsed = JSON.parse(jsonCandidate);
            return {
                preferences: typeof parsed.preferences === 'string' ? parsed.preferences : fallback.preferences,
                background: typeof parsed.background === 'string' ? parsed.background : fallback.background,
                expertise: Array.isArray(parsed.expertise)
                    ? parsed.expertise.filter((e) => typeof e === 'string').slice(0, 10)
                    : fallback.expertise,
                communicationStyle: typeof parsed.communicationStyle === 'string'
                    ? parsed.communicationStyle
                    : fallback.communicationStyle,
                goals: Array.isArray(parsed.goals)
                    ? parsed.goals.filter((g) => typeof g === 'string').slice(0, 10)
                    : fallback.goals,
            };
        }
        catch {
            return fallback;
        }
    }
    /**
     * 从 LLM 输出中提取 JSON 对象
     */
    extractJsonObject(raw) {
        if (!raw.trim())
            return null;
        const fenced = raw.match(/```(?:json)?\s*([\s\S]*?)\s*```/i);
        if (fenced?.[1]) {
            const candidate = fenced[1].trim();
            if (candidate.startsWith('{') && candidate.endsWith('}')) {
                return candidate;
            }
        }
        const start = raw.indexOf('{');
        const end = raw.lastIndexOf('}');
        if (start >= 0 && end > start) {
            return raw.slice(start, end + 1);
        }
        return null;
    }
    /**
     * 空画像分区
     */
    emptyProfileSections() {
        return {
            preferences: '',
            background: '',
            expertise: [],
            communicationStyle: '',
            goals: [],
        };
    }
}
//# sourceMappingURL=UserProfileService.js.map