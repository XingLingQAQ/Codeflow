/**
 * 三元组提取器
 * 从文本中提取 S-P-O 三元组
 */
import { PREDICATES, SAMG_ENTITY_TYPES, generateTripleId, generateEntityId, createNode, } from './types.js';
const DEFAULT_CONFIG = {
    minConfidence: 0.5,
    maxTriplesPerExtraction: 50,
    enableRuleBasedExtraction: true,
};
export class TripleExtractor {
    constructor(config = {}) {
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.adapter = config.adapter;
    }
    async extract(text, context) {
        const source = {
            sessionId: context.sessionId,
            messageIndex: context.messageIndex,
            agentRole: context.agentRole,
            gitCommitHash: context.gitCommitHash,
            extractionMethod: 'rule',
        };
        let triples = [];
        if (this.config.enableRuleBasedExtraction) {
            const ruleTriples = this.extractByRules(text, source);
            triples.push(...ruleTriples);
        }
        if (this.adapter) {
            try {
                const llmTriples = await this.extractByLLM(text, source);
                triples.push(...llmTriples);
            }
            catch {
                // LLM extraction failed, continue with rule-based results
            }
        }
        triples = triples.filter(t => t.confidence >= this.config.minConfidence);
        triples = this.deduplicateTriples(triples);
        return triples.slice(0, this.config.maxTriplesPerExtraction);
    }
    extractByRules(text, source) {
        const triples = [];
        const ruleSource = { ...source, extractionMethod: 'rule' };
        triples.push(...this.extractCodeRelations(text, ruleSource));
        triples.push(...this.extractDecisionRelations(text, ruleSource));
        triples.push(...this.extractFileRelations(text, ruleSource));
        return triples;
    }
    extractCodeRelations(text, source) {
        const triples = [];
        // Pattern: "X calls Y" / "X 调用 Y"
        const callPatterns = [
            /(\w+)\s+(?:calls?|invokes?|调用)\s+(\w+)/gi,
            /(\w+)\s*\(\s*\)\s*(?:calls?|invokes?)\s+(\w+)/gi,
        ];
        for (const pattern of callPatterns) {
            let match;
            while ((match = pattern.exec(text)) !== null) {
                const [, caller, callee] = match;
                triples.push(this.createTriple(caller, SAMG_ENTITY_TYPES.FUNCTION, PREDICATES.CALLS, callee, SAMG_ENTITY_TYPES.FUNCTION, 0.7, source));
            }
        }
        // Pattern: "X imports Y" / "X 导入 Y"
        const importPatterns = [
            /import\s+(?:\{[^}]+\}|[\w*]+)\s+from\s+['"]([^'"]+)['"]/gi,
            /require\s*\(\s*['"]([^'"]+)['"]\s*\)/gi,
        ];
        for (const pattern of importPatterns) {
            let match;
            while ((match = pattern.exec(text)) !== null) {
                const [fullMatch, moduleName] = match;
                const currentModule = 'currentModule';
                triples.push(this.createTriple(currentModule, SAMG_ENTITY_TYPES.MODULE, PREDICATES.IMPORTS, moduleName, SAMG_ENTITY_TYPES.MODULE, 0.9, source));
            }
        }
        // Pattern: "class X extends Y"
        const extendsPattern = /class\s+(\w+)\s+extends\s+(\w+)/gi;
        let match;
        while ((match = extendsPattern.exec(text)) !== null) {
            const [, child, parent] = match;
            triples.push(this.createTriple(child, SAMG_ENTITY_TYPES.CLASS, PREDICATES.EXTENDS, parent, SAMG_ENTITY_TYPES.CLASS, 0.95, source));
        }
        // Pattern: "class X implements Y"
        const implementsPattern = /class\s+(\w+)(?:\s+extends\s+\w+)?\s+implements\s+([\w,\s]+)/gi;
        while ((match = implementsPattern.exec(text)) !== null) {
            const [, className, interfaces] = match;
            const interfaceList = interfaces.split(',').map(i => i.trim());
            for (const iface of interfaceList) {
                if (iface) {
                    triples.push(this.createTriple(className, SAMG_ENTITY_TYPES.CLASS, PREDICATES.IMPLEMENTS, iface, SAMG_ENTITY_TYPES.CLASS, 0.95, source));
                }
            }
        }
        return triples;
    }
    extractDecisionRelations(text, source) {
        const triples = [];
        // Pattern: "decided to X" / "决定 X"
        const decisionPatterns = [
            /(?:we|I|team)?\s*(?:decided?|chose?|选择|决定)\s+(?:to\s+)?(.+?)(?:\.|$)/gi,
            /(?:decision|决定|选择)[:：]\s*(.+?)(?:\.|$)/gi,
        ];
        for (const pattern of decisionPatterns) {
            let match;
            while ((match = pattern.exec(text)) !== null) {
                const [, decision] = match;
                if (decision && decision.length > 5 && decision.length < 200) {
                    const decisionId = generateEntityId(SAMG_ENTITY_TYPES.DECISION, decision.slice(0, 50));
                    triples.push({
                        '@id': generateTripleId('session', PREDICATES.DECIDES, decisionId),
                        subject: createNode('session:current', 'codeflow:Session', 'Current Session'),
                        predicate: PREDICATES.DECIDES,
                        object: createNode(decisionId, SAMG_ENTITY_TYPES.DECISION, decision.slice(0, 100)),
                        confidence: 0.6,
                        timestamp: Date.now(),
                        source,
                    });
                }
            }
        }
        // Pattern: "created X" / "创建了 X"
        const createPatterns = [
            /(?:created?|added?|implemented?|创建|添加|实现)\s+(?:a\s+)?(?:new\s+)?(\w+(?:\s+\w+)?)/gi,
        ];
        for (const pattern of createPatterns) {
            let match;
            while ((match = pattern.exec(text)) !== null) {
                const [, entity] = match;
                if (entity && entity.length > 2) {
                    triples.push(this.createTriple('session:current', 'codeflow:Session', PREDICATES.CREATES, entity, SAMG_ENTITY_TYPES.CONCEPT, 0.5, source));
                }
            }
        }
        return triples;
    }
    extractFileRelations(text, source) {
        const triples = [];
        // Pattern: file paths
        const filePattern = /(?:^|\s)([\w./\\-]+\.(?:ts|js|tsx|jsx|py|java|go|rs|cpp|c|h|css|scss|html|json|yaml|yml|md))(?:\s|$|:)/gi;
        let match;
        const files = [];
        while ((match = filePattern.exec(text)) !== null) {
            const [, filePath] = match;
            if (!files.includes(filePath)) {
                files.push(filePath);
            }
        }
        for (let i = 0; i < files.length; i++) {
            for (let j = i + 1; j < files.length; j++) {
                triples.push(this.createTriple(files[i], SAMG_ENTITY_TYPES.FILE, PREDICATES.RELATED_TO, files[j], SAMG_ENTITY_TYPES.FILE, 0.4, source));
            }
        }
        // Pattern: "X modifies/changes Y"
        const modifyPatterns = [
            /(?:modif(?:y|ied)|chang(?:e|ed)|updat(?:e|ed)|修改|更新)\s+(?:the\s+)?(?:file\s+)?['"]?([^'"]+\.(?:ts|js|tsx|jsx|py))['"]?/gi,
        ];
        for (const pattern of modifyPatterns) {
            while ((match = pattern.exec(text)) !== null) {
                const [, filePath] = match;
                triples.push(this.createTriple('session:current', 'codeflow:Session', PREDICATES.MODIFIES, filePath, SAMG_ENTITY_TYPES.FILE, 0.7, source));
            }
        }
        return triples;
    }
    async extractByLLM(text, source) {
        if (!this.adapter)
            return [];
        const prompt = `Extract semantic relationships from the following text as JSON array.
Each relationship should have: subject, subjectType, predicate, object, objectType, confidence (0-1).

Predicates: calls, imports, extends, implements, defines, uses, dependsOn, mentions, references, decides, creates, modifies, deletes, relatedTo
Entity types: File, Class, Function, Variable, Module, Package, Decision, Requirement, Issue, Feature, Bug, Concept, Technology, Pattern

Text:
${text.slice(0, 2000)}

Output JSON array only:`;
        try {
            const response = await this.adapter.send(prompt);
            const content = response.content;
            const jsonMatch = content.match(/\[[\s\S]*\]/);
            if (!jsonMatch)
                return [];
            const relations = JSON.parse(jsonMatch[0]);
            const llmSource = { ...source, extractionMethod: 'llm' };
            return relations
                .filter(r => r.subject && r.predicate && r.object)
                .map(r => this.createTriple(r.subject, r.subjectType || SAMG_ENTITY_TYPES.CONCEPT, this.normalizePredicateFromLLM(r.predicate), r.object, r.objectType || SAMG_ENTITY_TYPES.CONCEPT, r.confidence || 0.6, llmSource));
        }
        catch {
            return [];
        }
    }
    normalizePredicateFromLLM(predicate) {
        const normalized = predicate.toLowerCase().replace(/[^a-z]/g, '');
        const mapping = {
            calls: PREDICATES.CALLS,
            imports: PREDICATES.IMPORTS,
            extends: PREDICATES.EXTENDS,
            implements: PREDICATES.IMPLEMENTS,
            defines: PREDICATES.DEFINES,
            uses: PREDICATES.USES,
            dependson: PREDICATES.DEPENDS_ON,
            mentions: PREDICATES.MENTIONS,
            references: PREDICATES.REFERENCES,
            decides: PREDICATES.DECIDES,
            creates: PREDICATES.CREATES,
            modifies: PREDICATES.MODIFIES,
            deletes: PREDICATES.DELETES,
            relatedto: PREDICATES.RELATED_TO,
        };
        return mapping[normalized] || `codeflow:${predicate}`;
    }
    createTriple(subjectLabel, subjectType, predicate, objectLabel, objectType, confidence, source) {
        const subjectId = generateEntityId(subjectType, subjectLabel);
        const objectId = generateEntityId(objectType, objectLabel);
        return {
            '@id': generateTripleId(subjectId, predicate, objectId),
            subject: createNode(subjectId, subjectType, subjectLabel),
            predicate,
            object: createNode(objectId, objectType, objectLabel),
            confidence,
            timestamp: Date.now(),
            source,
        };
    }
    deduplicateTriples(triples) {
        const seen = new Map();
        for (const triple of triples) {
            const key = `${triple.subject['@id']}|${triple.predicate}|${'@id' in triple.object ? triple.object['@id'] : triple.object['@value']}`;
            const existing = seen.get(key);
            if (!existing || triple.confidence > existing.confidence) {
                seen.set(key, triple);
            }
        }
        return Array.from(seen.values());
    }
}
//# sourceMappingURL=TripleExtractor.js.map