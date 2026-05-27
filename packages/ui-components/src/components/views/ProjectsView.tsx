/**
 * ProjectsView - 项目管理视图
 * 项目卡片网格布局、状态徽章、进度条、搜索和筛选
 */

import React, { useState, useEffect, useMemo, useCallback } from 'react';
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
import { CustomSelect, SelectOption } from '../shared/CustomSelect';
import { Input } from '../shared/Input';
import { ProgressBar } from '../shared/ProgressBar';
import { AvatarStack } from '../shared/Avatar';
import { Button } from '../shared/Button';

export type ProjectStatus = 'active' | 'completed' | 'paused' | 'planning';

export interface Project {
  id: string;
  name: string;
  description: string;
  status: ProjectStatus;
  progress: number;
  members: Array<{ name: string; src?: string }>;
  updatedAt: Date;
  tags?: string[];
}

export interface ProjectsViewProps {
  projects?: Project[];
  onSelectProject?: (projectId: string) => void;
  onCreateProject?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

// Icons
const SearchIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="11" cy="11" r="8" />
    <line x1="21" y1="21" x2="16.65" y2="16.65" />
  </svg>
);

const PlusIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <line x1="12" y1="5" x2="12" y2="19" />
    <line x1="5" y1="12" x2="19" y2="12" />
  </svg>
);

const FolderIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
  </svg>
);

// Demo data
const demoProjects: Project[] = [
  {
    id: '1',
    name: 'CodeFlow Core',
    description: 'Main application core with AI integration and multi-model support',
    status: 'active',
    progress: 75,
    members: [
      { name: 'Alice Chen' },
      { name: 'Bob Smith' },
      { name: 'Carol Wang' },
      { name: 'David Lee' },
    ],
    updatedAt: new Date(Date.now() - 1000 * 60 * 30),
    tags: ['typescript', 'react', 'ai'],
  },
  {
    id: '2',
    name: 'GUI Components',
    description: 'Reusable UI component library with design system tokens',
    status: 'active',
    progress: 60,
    members: [{ name: 'Eve Johnson' }, { name: 'Frank Brown' }],
    updatedAt: new Date(Date.now() - 1000 * 60 * 60 * 2),
    tags: ['react', 'css', 'design-system'],
  },
  {
    id: '3',
    name: 'API Gateway',
    description: 'Backend API gateway for model routing and cost optimization',
    status: 'completed',
    progress: 100,
    members: [{ name: 'Grace Kim' }],
    updatedAt: new Date(Date.now() - 1000 * 60 * 60 * 24),
    tags: ['node', 'api', 'gateway'],
  },
  {
    id: '4',
    name: 'Documentation',
    description: 'Project documentation and API reference guides',
    status: 'paused',
    progress: 40,
    members: [{ name: 'Henry Zhang' }, { name: 'Ivy Liu' }],
    updatedAt: new Date(Date.now() - 1000 * 60 * 60 * 48),
    tags: ['docs', 'markdown'],
  },
  {
    id: '5',
    name: 'Mobile App',
    description: 'React Native mobile application for iOS and Android',
    status: 'planning',
    progress: 10,
    members: [{ name: 'Jack Wilson' }],
    updatedAt: new Date(Date.now() - 1000 * 60 * 60 * 72),
    tags: ['react-native', 'mobile'],
  },
  {
    id: '6',
    name: 'Analytics Dashboard',
    description: 'Usage analytics and cost tracking dashboard',
    status: 'planning',
    progress: 5,
    members: [{ name: 'Kate Miller' }, { name: 'Leo Garcia' }],
    updatedAt: new Date(Date.now() - 1000 * 60 * 60 * 96),
    tags: ['analytics', 'dashboard'],
  },
];

// Status config
const statusConfig: Record<ProjectStatus, { label: string; color: string; bgColor: string }> = {
  active: { label: 'Active', color: colors.success.dark, bgColor: colors.success.light },
  completed: { label: 'Completed', color: colors.primary[700], bgColor: colors.primary[100] },
  paused: { label: 'Paused', color: colors.warning.dark, bgColor: colors.warning.light },
  planning: { label: 'Planning', color: colors.slate[600], bgColor: colors.slate[100] },
};

// Filter options
const statusOptions: SelectOption[] = [
  { value: 'all', label: 'All Status' },
  { value: 'active', label: 'Active' },
  { value: 'completed', label: 'Completed' },
  { value: 'paused', label: 'Paused' },
  { value: 'planning', label: 'Planning' },
];

const formatDate = (date: Date): string => {
  const now = new Date();
  const diff = now.getTime() - date.getTime();
  const hours = Math.floor(diff / (1000 * 60 * 60));
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));

  if (hours < 1) return 'Just now';
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;
  return date.toLocaleDateString();
};

