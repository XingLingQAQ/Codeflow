import type { RegisterSkillOptions, SkillManifest, SkillRegistration, SkillRegistryFilter } from './types.js';
export declare class SkillRegistry {
    private readonly skills;
    private readonly aliases;
    register(skill: SkillRegistration, options?: RegisterSkillOptions): void;
    resolve(skillIdOrAlias: string, version?: string): SkillRegistration | undefined;
    get(skillId: string, version: string): SkillRegistration | undefined;
    has(skillIdOrAlias: string, version?: string): boolean;
    list(filter?: SkillRegistryFilter): SkillManifest[];
    deprecate(skillId: string, version: string): boolean;
    remove(skillId: string, version: string): boolean;
    clear(): void;
    private createVersionKey;
}
//# sourceMappingURL=SkillRegistry.d.ts.map