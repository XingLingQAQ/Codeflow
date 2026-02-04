/**
 * PlanModePanel - Plan 模式面板组件
 * 可视化展示 Plan 模式各阶段，支持编辑愿景和约束
 */

import React, { useState, useMemo } from 'react';
import {
  PlanModePanelProps,
  PlanPhase,
  PlanArtifact,
  VisionData,
  ConstraintItem,
  PHASE_CONFIG,
  ARTIFACT_CONFIG,
  CONSTRAINT_TYPE_CONFIG,
  formatDuration,
} from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
import { CustomSelect, SelectOption } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Tabs, TabPanel } from '../shared/Tabs';

/**
 * 阶段进度条组件
 */
const PhaseProgress: React.FC<{
  phases: PlanPhase[];
  currentPhase?: string;
  onPhaseSelect?: (phaseId: string) => void;
}> = ({ phases, currentPhase, onPhaseSelect }) => {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: spacing[1], padding: spacing[4] }}>
      {phases.map((phase, index) => {
        const config = PHASE_CONFIG[phase.id] || { icon: '📌', color: colors.slate[500], label: phase.name };
        const isActive = phase.id === currentPhase;
        const isCompleted = phase.status === 'completed';

        return (
          <React.Fragment key={phase.id}>
            {index > 0 && (
              <div
                style={{
                  flex: 1,
                  height: 2,
                  backgroundColor: isCompleted ? colors.primary[400] : colors.slate[200],
                  transition: transitions.normal,
                }}
              />
            )}
            <div
              onClick={() => onPhaseSelect?.(phase.id)}
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                gap: spacing[1],
                cursor: 'pointer',
                transition: transitions.fast,
              }}
            >
              <div
                style={{
                  width: isActive ? 48 : 40,
                  height: isActive ? 48 : 40,
                  borderRadius: borderRadius.full,
                  backgroundColor: isCompleted
                    ? colors.primary[500]
                    : isActive
                    ? config.color
                    : colors.slate[100],
                  border: isActive ? `3px solid ${config.color}30` : 'none',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: isActive ? 20 : 16,
                  boxShadow: isActive ? shadows.lg : shadows.sm,
                  transition: transitions.fast,
                }}
              >
                {isCompleted ? '✓' : config.icon}
              </div>
              <span
                style={{
                  fontSize: fontSize.xs,
                  fontWeight: isActive ? fontWeight.bold : fontWeight.medium,
                  color: isActive ? config.color : colors.slate[500],
                  textTransform: 'uppercase',
                }}
              >
                {config.label}
              </span>
            </div>
          </React.Fragment>
        );
      })}
    </div>
  );
};

/**
 * 愿景编辑器组件
 */
