import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  ProposalGenerator,
  SpecGenerator,
  DesignGenerator,
  TaskGenerator,
  ArtifactGeneratorFactory,
  AIGenerateCallback,
} from '../ArtifactGenerators.js';
import {
  VisionDocument,
  ConstraintSet,
  SpecArtifact,
  DesignArtifact,
} from '../types.js';

// Mock data
const mockVision: VisionDocument = {
  id: 'vision-1',
  title: 'Test Feature',
  summary: 'Implement a new authentication system',
  goals: ['Support OAuth2', 'Support JWT tokens', 'Integrate with existing users'],
  scope: {
    included: ['Login flow', 'Token management', 'User sessions'],
    excluded: ['Social login', 'Biometric auth'],
  },
  constraints: ['Use TypeScript', 'Response time < 100ms'],
  priorities: ['Must have: Core auth', 'Nice to have: Remember me'],
  risks: ['Security vulnerabilities', 'Performance issues'],
  answers: [],
  createdAt: Date.now(),
  updatedAt: Date.now(),
};

const mockConstraints: ConstraintSet = {
  id: 'constraints-1',
  visionId: 'vision-1',
  constraints: [
    { id: 'c1', type: 'functional', priority: 'must', description: 'Support OAuth2 authentication', verifiable: true },
    { id: 'c2', type: 'technical', priority: 'must', description: 'Use TypeScript', verifiable: false },
    { id: 'c3', type: 'performance', priority: 'should', description: 'Response time < 100ms', verifiable: true },
    { id: 'c4', type: 'security', priority: 'must', description: 'Encrypt all tokens', verifiable: true },
  ],
  createdAt: Date.now(),
  updatedAt: Date.now(),
};

describe('ProposalGenerator', () => {
  let generator: ProposalGenerator;

  beforeEach(() => {
    generator = new ProposalGenerator();
  });

  describe('generate (static)', () => {
    it('should generate proposal content', async () => {
      const content = await generator.generate(mockVision, mockConstraints);

      expect(content).toBeDefined();
      expect(content.why).toBe(mockVision.summary);
      expect(content.what).toContain('OAuth2');
      expect(content.risks).toEqual(mockVision.risks);
    });

    it('should include must constraints in how section', async () => {
      const content = await generator.generate(mockVision, mockConstraints);

      expect(content.how).toContain('Must-have');
    });

    it('should include technical constraints', async () => {
      const content = await generator.generate(mockVision, mockConstraints);

      expect(content.how).toContain('Technical');
    });
  });

  describe('generate (with AI)', () => {
    it('should use AI callback when provided', async () => {
      const mockAI: AIGenerateCallback = vi.fn().mockResolvedValue(JSON.stringify({
        why: 'AI generated why',
        what: 'AI generated what',
        how: 'AI generated how',
        impact: 'AI generated impact',
        risks: ['AI risk 1'],
        alternatives: ['AI alternative 1'],
      }));

      const aiGenerator = new ProposalGenerator({}, mockAI);
      const content = await aiGenerator.generate(mockVision, mockConstraints);

      expect(mockAI).toHaveBeenCalled();
      expect(content.why).toBe('AI generated why');
    });

    it('should fallback to static on AI parse error', async () => {
      const mockAI: AIGenerateCallback = vi.fn().mockResolvedValue('invalid json');

      const aiGenerator = new ProposalGenerator({}, mockAI);
      const content = await aiGenerator.generate(mockVision, mockConstraints);

      expect(content.why).toBe(mockVision.summary);
    });
  });

  describe('language support', () => {
    it('should support Chinese language config', () => {
      const zhGenerator = new ProposalGenerator({ language: 'zh' });
      expect(zhGenerator).toBeDefined();
    });
  });
});

