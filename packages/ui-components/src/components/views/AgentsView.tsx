/**
 * AgentsView - Agent 预设视图
 * 预设卡片网格、头像堆叠效果、悬停操作按钮
 */

import React, { useState, useEffect } from 'react';
import {
  colors,
  spacing,
  borderRadius,
  fontSize,
  fontWeight,
  shadows,
  transitions,
  breakpoints,
} from '../shared/tokens';
import { Card, CardContent } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { AvatarStack } from '../shared/Avatar';
import { Button } from '../shared/Button';

export interface AgentPreset {
  id: string;
  name: string;
  description: string;
  category: string;
  models: string[];
  users: Array<{ name: string; src?: string }>;
  usageCount: number;
  isPopular?: boolean;
}

export interface AgentsViewProps {
  presets?: AgentPreset[];
  onUsePreset?: (presetId: string) => void;
  onCreatePreset?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

// Icons
const BotIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="3" y="11" width="18" height="10" rx="2" />
    <circle cx="12" cy="5" r="2" />
    <path d="M12 7v4" />
    <line x1="8" y1="16" x2="8" y2="16" />
    <line x1="16" y1="16" x2="16" y2="16" />
  </svg>
);

const PlusIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <line x1="12" y1="5" x2="12" y2="19" />
    <line x1="5" y1="12" x2="19" y2="12" />
  </svg>
);

const PlayIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polygon points="5 3 19 12 5 21 5 3" />
  </svg>
);

const StarIcon = () => (
  <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
  </svg>
);

// Demo data
const demoPresets: AgentPreset[] = [
  {
    id: '1',
    name: 'Code Reviewer',
    description: 'Expert code review with best practices, security analysis, and performance suggestions',
    category: 'Development',
    models: ['Claude Opus', 'GPT-4o'],
    users: [{ name: 'Alice' }, { name: 'Bob' }, { name: 'Carol' }, { name: 'David' }, { name: 'Eve' }],
    usageCount: 1250,
    isPopular: true,
  },
  {
    id: '2',
    name: 'Debug Assistant',
    description: 'Systematic debugging with root cause analysis and fix suggestions',
    category: 'Development',
    models: ['Claude Sonnet'],
    users: [{ name: 'Frank' }, { name: 'Grace' }],
    usageCount: 890,
    isPopular: true,
  },
  {
    id: '3',
    name: 'Documentation Writer',
    description: 'Generate comprehensive documentation, API references, and user guides',
    category: 'Documentation',
    models: ['Claude Opus'],
    users: [{ name: 'Henry' }, { name: 'Ivy' }, { name: 'Jack' }],
    usageCount: 650,
  },
  {
    id: '4',
    name: 'Test Generator',
    description: 'Create unit tests, integration tests, and test scenarios',
    category: 'Testing',
    models: ['Claude Sonnet', 'Gemini Pro'],
    users: [{ name: 'Kate' }],
    usageCount: 420,
  },
  {
    id: '5',
    name: 'Refactoring Expert',
    description: 'Code refactoring with design patterns and clean code principles',
    category: 'Development',
    models: ['Claude Opus'],
    users: [{ name: 'Leo' }, { name: 'Mia' }],
    usageCount: 380,
  },
  {
    id: '6',
    name: 'API Designer',
    description: 'Design RESTful APIs with OpenAPI specifications',
    category: 'Architecture',
    models: ['GPT-4o'],
    users: [{ name: 'Noah' }, { name: 'Olivia' }, { name: 'Peter' }, { name: 'Quinn' }],
    usageCount: 290,
  },
];

// Category colors
const categoryColors: Record<string, { bg: string; text: string }> = {
  Development: { bg: colors.primary[100], text: colors.primary[700] },
  Documentation: { bg: colors.indigo[100], text: colors.indigo[700] },
  Testing: { bg: colors.success.light, text: colors.success.dark },
  Architecture: { bg: colors.warning.light, text: colors.warning.dark },
};

