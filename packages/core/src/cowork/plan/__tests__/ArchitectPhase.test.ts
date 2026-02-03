import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import {
  TechResearcher,
  ArchitectureDesigner,
  RoadmapGenerator,
  ArchitectPhase,
  TechResearchCallback,
  ArchitectureDesignCallback,
  RoadmapGenerateCallback,
} from '../ArchitectPhase.js';
import {
  VisionDocument,
  ConstraintSet,
  ArchitectureArtifact,
  TechResearchResult,
} from '../types.js';

// Mock data
const createMockVision = (): VisionDocument => ({
  id: 'vision-1',
  title: 'Test Project',
  summary: 'A test project for architecture design',
  goals: [
    'Implement user authentication with OAuth',
    'Build REST API for data management',
    'Create frontend dashboard',
  ],
  scope: {
    included: ['Authentication', 'API', 'Dashboard'],
    excluded: ['Mobile app'],
  },
  constraints: [
    'Must use PostgreSQL database',
    'API response time < 200ms',
  ],
  priorities: ['Security', 'Performance', 'Usability'],
  risks: ['Third-party API dependency'],
  answers: [],
  createdAt: Date.now(),
  updatedAt: Date.now(),
});

const createMockConstraints = (): ConstraintSet => ({
  id: 'constraints-1',
  visionId: 'vision-1',
  constraints: [
    {
      id: 'c1',
      type: 'technical',
      priority: 'must',
      description: 'Use PostgreSQL',
      verifiable: true,
    },
    {
      id: 'c2',
      type: 'performance',
      priority: 'should',
      description: 'Response time < 200ms',
      verifiable: true,
    },
  ],
  createdAt: Date.now(),
  updatedAt: Date.now(),
});

describe('TechResearcher', () => {
  let researcher: TechResearcher;

  beforeEach(() => {
    researcher = new TechResearcher();
  });

  describe('research', () => {
    it('should research a topic', async () => {
      const result = await researcher.research('PostgreSQL', 'Database selection');

      expect(result).toBeDefined();
      expect(result.topic).toBe('PostgreSQL');
      expect(result.findings.length).toBeGreaterThan(0);
      expect(result.recommendations.length).toBeGreaterThan(0);
    });

    it('should emit research:start and research:complete events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      researcher.on('research:start', startListener);
      researcher.on('research:complete', completeListener);

      await researcher.research('React', 'Frontend framework');

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should cache results when enabled', async () => {
      const cachedListener = vi.fn();
      researcher.on('research:cached', cachedListener);

      await researcher.research('Vue', 'Frontend');
      await researcher.research('Vue', 'Frontend');

      expect(cachedListener).toHaveBeenCalledTimes(1);
    });

    it('should use AI callback when provided', async () => {
      const mockCallback: TechResearchCallback = vi.fn().mockResolvedValue({
        id: 'ai-research-1',
        topic: 'AI Topic',
        query: 'AI Query',
        findings: [{ title: 'AI Finding', summary: 'AI Summary', pros: [], cons: [], relevance: 'high' }],
        recommendations: ['AI Recommendation'],
        sources: ['AI Source'],
        timestamp: Date.now(),
      });

      const aiResearcher = new TechResearcher({}, mockCallback);
      const result = await aiResearcher.research('AI Topic', 'AI Context');

      expect(mockCallback).toHaveBeenCalledWith('AI Topic', 'AI Context');
      expect(result.id).toBe('ai-research-1');
    });
  });

  describe('researchBatch', () => {
    it('should research multiple topics', async () => {
      const topics = [
        { topic: 'React', context: 'Frontend' },
        { topic: 'Node.js', context: 'Backend' },
      ];

      const results = await researcher.researchBatch(topics);

      expect(results.length).toBe(2);
      expect(results[0].topic).toBe('React');
      expect(results[1].topic).toBe('Node.js');
    });

    it('should respect maxTopics limit', async () => {
      const limitedResearcher = new TechResearcher({ maxTopics: 2 });
      const topics = [
        { topic: 'A', context: '1' },
        { topic: 'B', context: '2' },
        { topic: 'C', context: '3' },
      ];

      const results = await limitedResearcher.researchBatch(topics);

      expect(results.length).toBe(2);
    });
  });

  describe('extractTopicsFromVision', () => {
    it('should extract tech topics from vision', () => {
      const vision = createMockVision();
      const topics = researcher.extractTopicsFromVision(vision);

      expect(topics.length).toBeGreaterThan(0);
    });

    it('should identify tech keywords', () => {
      const vision: VisionDocument = {
        ...createMockVision(),
        goals: ['Build a REST API', 'Implement database caching'],
        constraints: ['Use cloud infrastructure'],
      };

      const topics = researcher.extractTopicsFromVision(vision);

      expect(topics.some(t => t.topic.toLowerCase().includes('api') || t.context.toLowerCase().includes('api'))).toBe(true);
    });
  });

  describe('cache management', () => {
    it('should clear cache', async () => {
      await researcher.research('Test', 'Context');
      expect(researcher.getCachedResults().length).toBe(1);

      researcher.clearCache();
      expect(researcher.getCachedResults().length).toBe(0);
    });
  });
});