describe('SpecGenerator', () => {
  let generator: SpecGenerator;

  beforeEach(() => {
    generator = new SpecGenerator();
  });

  describe('generate (static)', () => {
    it('should generate spec content', async () => {
      const content = await generator.generate('User can login with email and password');

      expect(content).toBeDefined();
      expect(content.requirement).toBe('User can login with email and password');
      expect(content.scenarios.length).toBeGreaterThan(0);
    });

    it('should create scenarios from requirement', async () => {
      const content = await generator.generate('User can login. User can logout. User can reset password.');

      expect(content.scenarios.length).toBe(3);
      expect(content.scenarios[0].given).toBeDefined();
      expect(content.scenarios[0].when).toBeDefined();
      expect(content.scenarios[0].then).toBeDefined();
    });

    it('should generate acceptance criteria from scenarios', async () => {
      const content = await generator.generate('User can login with email');

      expect(content.acceptanceCriteria.length).toBe(content.scenarios.length);
    });
  });

  describe('generateBatch', () => {
    it('should generate multiple specs', async () => {
      const requirements = ['Login feature', 'Logout feature', 'Password reset'];
      const specs = await generator.generateBatch(requirements);

      expect(specs.length).toBe(3);
      specs.forEach((spec, i) => {
        expect(spec.requirement).toBe(requirements[i]);
      });
    });
  });

  describe('generate (with AI)', () => {
    it('should use AI callback when provided', async () => {
      const mockAI: AIGenerateCallback = vi.fn().mockResolvedValue(JSON.stringify({
        requirement: 'AI refined requirement',
        scenarios: [{ id: 's1', name: 'AI Scenario', given: 'G', when: 'W', then: 'T' }],
        acceptanceCriteria: ['AI criterion'],
      }));

      const aiGenerator = new SpecGenerator({}, mockAI);
      const content = await aiGenerator.generate('Test requirement');

      expect(mockAI).toHaveBeenCalled();
      expect(content.requirement).toBe('AI refined requirement');
    });
  });
});

describe('DesignGenerator', () => {
  let generator: DesignGenerator;
  let mockSpecs: SpecArtifact['content'][];

  beforeEach(() => {
    generator = new DesignGenerator();
    mockSpecs = [
      {
        requirement: 'User authentication',
        scenarios: [{ id: 's1', name: 'Login', given: 'User exists', when: 'Login', then: 'Authenticated' }],
        acceptanceCriteria: ['User can login'],
      },
      {
        requirement: 'Token management',
        scenarios: [{ id: 's2', name: 'Token', given: 'User logged in', when: 'Request', then: 'Token issued' }],
        acceptanceCriteria: ['Token is valid'],
      },
    ];
  });

  describe('generate (static)', () => {
    it('should generate design content', async () => {
      const content = await generator.generate(mockSpecs, mockConstraints);

      expect(content).toBeDefined();
      expect(content.overview).toContain('requirements');
      expect(content.components.length).toBeGreaterThan(0);
    });

    it('should create components from specs', async () => {
      const content = await generator.generate(mockSpecs, mockConstraints);

      expect(content.components.length).toBe(mockSpecs.length);
    });

    it('should create interfaces from specs', async () => {
      const content = await generator.generate(mockSpecs, mockConstraints);

      expect(content.interfaces.length).toBe(mockSpecs.length);
    });

    it('should include technical constraints as dependencies', async () => {
      const content = await generator.generate(mockSpecs, mockConstraints);

      expect(content.dependencies.length).toBeGreaterThan(0);
    });

    it('should generate data flow', async () => {
      const content = await generator.generate(mockSpecs, mockConstraints);

      expect(content.dataFlow).toBeDefined();
    });
  });

  describe('generate (with AI)', () => {
    it('should use AI callback when provided', async () => {
      const mockAI: AIGenerateCallback = vi.fn().mockResolvedValue(JSON.stringify({
        overview: 'AI overview',
        components: [{ name: 'AIComponent', responsibility: 'AI task', dependencies: [] }],
        interfaces: [{ name: 'IAI', methods: ['ai()'], description: 'AI interface' }],
        dataFlow: 'AI flow',
        dependencies: ['AI dep'],
      }));

      const aiGenerator = new DesignGenerator({}, mockAI);
      const content = await aiGenerator.generate(mockSpecs, mockConstraints);

      expect(mockAI).toHaveBeenCalled();
      expect(content.overview).toBe('AI overview');
    });
  });
});