// Agent Card Component
const AgentCard: React.FC<{
  preset: AgentPreset;
  onUse: () => void;
}> = ({ preset, onUse }) => {
  const [isHovered, setIsHovered] = useState(false);
  const categoryStyle = categoryColors[preset.category] || { bg: colors.slate[100], text: colors.slate[600] };

  return (
    <div
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{ height: '100%' }}
    >
      <Card
        hoverable
        style={{
          height: '100%',
          display: 'flex',
          flexDirection: 'column',
          position: 'relative',
          overflow: 'hidden',
        }}
      >
      <CardContent style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
        {/* Header */}
        <div
          style={{
            display: 'flex',
            alignItems: 'flex-start',
            justifyContent: 'space-between',
            marginBottom: spacing[3],
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 40,
              height: 40,
              backgroundColor: colors.indigo[50],
              borderRadius: borderRadius.lg,
              color: colors.indigo[600],
            }}
          >
            <BotIcon />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
            {preset.isPopular && (
              <span
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: spacing[1],
                  color: colors.warning.main,
                  fontSize: fontSize.xs,
                }}
              >
                <StarIcon />
              </span>
            )}
            <Badge style={{ backgroundColor: categoryStyle.bg, color: categoryStyle.text }}>
              {preset.category}
            </Badge>
          </div>
        </div>

        {/* Title & Description */}
        <h3
          style={{
            fontSize: fontSize.base,
            fontWeight: fontWeight.semibold,
            color: colors.slate[800],
            marginBottom: spacing[2],
          }}
        >
          {preset.name}
        </h3>
        <p
          style={{
            fontSize: fontSize.sm,
            color: colors.slate[500],
            marginBottom: spacing[4],
            flex: 1,
            overflow: 'hidden',
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
          }}
        >
          {preset.description}
        </p>

        {/* Models */}
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: spacing[1],
            marginBottom: spacing[4],
          }}
        >
          {preset.models.map((model, index) => (
            <span
              key={index}
              style={{
                padding: `${spacing[0.5]}px ${spacing[2]}px`,
                backgroundColor: colors.slate[100],
                borderRadius: borderRadius.full,
                fontSize: fontSize.xs,
                color: colors.slate[600],
              }}
            >
              {model}
            </span>
          ))}
        </div>

        {/* Footer */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <AvatarStack avatars={preset.users} size="sm" max={3} />
          <span style={{ fontSize: fontSize.xs, color: colors.slate[400] }}>
            {preset.usageCount.toLocaleString()} uses
          </span>
        </div>
      </CardContent>

      {/* Hover overlay with Use button */}
      <div
        style={{
          position: 'absolute',
          bottom: 0,
          left: 0,
          right: 0,
          padding: spacing[4],
          backgroundColor: 'rgba(255, 255, 255, 0.95)',
          backdropFilter: 'blur(4px)',
          borderTop: `1px solid ${colors.slate[100]}`,
          transform: isHovered ? 'translateY(0)' : 'translateY(100%)',
          transition: transitions.fast,
          display: 'flex',
          justifyContent: 'center',
        }}
      >
        <Button
          variant="primary"
          onClick={(e) => {
            e.stopPropagation();
            onUse();
          }}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: spacing[2],
            width: '100%',
            justifyContent: 'center',
          }}
        >
          <PlayIcon />
          <span>Use Preset</span>
        </Button>
      </div>
    </Card>
    </div>
  );
};

export const AgentsView: React.FC<AgentsViewProps> = ({
  presets = demoPresets,
  onUsePreset,
  onCreatePreset,
  className,
  style,
}) => {
  const [isMobile, setIsMobile] = useState(false);
  const [isTablet, setIsTablet] = useState(false);

  useEffect(() => {
    const checkBreakpoints = () => {
      const width = window.innerWidth;
      setIsMobile(width < breakpoints.md);
      setIsTablet(width >= breakpoints.md && width < breakpoints.lg);
    };
    checkBreakpoints();
    window.addEventListener('resize', checkBreakpoints);
    return () => window.removeEventListener('resize', checkBreakpoints);
  }, []);

  const getGridColumns = () => {
    if (isMobile) return 1;
    if (isTablet) return 2;
    return 3;
  };

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        backgroundColor: colors.slate[50],
        ...style,
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: `${spacing[4]}px ${spacing[6]}px`,
          backgroundColor: '#fff',
          borderBottom: `1px solid ${colors.slate[200]}`,
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <div>
            <h1
              style={{
                fontSize: fontSize.xl,
                fontWeight: fontWeight.bold,
                color: colors.slate[800],
                marginBottom: spacing[1],
              }}
            >
              Agent Presets
            </h1>
            <p style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>
              Pre-configured AI agents for common tasks
            </p>
          </div>
          <Button
            variant="primary"
            onClick={onCreatePreset}
            style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}
          >
            <PlusIcon />
            <span>Create Preset</span>
          </Button>
        </div>
      </div>

      {/* Preset Grid */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: spacing[6],
        }}
      >
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: `repeat(${getGridColumns()}, 1fr)`,
            gap: spacing[4],
          }}
        >
          {presets.map((preset) => (
            <AgentCard
              key={preset.id}
              preset={preset}
              onUse={() => onUsePreset?.(preset.id)}
            />
          ))}
        </div>
      </div>
    </div>
  );
};

export default AgentsView;