const VisionEditor: React.FC<{
  vision?: VisionData;
  onUpdate?: (vision: VisionData) => void;
}> = ({ vision, onUpdate }) => {
  const [editedVision, setEditedVision] = useState<VisionData>(
    vision || { goal: '', requirements: [], constraints: [], questions: [] }
  );

  const handleGoalChange = (goal: string) => {
    const updated = { ...editedVision, goal };
    setEditedVision(updated);
    onUpdate?.(updated);
  };

  const handleRequirementAdd = () => {
    const updated = {
      ...editedVision,
      requirements: [...editedVision.requirements, ''],
    };
    setEditedVision(updated);
  };

  const handleRequirementChange = (index: number, value: string) => {
    const requirements = [...editedVision.requirements];
    requirements[index] = value;
    const updated = { ...editedVision, requirements };
    setEditedVision(updated);
    onUpdate?.(updated);
  };

  const handleRequirementRemove = (index: number) => {
    const requirements = editedVision.requirements.filter((_, i) => i !== index);
    const updated = { ...editedVision, requirements };
    setEditedVision(updated);
    onUpdate?.(updated);
  };

  return (
    <div style={{ padding: spacing[4] }}>
      {/* Goal */}
      <div style={{ marginBottom: spacing[6] }}>
        <label
          style={{
            display: 'block',
            marginBottom: spacing[2],
            fontSize: fontSize.xs,
            fontWeight: fontWeight.bold,
            color: colors.slate[700],
            textTransform: 'uppercase',
          }}
        >
          Goal Statement
        </label>
        <textarea
          value={editedVision.goal}
          onChange={(e) => handleGoalChange(e.target.value)}
          placeholder="Describe the main goal of this plan..."
          style={{
            width: '100%',
            minHeight: 100,
            padding: spacing[3],
            fontSize: fontSize.sm,
            borderRadius: borderRadius.xl,
            border: `1px solid ${colors.slate[200]}`,
            backgroundColor: colors.slate[50],
            resize: 'vertical',
            outline: 'none',
            transition: transitions.fast,
          }}
        />
      </div>

      {/* Requirements */}
      <div style={{ marginBottom: spacing[6] }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[2] }}>
          <label
            style={{
              fontSize: fontSize.xs,
              fontWeight: fontWeight.bold,
              color: colors.slate[700],
              textTransform: 'uppercase',
            }}
          >
            Core Requirements
          </label>
          <Button size="sm" variant="ghost" onClick={handleRequirementAdd}>
            + Add
          </Button>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[2] }}>
          {editedVision.requirements.map((req, index) => (
            <div key={index} style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
              <span style={{ color: colors.primary[500], fontWeight: fontWeight.bold }}>•</span>
              <input
                type="text"
                value={req}
                onChange={(e) => handleRequirementChange(index, e.target.value)}
                placeholder="Enter requirement..."
                style={{
                  flex: 1,
                  padding: `${spacing[2]}px ${spacing[3]}px`,
                  fontSize: fontSize.sm,
                  borderRadius: borderRadius.lg,
                  border: `1px solid ${colors.slate[200]}`,
                  backgroundColor: '#fff',
                  outline: 'none',
                }}
              />
              <button
                onClick={() => handleRequirementRemove(index)}
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: borderRadius.full,
                  backgroundColor: colors.slate[100],
                  border: 'none',
                  cursor: 'pointer',
                  color: colors.slate[500],
                  fontSize: 12,
                }}
              >
                ×
              </button>
            </div>
          ))}
          {editedVision.requirements.length === 0 && (
            <div style={{ padding: spacing[4], textAlign: 'center', color: colors.slate[400], fontSize: fontSize.sm }}>
              No requirements added yet
            </div>
          )}
        </div>
      </div>

      {/* Questions */}
      {editedVision.questions.length > 0 && (
        <div>
          <label
            style={{
              display: 'block',
              marginBottom: spacing[2],
              fontSize: fontSize.xs,
              fontWeight: fontWeight.bold,
              color: colors.slate[700],
              textTransform: 'uppercase',
            }}
          >
            Clarifying Questions
          </label>
          <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
            {editedVision.questions.map((q) => (
              <Card key={q.id} padding="sm" variant="outlined">
                <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700], marginBottom: spacing[2] }}>
                  {q.question}
                  {q.required && <span style={{ color: colors.error.main, marginLeft: 4 }}>*</span>}
                </div>
                <input
                  type="text"
                  value={q.answer || ''}
                  placeholder="Your answer..."
                  style={{
                    width: '100%',
                    padding: `${spacing[2]}px ${spacing[3]}px`,
                    fontSize: fontSize.sm,
                    borderRadius: borderRadius.lg,
                    border: `1px solid ${colors.slate[200]}`,
                    backgroundColor: colors.slate[50],
                    outline: 'none',
                  }}
                />
              </Card>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};

/**
 * 约束列表组件
 */
const ConstraintList: React.FC<{
  constraints?: ConstraintItem[];
  onAdd?: (constraint: Omit<ConstraintItem, 'id'>) => void;
  onRemove?: (constraintId: string) => void;
}> = ({ constraints = [], onAdd, onRemove }) => {
  const [showAddForm, setShowAddForm] = useState(false);
  const [newConstraint, setNewConstraint] = useState<Omit<ConstraintItem, 'id'>>({
    type: 'functional',
    description: '',
    priority: 'should',
    source: 'manual',
  });

  const groupedConstraints = useMemo(() => {
    const groups: Record<string, ConstraintItem[]> = {
      functional: [],
      technical: [],
      business: [],
      security: [],
    };
    constraints.forEach((c) => {
      groups[c.type]?.push(c);
    });
    return groups;
  }, [constraints]);

  const handleAdd = () => {
    if (newConstraint.description.trim()) {
      onAdd?.(newConstraint);
      setNewConstraint({ type: 'functional', description: '', priority: 'should', source: 'manual' });
      setShowAddForm(false);
    }
  };

  const typeOptions: SelectOption[] = Object.entries(CONSTRAINT_TYPE_CONFIG).map(([key, config]) => ({
    value: key,
    label: config.label,
    icon: <span>{config.icon}</span>,
    color: config.color,
  }));

  const priorityOptions: SelectOption[] = [
    { value: 'must', label: 'Must Have', color: colors.error.main },
    { value: 'should', label: 'Should Have', color: colors.warning.main },
    { value: 'could', label: 'Could Have', color: colors.success.main },
  ];

  return (
    <div style={{ padding: spacing[4] }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[4] }}>
        <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700] }}>
          Constraints ({constraints.length})
        </span>
        <Button size="sm" variant="secondary" onClick={() => setShowAddForm(!showAddForm)}>
          {showAddForm ? 'Cancel' : '+ Add Constraint'}
        </Button>
      </div>

      {/* Add Form */}
      {showAddForm && (
        <Card padding="md" style={{ marginBottom: spacing[4], backgroundColor: colors.slate[50] }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
            <div style={{ display: 'flex', gap: spacing[3] }}>
              <CustomSelect
                options={typeOptions}
                value={newConstraint.type}
                onChange={(v) => setNewConstraint({ ...newConstraint, type: v as ConstraintItem['type'] })}
                label="Type"
                size="sm"
                style={{ flex: 1 }}
              />
              <CustomSelect
                options={priorityOptions}
                value={newConstraint.priority}
                onChange={(v) => setNewConstraint({ ...newConstraint, priority: v as ConstraintItem['priority'] })}
                label="Priority"
                size="sm"
                style={{ flex: 1 }}
              />
            </div>
            <textarea
              value={newConstraint.description}
              onChange={(e) => setNewConstraint({ ...newConstraint, description: e.target.value })}
              placeholder="Describe the constraint..."
              style={{
                width: '100%',
                minHeight: 80,
                padding: spacing[3],
                fontSize: fontSize.sm,
                borderRadius: borderRadius.lg,
                border: `1px solid ${colors.slate[200]}`,
                backgroundColor: '#fff',
                resize: 'vertical',
                outline: 'none',
              }}
            />
            <Button size="sm" onClick={handleAdd}>
              Add Constraint
            </Button>
          </div>
        </Card>
      )}

      {/* Grouped Constraints */}
      {Object.entries(groupedConstraints).map(([type, items]) => {
        if (items.length === 0) return null;
        const config = CONSTRAINT_TYPE_CONFIG[type];

        return (
          <div key={type} style={{ marginBottom: spacing[4] }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2], marginBottom: spacing[2] }}>
              <span>{config.icon}</span>
              <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: config.color, textTransform: 'uppercase' }}>
                {config.label}
              </span>
              <Badge size="sm" variant="default">{items.length}</Badge>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[2] }}>
              {items.map((constraint) => (
                <div
                  key={constraint.id}
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: spacing[3],
                    padding: spacing[3],
                    backgroundColor: '#fff',
                    borderRadius: borderRadius.lg,
                    border: `1px solid ${colors.slate[200]}`,
                  }}
                >
                  <div
                    style={{
                      width: 4,
                      height: '100%',
                      minHeight: 40,
                      borderRadius: 2,
                      backgroundColor:
                        constraint.priority === 'must'
                          ? colors.error.main
                          : constraint.priority === 'should'
                          ? colors.warning.main
                          : colors.success.main,
                    }}
                  />
                  <div style={{ flex: 1 }}>
                    <div style={{ fontSize: fontSize.sm, color: colors.slate[700], marginBottom: spacing[1] }}>
                      {constraint.description}
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
                      <Badge size="sm" variant={constraint.priority === 'must' ? 'error' : constraint.priority === 'should' ? 'warning' : 'success'}>
                        {constraint.priority}
                      </Badge>
                      <span style={{ fontSize: fontSize.xs, color: colors.slate[400] }}>
                        Source: {constraint.source}
                      </span>
                    </div>
                  </div>
                  {onRemove && (
                    <button
                      onClick={() => onRemove(constraint.id)}
                      style={{
                        width: 24,
                        height: 24,
                        borderRadius: borderRadius.full,
                        backgroundColor: colors.slate[100],
                        border: 'none',
                        cursor: 'pointer',
                        color: colors.slate[500],
                        fontSize: 12,
                      }}
                    >
                      ×
                    </button>
                  )}
                </div>
              ))}
            </div>
          </div>
        );
      })}

      {constraints.length === 0 && !showAddForm && (
        <div style={{ padding: spacing[6], textAlign: 'center', color: colors.slate[400], fontSize: fontSize.sm }}>
          No constraints defined yet
        </div>
      )}
    </div>
  );
};

