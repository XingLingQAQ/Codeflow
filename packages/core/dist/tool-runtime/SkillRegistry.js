function matchesRole(skill, role) {
    if (!role || !skill.applicableRoles || skill.applicableRoles.length === 0) {
        return true;
    }
    return skill.applicableRoles.includes(role);
}
export class SkillRegistry {
    constructor() {
        this.skills = new Map();
        this.aliases = new Map();
    }
    register(skill, options = {}) {
        const key = this.createVersionKey(skill.manifest.skillId, skill.manifest.version);
        if (this.skills.has(key) && !options.replace) {
            throw new Error(`Skill already registered: ${skill.manifest.skillId}@${skill.manifest.version}`);
        }
        this.skills.set(key, skill);
        this.aliases.set(skill.manifest.skillId, key);
        for (const alias of skill.manifest.aliases ?? []) {
            this.aliases.set(alias, key);
        }
    }
    resolve(skillIdOrAlias, version) {
        if (version) {
            return this.skills.get(this.createVersionKey(skillIdOrAlias, version));
        }
        const direct = this.skills.get(skillIdOrAlias);
        if (direct) {
            return direct;
        }
        const aliasKey = this.aliases.get(skillIdOrAlias);
        if (!aliasKey) {
            return undefined;
        }
        return this.skills.get(aliasKey);
    }
    get(skillId, version) {
        return this.skills.get(this.createVersionKey(skillId, version));
    }
    has(skillIdOrAlias, version) {
        return Boolean(this.resolve(skillIdOrAlias, version));
    }
    list(filter = {}) {
        return Array.from(this.skills.values())
            .map((skill) => skill.manifest)
            .filter((skill) => {
            if (!filter.includeDeprecated && skill.deprecated) {
                return false;
            }
            if (filter.entryPoint && !skill.entryPoints.includes(filter.entryPoint)) {
                return false;
            }
            if (filter.tag && !skill.tags.includes(filter.tag)) {
                return false;
            }
            if (filter.riskLevel && skill.riskLevel !== filter.riskLevel) {
                return false;
            }
            if (filter.source && skill.source !== filter.source) {
                return false;
            }
            if (!matchesRole(skill, filter.role)) {
                return false;
            }
            return true;
        });
    }
    deprecate(skillId, version) {
        const registration = this.get(skillId, version);
        if (!registration) {
            return false;
        }
        registration.manifest.deprecated = true;
        return true;
    }
    remove(skillId, version) {
        const key = this.createVersionKey(skillId, version);
        const registration = this.skills.get(key);
        if (!registration) {
            return false;
        }
        this.skills.delete(key);
        for (const [alias, aliasKey] of Array.from(this.aliases.entries())) {
            if (aliasKey === key) {
                this.aliases.delete(alias);
            }
        }
        return true;
    }
    clear() {
        this.skills.clear();
        this.aliases.clear();
    }
    createVersionKey(skillId, version) {
        return `${skillId}@${version}`;
    }
}
//# sourceMappingURL=SkillRegistry.js.map