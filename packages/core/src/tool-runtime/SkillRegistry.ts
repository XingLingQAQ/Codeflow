import type {
  RegisterSkillOptions,
  SkillManifest,
  SkillRegistration,
  SkillRegistryFilter,
} from './types.js';

function matchesRole(skill: SkillManifest, role?: string): boolean {
  if (!role || !skill.applicableRoles || skill.applicableRoles.length === 0) {
    return true;
  }
  return skill.applicableRoles.includes(role);
}

export class SkillRegistry {
  private readonly skills = new Map<string, SkillRegistration>();
  private readonly aliases = new Map<string, string>();

  register(skill: SkillRegistration, options: RegisterSkillOptions = {}): void {
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

  resolve(skillIdOrAlias: string, version?: string): SkillRegistration | undefined {
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

  get(skillId: string, version: string): SkillRegistration | undefined {
    return this.skills.get(this.createVersionKey(skillId, version));
  }

  has(skillIdOrAlias: string, version?: string): boolean {
    return Boolean(this.resolve(skillIdOrAlias, version));
  }

  list(filter: SkillRegistryFilter = {}): SkillManifest[] {
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

  deprecate(skillId: string, version: string): boolean {
    const registration = this.get(skillId, version);
    if (!registration) {
      return false;
    }

    registration.manifest.deprecated = true;
    return true;
  }

  remove(skillId: string, version: string): boolean {
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

  clear(): void {
    this.skills.clear();
    this.aliases.clear();
  }

  private createVersionKey(skillId: string, version: string): string {
    return `${skillId}@${version}`;
  }
}