describe('ArchitectureDesigner', () => {
  let designer: ArchitectureDesigner;

  beforeEach(() => {
    designer = new ArchitectureDesigner();
  });

  describe('design', () => {
    it('should design architecture', async () => {
      const vision = createMockVision();
      const constraints = createMockConstraints();
      const research: TechResearchResult[] = [
        {
          id: 'r1',
          topic: 'PostgreSQL',
          query: 'Database',
          findings: [{ title: 'PostgreSQL', summary: 'Relational DB', pros: ['ACID'], cons: ['Scaling'], relevance: 'high' }],
          recommendations: ['Use PostgreSQL'],
          sources: [],
          timestamp: Date.now(),
        },
      ];

      const architecture = await designer.design(vision, constraints, research);

      expect(architecture).toBeDefined();
      expect(architecture.type).toBe('architecture');
      expect(architecture.content.techStack.length).toBeGreaterThan(0);
      expect(architecture.content.systemComponents.length).toBeGreaterThan(0);
    });

    it('should emit design events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      designer.on('design:start', startListener);
      designer.on('design:complete', completeListener);

      await designer.design(createMockVision(), createMockConstraints(), []);

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should include security analysis when enabled', async () => {
      const secureDesigner = new ArchitectureDesigner({ includeSecurityAnalysis: true });
      const architecture = await secureDesigner.design(createMockVision(), createMockConstraints(), []);

      expect(architecture.content.securityArchitecture.authentication).not.toBe('TBD');
    });

    it('should include scalability plan when enabled', async () => {
      const scalableDesigner = new ArchitectureDesigner({ includeScalabilityPlan: true });
      const architecture = await scalableDesigner.design(createMockVision(), createMockConstraints(), []);

      expect(architecture.content.scalabilityPlan.horizontalScaling).toBe(true);
    });

    it('should use AI callback when provided', async () => {
      const mockContent: ArchitectureArtifact['content'] = {
        overview: 'AI Overview',
        techStack: [],
        systemComponents: [],
        dataArchitecture: { dataStores: [], dataFlows: [], caching: { enabled: false, layers: [], invalidation: '' } },
        integrations: [],
        securityArchitecture: { authentication: 'AI Auth', authorization: 'AI Authz', encryption: [], compliance: [] },
        scalabilityPlan: { horizontalScaling: false, verticalScaling: false, loadBalancing: '', bottlenecks: [], mitigations: [] },
      };

      const mockCallback: ArchitectureDesignCallback = vi.fn().mockResolvedValue(mockContent);
      const aiDesigner = new ArchitectureDesigner({}, mockCallback);

      const architecture = await aiDesigner.design(createMockVision(), createMockConstraints(), []);

      expect(mockCallback).toHaveBeenCalled();
      expect(architecture.content.overview).toBe('AI Overview');
    });
  });

  describe('generateMarkdown', () => {
    it('should generate markdown', async () => {
      const architecture = await designer.design(createMockVision(), createMockConstraints(), []);
      const markdown = designer.generateMarkdown(architecture);

      expect(markdown).toContain('# ');
      expect(markdown).toContain('## Overview');
      expect(markdown).toContain('## Tech Stack');
      expect(markdown).toContain('## System Components');
    });
  });
});