describe('TaskGenerator', () => {
  let generator: TaskGenerator;
  let mockDesign: DesignArtifact['content'];
  let mockSpecs: SpecArtifact['content'][];

  beforeEach(() => {
    generator = new TaskGenerator();
    mockDesign = {
      overview: 'Authentication system design',
      components: [
        { name: 'AuthService', responsibility: 'Handle authentication', dependencies: [] },
        { name: 'TokenService', responsibility: 'Manage tokens', dependencies: ['AuthService'] },
      ],
      interfaces: [
        { name: 'IAuthService', methods: ['login()', 'logout()'], description: 'Auth interface' },
      ],
      dataFlow: 'User -> AuthService -> TokenService',
      dependencies: ['TypeScript', 'JWT library'],
    };
    mockSpecs = [
      {
        requirement: 'User authentication',
        scenarios: [{ id: 's1', name: 'Login', given: 'User exists', when: 'Login', then: 'Authenticated' }],
        acceptanceCriteria: ['User can login'],
      },
    ];
  });

  describe('generate (static)', () => {
    it('should generate tasks content', async () => {
      const content = await generator.generate(mockDesign, mockSpecs);

      expect(content).toBeDefined();
      expect(content.tasks.length).toBeGreaterThan(0);
    });

    it('should create implementation tasks for components', async () => {
      const content = await generator.generate(mockDesign, mockSpecs);

      const implTasks = content.tasks.filter(t => t.title.includes('Implement'));
      expect(implTasks.length).toBe(mockDesign.components.length);
    });

    it('should create test tasks for specs', async () => {
      const content = await generator.generate(mockDesign, mockSpecs);

      const testTasks = content.tasks.filter(t => t.title.includes('Test'));
      expect(testTasks.length).toBeGreaterThanOrEqual(mockSpecs.length);
    });

    it('should create integration task', async () => {
      const content = await generator.generate(mockDesign, mockSpecs);

      const integrationTask = content.tasks.find(t => t.title.includes('Integration'));
      expect(integrationTask).toBeDefined();
    });

    it('should set up task dependencies', async () => {
      const content = await generator.generate(mockDesign, mockSpecs);

      expect(content.dependencies.length).toBeGreaterThan(0);
    });

    it('should estimate duration', async () => {
      const content = await generator.generate(mockDesign, mockSpecs);

      expect(content.estimatedDuration).toBeGreaterThan(0);
    });
  });

  describe('generate (with AI)', () => {
    it('should use AI callback when provided', async () => {
      const mockAI: AIGenerateCallback = vi.fn().mockResolvedValue(JSON.stringify({
        tasks: [{ id: 't1', title: 'AI Task', description: 'AI desc', priority: 'high', status: 'pending' }],
        dependencies: [],
        estimatedDuration: 5,
      }));

      const aiGenerator = new TaskGenerator({}, mockAI);
      const content = await aiGenerator.generate(mockDesign, mockSpecs);

      expect(mockAI).toHaveBeenCalled();
      expect(content.tasks[0].title).toBe('AI Task');
    });
  });
});

describe('ArtifactGeneratorFactory', () => {
  let factory: ArtifactGeneratorFactory;

  beforeEach(() => {
    factory = new ArtifactGeneratorFactory();
  });

  describe('createGenerators', () => {
    it('should create proposal generator', () => {
      const gen = factory.createProposalGenerator();
      expect(gen).toBeInstanceOf(ProposalGenerator);
    });

    it('should create spec generator', () => {
      const gen = factory.createSpecGenerator();
      expect(gen).toBeInstanceOf(SpecGenerator);
    });

    it('should create design generator', () => {
      const gen = factory.createDesignGenerator();
      expect(gen).toBeInstanceOf(DesignGenerator);
    });

    it('should create task generator', () => {
      const gen = factory.createTaskGenerator();
      expect(gen).toBeInstanceOf(TaskGenerator);
    });
  });

  describe('generateAll', () => {
    it('should generate all artifacts at once', async () => {
      const result = await factory.generateAll(mockVision, mockConstraints);

      expect(result.proposal).toBeDefined();
      expect(result.specs.length).toBeGreaterThan(0);
      expect(result.design).toBeDefined();
      expect(result.tasks).toBeDefined();
    });

    it('should use custom requirements when provided', async () => {
      const customReqs = ['Custom requirement 1', 'Custom requirement 2'];
      const result = await factory.generateAll(mockVision, mockConstraints, customReqs);

      expect(result.specs.length).toBe(customReqs.length);
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      factory.updateConfig({ language: 'zh' });
      // Config is internal, but generators created after should use new config
      const gen = factory.createProposalGenerator();
      expect(gen).toBeDefined();
    });

    it('should set AI callback', () => {
      const mockAI: AIGenerateCallback = vi.fn().mockResolvedValue('{}');
      factory.setAICallback(mockAI);
      // Callback is internal, but generators created after should use it
      const gen = factory.createProposalGenerator();
      expect(gen).toBeDefined();
    });
  });
});