/**
 * 工件查看器组件
 */
const ArtifactViewer: React.FC<{
  artifacts?: PlanArtifact[];
  onView?: (artifactId: string) => void;
}> = ({ artifacts = [], onView }) => {
  return (
    <div style={{ padding: spacing[4] }}>
      <div style={{ marginBottom: spacing[4] }}>
        <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700] }}>
          Generated Artifacts ({artifacts.length})
        </span>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: spacing[3] }}>
        {artifacts.map((artifact) => {
          const config = ARTIFACT_CONFIG[artifact.type] || { icon: '📄', color: colors.slate[500], label: artifact.type };

          return (
            <Card
              key={artifact.id}
              padding="md"
              hoverable
              onClick={() => onView?.(artifact.id)}
              style={{ cursor: 'pointer' }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3], marginBottom: spacing[2] }}>
                <div
                  style={{
                    width: 36,
                    height: 36,
                    borderRadius: borderRadius.lg,
                    backgroundColor: `${config.color}15`,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    fontSize: 18,
                  }}
                >
                  {config.icon}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      fontSize: fontSize.sm,
                      fontWeight: fontWeight.semibold,
                      color: colors.slate[800],
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {artifact.name}
                  </div>
                  <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
                    {config.label}
                  </div>
                </div>
              </div>
              <Badge
                size="sm"
                variant={
                  artifact.status === 'completed'
                    ? 'success'
                    : artifact.status === 'generating'
                    ? 'info'
                    : artifact.status === 'error'
                    ? 'error'
                    : 'default'
                }
                dot={artifact.status === 'generating'}
              >
                {artifact.status}
              </Badge>
            </Card>
          );
        })}
      </div>

      {artifacts.length === 0 && (
        <div style={{ padding: spacing[6], textAlign: 'center', color: colors.slate[400], fontSize: fontSize.sm }}>
          No artifacts generated yet
        </div>
      )}
    </div>
  );
};