describe('RoadmapGenerator', () => {
  let generator: RoadmapGenerator;
  let mockArchitecture: ArchitectureArtifact;

  beforeEach(async () => {
    generator = new RoadmapGenerator();
    const designer = new ArchitectureDesigner();
    mockArchitecture = await designer.design(createMockVision(), createMockConstraints(), []);
  });

  describe('generate', () => {
    it('should generate roadmap', async () => {
      const roadmap = await generator.generate(createMockVision(), mockArchitecture);

      expect(roadmap).toBeDefined();
      expect(roadmap.type).toBe('roadmap');
      expect(roadmap.content.milestones.length).toBeGreaterThan(0);
      expect(roadmap.content.prdQueue.length).toBeGreaterThan(0);
    });

    it('should emit roadmap events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      generator.on('roadmap:start', startListener);
      generator.on('roadmap:complete', completeListener);

      await generator.generate(createMockVision(), mockArchitecture);

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should generate timeline when enabled', async () => {
      const timelineGenerator = new RoadmapGenerator({ estimateTimeline: true });
      const roadmap = await timelineGenerator.generate(createMockVision(), mockArchitecture);

      expect(roadmap.content.timeline.length).toBeGreaterThan(0);
    });

    it('should respect milestone count config', async () => {
      const limitedGenerator = new RoadmapGenerator({ defaultMilestoneCount: 2 });
      const roadmap = await limitedGenerator.generate(createMockVision(), mockArchitecture);

      expect(roadmap.content.milestones.length).toBeLessThanOrEqual(2);
    });

    it('should generate dependencies between milestones', async () => {
      const roadmap = await generator.generate(createMockVision(), mockArchitecture);

      if (roadmap.content.milestones.length > 1) {
        expect(roadmap.content.dependencies.length).toBeGreaterThan(0);
      }
    });

    it('should use AI callback when provided', async () => {
      const mockContent: RoadmapArtifact['content'] = {
        milestones: [{ id: 'ai-m1', name: 'AI Milestone', description: 'AI', deliverables: [], status: 'planned', priority: 'high' }],
        prdQueue: [],
        timeline: [],
        dependencies: [],
      };

      const mockCallback: RoadmapGenerateCallback = vi.fn().mockResolvedValue(mockContent);
      const aiGenerator = new RoadmapGenerator({}, mockCallback);

      const roadmap = await aiGenerator.generate(createMockVision(), mockArchitecture);

      expect(mockCallback).toHaveBeenCalled();
      expect(roadmap.content.milestones[0].name).toBe('AI Milestone');
    });
  });

  describe('generateJSON', () => {
    it('should generate valid JSON', async () => {
      const roadmap = await generator.generate(createMockVision(), mockArchitecture);
      const json = generator.generateJSON(roadmap);

      expect(() => JSON.parse(json)).not.toThrow();
    });
  });

  describe('generateMarkdown', () => {
    it('should generate markdown', async () => {
      const roadmap = await generator.generate(createMockVision(), mockArchitecture);
      const markdown = generator.generateMarkdown(roadmap);

      expect(markdown).toContain('# ');
      expect(markdown).toContain('## Milestones');
      expect(markdown).toContain('## PRD Queue');
    });
  });
});

describe('ArchitectPhase', () => {
  let phase: ArchitectPhase;
  let testDir: string;

  beforeEach(() => {
    testDir = path.join(os.tmpdir(), `architect-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });
    phase = new ArchitectPhase({ outputDir: testDir });
  });

  afterEach(() => {
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('execute', () => {
    it('should execute full architect phase', async () => {
      const vision = createMockVision();
      const constraints = createMockConstraints();

      const result = await phase.execute(vision, constraints);

      expect(result.research.length).toBeGreaterThanOrEqual(0);
      expect(result.architecture).toBeDefined();
      expect(result.roadmap).toBeDefined();
    });

    it('should emit phase events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      phase.on('phase:start', startListener);
      phase.on('phase:complete', completeListener);

      await phase.execute(createMockVision(), createMockConstraints());

      expect(startListener).toHaveBeenCalledWith({ phase: 'architect' });
      expect(completeListener).toHaveBeenCalledWith({ phase: 'architect' });
    });

    it('should save artifacts to filesystem', async () => {
      await phase.execute(createMockVision(), createMockConstraints());

      expect(fs.existsSync(path.join(testDir, 'architecture.md'))).toBe(true);
      expect(fs.existsSync(path.join(testDir, 'roadmap.json'))).toBe(true);
      expect(fs.existsSync(path.join(testDir, 'roadmap.md'))).toBe(true);
    });

    it('should forward component events', async () => {
      const archListener = vi.fn();
      const roadmapListener = vi.fn();
      phase.on('architecture:complete', archListener);
      phase.on('roadmap:complete', roadmapListener);

      await phase.execute(createMockVision(), createMockConstraints());

      expect(archListener).toHaveBeenCalled();
      expect(roadmapListener).toHaveBeenCalled();
    });
  });

  describe('getters', () => {
    it('should return research results', async () => {
      await phase.execute(createMockVision(), createMockConstraints());
      const results = phase.getResearchResults();

      expect(Array.isArray(results)).toBe(true);
    });

    it('should return architecture', async () => {
      await phase.execute(createMockVision(), createMockConstraints());
      const architecture = phase.getArchitecture();

      expect(architecture).toBeDefined();
      expect(architecture?.type).toBe('architecture');
    });

    it('should return roadmap', async () => {
      await phase.execute(createMockVision(), createMockConstraints());
      const roadmap = phase.getRoadmap();

      expect(roadmap).toBeDefined();
      expect(roadmap?.type).toBe('roadmap');
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      phase.updateConfig({ outputDir: '/new/path' });
      const config = phase.getConfig();

      expect(config.outputDir).toBe('/new/path');
    });

    it('should use custom callbacks', async () => {
      const mockResearch: TechResearchCallback = vi.fn().mockResolvedValue({
        id: 'custom-r1',
        topic: 'Custom',
        query: 'Query',
        findings: [],
        recommendations: [],
        sources: [],
        timestamp: Date.now(),
      });

      const customPhase = new ArchitectPhase(
        { outputDir: testDir },
        { research: mockResearch }
      );

      const vision: VisionDocument = {
        ...createMockVision(),
        goals: ['Build API server'],
      };

      await customPhase.execute(vision, createMockConstraints());

      expect(mockResearch).toHaveBeenCalled();
    });
  });
});

// Integration type definitions
import type { RoadmapArtifact } from '../types.js';