// Project Card Component
const ProjectCard: React.FC<{
  project: Project;
  onClick: () => void;
}> = ({ project, onClick }) => {
  const status = statusConfig[project.status];

  return (
    <Card
      hoverable
      onClick={onClick}
      style={{
        cursor: 'pointer',
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
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
              backgroundColor: colors.primary[50],
              borderRadius: borderRadius.lg,
              color: colors.primary[600],
            }}
          >
            <FolderIcon />
          </div>
          <Badge
            style={{
              backgroundColor: status.bgColor,
              color: status.color,
            }}
          >
            {status.label}
          </Badge>
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
          {project.name}
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
          {project.description}
        </p>

        {/* Progress */}
        <div style={{ marginBottom: spacing[4] }}>
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              marginBottom: spacing[1],
            }}
          >
            <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Progress</span>
            <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.medium, color: colors.slate[700] }}>
              {project.progress}%
            </span>
          </div>
          <ProgressBar
            value={project.progress}
            size="sm"
            status={project.status === 'completed' ? 'success' : 'default'}
          />
        </div>

        {/* Footer */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <AvatarStack avatars={project.members} size="sm" max={3} />
          <span style={{ fontSize: fontSize.xs, color: colors.slate[400] }}>
            {formatDate(project.updatedAt)}
          </span>
        </div>
      </CardContent>
    </Card>
  );
};

// Empty State
const EmptyState: React.FC<{ onCreateProject?: () => void }> = ({ onCreateProject }) => (
  <div
    style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      padding: spacing[12],
      textAlign: 'center',
    }}
  >
    <div
      style={{
        width: 64,
        height: 64,
        backgroundColor: colors.slate[100],
        borderRadius: borderRadius.full,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        marginBottom: spacing[4],
        color: colors.slate[400],
      }}
    >
      <FolderIcon />
    </div>
    <h3
      style={{
        fontSize: fontSize.lg,
        fontWeight: fontWeight.semibold,
        color: colors.slate[700],
        marginBottom: spacing[2],
      }}
    >
      No projects found
    </h3>
    <p
      style={{
        fontSize: fontSize.sm,
        color: colors.slate[500],
        marginBottom: spacing[4],
      }}
    >
      Try adjusting your search or filter criteria
    </p>
    {onCreateProject && (
      <Button variant="primary" onClick={onCreateProject}>
        <PlusIcon />
        <span style={{ marginLeft: spacing[2] }}>Create Project</span>
      </Button>
    )}
  </div>
);

export const ProjectsView: React.FC<ProjectsViewProps> = ({
  projects = demoProjects,
  onSelectProject,
  onCreateProject,
  className,
  style,
}) => {
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
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

  // Debounced search
  const [debouncedSearch, setDebouncedSearch] = useState(searchQuery);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(searchQuery), 300);
    return () => clearTimeout(timer);
  }, [searchQuery]);

  // Filtered projects
  const filteredProjects = useMemo(() => {
    return projects.filter((project) => {
      const matchesSearch =
        debouncedSearch === '' ||
        project.name.toLowerCase().includes(debouncedSearch.toLowerCase()) ||
        project.description.toLowerCase().includes(debouncedSearch.toLowerCase()) ||
        project.tags?.some((tag) => tag.toLowerCase().includes(debouncedSearch.toLowerCase()));

      const matchesStatus = statusFilter === 'all' || project.status === statusFilter;

      return matchesSearch && matchesStatus;
    });
  }, [projects, debouncedSearch, statusFilter]);

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
            marginBottom: spacing[4],
          }}
        >
          <h1
            style={{
              fontSize: fontSize.xl,
              fontWeight: fontWeight.bold,
              color: colors.slate[800],
            }}
          >
            Projects
          </h1>
          <Button
            variant="primary"
            onClick={onCreateProject}
            style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}
          >
            <PlusIcon />
            <span>New Project</span>
          </Button>
        </div>

        {/* Search and Filter */}
        <div
          style={{
            display: 'flex',
            gap: spacing[3],
            flexWrap: 'wrap',
          }}
        >
          <div style={{ flex: 1, minWidth: 200, position: 'relative' }}>
            <div
              style={{
                position: 'absolute',
                left: spacing[3],
                top: '50%',
                transform: 'translateY(-50%)',
                color: colors.slate[400],
                pointerEvents: 'none',
              }}
            >
              <SearchIcon />
            </div>
            <Input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search projects..."
              style={{ paddingLeft: spacing[10] }}
            />
          </div>
          <CustomSelect
            options={statusOptions}
            value={statusFilter}
            onChange={setStatusFilter}
            style={{ minWidth: 150 }}
          />
        </div>
      </div>

      {/* Project Grid */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: spacing[6],
        }}
      >
        {filteredProjects.length === 0 ? (
          <EmptyState onCreateProject={onCreateProject} />
        ) : (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: `repeat(${getGridColumns()}, 1fr)`,
              gap: spacing[4],
            }}
          >
            {filteredProjects.map((project) => (
              <ProjectCard
                key={project.id}
                project={project}
                onClick={() => onSelectProject?.(project.id)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default ProjectsView;