/**
 * Plan 模式面板主组件
 */
export const PlanModePanel: React.FC<PlanModePanelProps> = ({
  planId,
  planName,
  phases,
  vision,
  constraints,
  artifacts,
  currentPhase,
  onPhaseSelect,
  onVisionUpdate,
  onConstraintAdd,
  onConstraintRemove,
  onArtifactView,
  onExecute,
  onFastForward,
  className,
  style,
}) => {
  const [activeTab, setActiveTab] = useState('vision');

  const tabs = [
    { id: 'vision', label: 'Vision', icon: <span>💡</span> },
    { id: 'constraints', label: 'Constraints', icon: <span>🔒</span>, badge: constraints?.length },
    { id: 'artifacts', label: 'Artifacts', icon: <span>📦</span>, badge: artifacts?.length },
  ];

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
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[4] }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
            <div
              style={{
                width: 40,
                height: 40,
                borderRadius: borderRadius.xl,
                background: `linear-gradient(135deg, ${colors.primary[500]}, ${colors.indigo[500]})`,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: '#fff',
                fontSize: 18,
                boxShadow: shadows.blue,
              }}
            >
              📋
            </div>
            <div>
              <h1 style={{ fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[900], margin: 0 }}>
                {planName}
              </h1>
              <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Plan ID: {planId}</span>
            </div>
          </div>
          <div style={{ display: 'flex', gap: spacing[2] }}>
            <Button variant="secondary" onClick={onFastForward}>
              ⚡ Fast Forward
            </Button>
            <Button onClick={onExecute}>
              ▶️ Execute Plan
            </Button>
          </div>
        </div>

        {/* Phase Progress */}
        <PhaseProgress phases={phases} currentPhase={currentPhase} onPhaseSelect={onPhaseSelect} />
      </div>

      {/* Tabs */}
      <div style={{ padding: `${spacing[3]}px ${spacing[6]}px`, backgroundColor: '#fff', borderBottom: `1px solid ${colors.slate[100]}` }}>
        <Tabs tabs={tabs} activeTab={activeTab} onChange={setActiveTab} variant="pills" />
      </div>

      {/* Content */}
      <div style={{ flex: 1, overflow: 'auto' }}>
        <TabPanel tabId="vision" activeTab={activeTab}>
          <VisionEditor vision={vision} onUpdate={onVisionUpdate} />
        </TabPanel>
        <TabPanel tabId="constraints" activeTab={activeTab}>
          <ConstraintList constraints={constraints} onAdd={onConstraintAdd} onRemove={onConstraintRemove} />
        </TabPanel>
        <TabPanel tabId="artifacts" activeTab={activeTab}>
          <ArtifactViewer artifacts={artifacts} onView={onArtifactView} />
        </TabPanel>
      </div>
    </div>
  );
};

export default PlanModePanel;
