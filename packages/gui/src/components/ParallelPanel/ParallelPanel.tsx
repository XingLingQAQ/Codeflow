/**
 * ParallelPanel - 并行模式面板组件
 * 实时显示各 Agent 执行状态，并排对比多个方案
 */

import React, { useState, useMemo } from 'react';
import {
  ParallelPanelProps,
  ParallelWorker,
  WorkerSolution,
  SolutionMetrics,
  MergeStrategy,
  WORKER_STATUS_CONFIG,
  PROVIDER_COLORS,
  METRIC_CONFIG,
  formatDuration,
  formatTimestamp,
} from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
import { CustomSelect, SelectOption } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Tabs, TabPanel } from '../shared/Tabs';
import { Modal } from '../shared/Modal';

/**
 * Worker 状态卡片
 */
const WorkerCard: React.FC<{
  worker: ParallelWorker;
  isSelected: boolean;
  onSelect: () => void;
  onCancel?: () => void;
}> = ({ worker, isSelected, onSelect, onCancel }) => {
  const statusConfig = WORKER_STATUS_CONFIG[worker.status];
  const providerColor = PROVIDER_COLORS[worker.modelProvider] || colors.slate[500];

  return (
    <Card
      padding="md"
      hoverable
      selected={isSelected}
      onClick={onSelect}
      style={{ minWidth: 240 }}
    >
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[3] }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
          <div
            style={{
              width: 32,
              height: 32,
              borderRadius: borderRadius.lg,
              backgroundColor: `${providerColor}15`,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 16,
            }}
          >
            🤖
          </div>
          <div>
            <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
              {worker.name}
            </div>
            <div style={{ fontSize: fontSize.xs, color: providerColor }}>
              {worker.model}
            </div>
          </div>
        </div>
        <Badge
          size="sm"
          variant={
            worker.status === 'completed'
              ? 'success'
              : worker.status === 'running'
              ? 'info'
              : worker.status === 'failed'
              ? 'error'
              : 'default'
          }
          dot={worker.status === 'running'}
        >
          {statusConfig.label}
        </Badge>
      </div>

      {/* Progress */}
      {worker.status === 'running' && (
        <div style={{ marginBottom: spacing[3] }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: spacing[1] }}>
            <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Progress</span>
            <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.primary[600] }}>
              {worker.progress}%
            </span>
          </div>
          <div
            style={{
              height: 4,
              borderRadius: borderRadius.full,
              backgroundColor: colors.slate[100],
              overflow: 'hidden',
            }}
          >
            <div
              style={{
                width: `${worker.progress}%`,
                height: '100%',
                backgroundColor: colors.primary[500],
                borderRadius: borderRadius.full,
                transition: transitions.normal,
              }}
            />
          </div>
        </div>
      )}

      {/* Worktree Info */}
      <div style={{ marginBottom: spacing[3] }}>
        <div style={{ fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }}>
          Branch: <span style={{ fontFamily: 'monospace', color: colors.slate[700] }}>{worker.branch}</span>
        </div>
      </div>

      {/* Metrics (if completed) */}
      {worker.solution && (
        <div style={{ display: 'flex', gap: spacing[2], flexWrap: 'wrap' }}>
          {Object.entries(worker.solution.metrics).slice(0, 3).map(([key, value]) => {
            const config = METRIC_CONFIG[key];
            return (
              <div
                key={key}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 4,
                  padding: `${spacing[0.5]}px ${spacing[1.5]}px`,
                  backgroundColor: colors.slate[50],
                  borderRadius: borderRadius.md,
                  fontSize: fontSize.xs,
                }}
              >
                <span>{config?.icon}</span>
                <span style={{ color: colors.slate[600] }}>{value.toFixed(0)}</span>
              </div>
            );
          })}
        </div>
      )}

      {/* Cancel Button */}
      {worker.status === 'running' && onCancel && (
        <Button
          size="sm"
          variant="ghost"
          onClick={(e) => {
            e.stopPropagation();
            onCancel();
          }}
          style={{ marginTop: spacing[3], width: '100%' }}
        >
          Cancel
        </Button>
      )}
    </Card>
  );
};

/**
 * 方案对比视图
 */
const SolutionCompare: React.FC<{
  solutions: WorkerSolution[];
  selectedId?: string;
  onSelect: (id: string) => void;
}> = ({ solutions, selectedId, onSelect }) => {
  if (solutions.length === 0) {
    return (
      <div style={{ padding: spacing[6], textAlign: 'center', color: colors.slate[400] }}>
        No solutions available yet
      </div>
    );
  }

  return (
    <div style={{ padding: spacing[4] }}>
      {/* Metrics Comparison Table */}
      <div style={{ marginBottom: spacing[6] }}>
        <h3 style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700], marginBottom: spacing[3] }}>
          Metrics Comparison
        </h3>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: `120px repeat(${solutions.length}, 1fr)`,
            gap: 1,
            backgroundColor: colors.slate[200],
            borderRadius: borderRadius.lg,
            overflow: 'hidden',
          }}
        >
          {/* Header */}
          <div style={{ padding: spacing[3], backgroundColor: colors.slate[100], fontWeight: fontWeight.semibold, fontSize: fontSize.xs }}>
            Metric
          </div>
          {solutions.map((sol) => (
            <div
              key={sol.id}
              style={{
                padding: spacing[3],
                backgroundColor: sol.id === selectedId ? colors.primary[50] : colors.slate[100],
                fontWeight: fontWeight.semibold,
                fontSize: fontSize.xs,
                textAlign: 'center',
                cursor: 'pointer',
              }}
              onClick={() => onSelect(sol.id)}
            >
              {sol.workerId}
            </div>
          ))}

          {/* Metrics Rows */}
          {Object.keys(METRIC_CONFIG).map((metricKey) => {
            const config = METRIC_CONFIG[metricKey];
            const values = solutions.map((s) => s.metrics[metricKey as keyof SolutionMetrics]);
            const maxValue = Math.max(...values);

            return (
              <React.Fragment key={metricKey}>
                <div
                  style={{
                    padding: spacing[3],
                    backgroundColor: '#fff',
                    fontSize: fontSize.xs,
                    display: 'flex',
                    alignItems: 'center',
                    gap: spacing[1],
                  }}
                >
                  <span>{config.icon}</span>
                  {config.label}
                </div>
                {solutions.map((sol) => {
                  const value = sol.metrics[metricKey as keyof SolutionMetrics];
                  const isMax = value === maxValue;

                  return (
                    <div
                      key={sol.id}
                      style={{
                        padding: spacing[3],
                        backgroundColor: sol.id === selectedId ? colors.primary[50] : '#fff',
                        textAlign: 'center',
                        fontSize: fontSize.sm,
                        fontWeight: isMax ? fontWeight.bold : fontWeight.normal,
                        color: isMax ? config.color : colors.slate[600],
                      }}
                    >
                      {value.toFixed(1)}
                      {isMax && <span style={{ marginLeft: 4 }}>🏆</span>}
                    </div>
                  );
                })}
              </React.Fragment>
            );
          })}
        </div>
      </div>

      {/* File Changes Comparison */}
      <div>
        <h3 style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700], marginBottom: spacing[3] }}>
          File Changes
        </h3>
        <div style={{ display: 'flex', gap: spacing[4], overflowX: 'auto' }}>
          {solutions.map((sol) => (
            <Card
              key={sol.id}
              padding="md"
              selected={sol.id === selectedId}
              onClick={() => onSelect(sol.id)}
              style={{ minWidth: 280, flex: 1 }}
            >
              <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800], marginBottom: spacing[3] }}>
                {sol.workerId}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[2] }}>
                {sol.files.slice(0, 5).map((file, i) => (
                  <div
                    key={i}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: spacing[2],
                      padding: spacing[2],
                      backgroundColor: colors.slate[50],
                      borderRadius: borderRadius.md,
                      fontSize: fontSize.xs,
                    }}
                  >
                    <span
                      style={{
                        color:
                          file.action === 'create'
                            ? colors.success.main
                            : file.action === 'delete'
                            ? colors.error.main
                            : colors.warning.main,
                      }}
                    >
                      {file.action === 'create' ? '+' : file.action === 'delete' ? '-' : '~'}
                    </span>
                    <span style={{ flex: 1, fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {file.path}
                    </span>
                    <span style={{ color: colors.success.main }}>+{file.additions}</span>
                    <span style={{ color: colors.error.main }}>-{file.deletions}</span>
                  </div>
                ))}
                {sol.files.length > 5 && (
                  <div style={{ fontSize: fontSize.xs, color: colors.slate[400], textAlign: 'center' }}>
                    +{sol.files.length - 5} more files
                  </div>
                )}
              </div>
            </Card>
          ))}
        </div>
      </div>
    </div>
  );
};

/**
 * 合并预览模态框
 */
const MergePreviewModal: React.FC<{
  isOpen: boolean;
  solution?: WorkerSolution;
  onClose: () => void;
  onMerge: (strategy: MergeStrategy) => void;
}> = ({ isOpen, solution, onClose, onMerge }) => {
  const [strategy, setStrategy] = useState<MergeStrategy>('merge');

  const strategyOptions: SelectOption[] = [
    { value: 'fast-forward', label: 'Fast Forward', description: 'Move HEAD to target branch' },
    { value: 'merge', label: 'Merge', description: 'Create a merge commit' },
    { value: 'rebase', label: 'Rebase', description: 'Rebase commits onto main' },
  ];

  if (!solution) return null;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Merge Solution"
      subtitle={`Merging ${solution.workerId}'s solution`}
      icon={<span>🔀</span>}
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => onMerge(strategy)}>
            Merge Solution
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[4] }}>
        {/* Summary */}
        <div>
          <h4 style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }}>
            Summary
          </h4>
          <p style={{ fontSize: fontSize.sm, color: colors.slate[600], lineHeight: 1.6 }}>
            {solution.summary}
          </p>
        </div>

        {/* Changes */}
        <div>
          <h4 style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }}>
            Changes ({solution.files.length} files)
          </h4>
          <div
            style={{
              maxHeight: 200,
              overflow: 'auto',
              backgroundColor: colors.slate[50],
              borderRadius: borderRadius.lg,
              padding: spacing[3],
            }}
          >
            {solution.files.map((file, i) => (
              <div
                key={i}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: spacing[2],
                  padding: `${spacing[1]}px 0`,
                  fontSize: fontSize.xs,
                  fontFamily: 'monospace',
                }}
              >
                <span
                  style={{
                    color:
                      file.action === 'create'
                        ? colors.success.main
                        : file.action === 'delete'
                        ? colors.error.main
                        : colors.warning.main,
                  }}
                >
                  {file.action === 'create' ? 'A' : file.action === 'delete' ? 'D' : 'M'}
                </span>
                <span style={{ flex: 1 }}>{file.path}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Strategy */}
        <CustomSelect
          options={strategyOptions}
          value={strategy}
          onChange={(v) => setStrategy(v as MergeStrategy)}
          label="Merge Strategy"
        />
      </div>
    </Modal>
  );
};

/**
 * 并行模式面板主组件
 */
export const ParallelPanel: React.FC<ParallelPanelProps> = ({
  task,
  onWorkerSelect,
  onSolutionSelect,
  onSolutionMerge,
  onTaskCancel,
  onWorkerCancel,
  className,
  style,
}) => {
  const [selectedWorkerId, setSelectedWorkerId] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState('workers');
  const [showMergeModal, setShowMergeModal] = useState(false);

  const solutions = useMemo(() => {
    return task?.workers.filter((w) => w.solution).map((w) => w.solution!) || [];
  }, [task]);

  const selectedSolution = useMemo(() => {
    return solutions.find((s) => s.id === task?.selectedSolutionId);
  }, [solutions, task?.selectedSolutionId]);

  const handleWorkerSelect = (workerId: string) => {
    setSelectedWorkerId(workerId);
    onWorkerSelect?.(workerId);
  };

  const handleSolutionSelect = (solutionId: string) => {
    onSolutionSelect?.(solutionId);
  };

  const handleMerge = (strategy: MergeStrategy) => {
    if (task?.selectedSolutionId) {
      onSolutionMerge?.(task.selectedSolutionId, strategy);
      setShowMergeModal(false);
    }
  };

  const tabs = [
    { id: 'workers', label: 'Workers', icon: <span>🤖</span>, badge: task?.workers.length },
    { id: 'compare', label: 'Compare', icon: <span>⚖️</span>, badge: solutions.length },
  ];

  if (!task) {
    return (
      <div
        className={className}
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100%',
          backgroundColor: colors.slate[50],
          ...style,
        }}
      >
        <div style={{ textAlign: 'center', color: colors.slate[400] }}>
          <div style={{ fontSize: 48, marginBottom: spacing[4] }}>🔄</div>
          <div style={{ fontSize: fontSize.lg, fontWeight: fontWeight.semibold, marginBottom: spacing[2] }}>
            No Active Parallel Task
          </div>
          <div style={{ fontSize: fontSize.sm }}>
            Start a parallel task to see workers here
          </div>
        </div>
      </div>
    );
  }

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
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[3] }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
            <div
              style={{
                width: 40,
                height: 40,
                borderRadius: borderRadius.xl,
                background: `linear-gradient(135deg, ${colors.indigo[500]}, ${colors.primary[500]})`,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: '#fff',
                fontSize: 18,
                boxShadow: shadows.indigo,
              }}
            >
              🔀
            </div>
            <div>
              <h1 style={{ fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[900], margin: 0 }}>
                {task.name}
              </h1>
              <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
                {task.workers.filter((w) => w.status === 'completed').length}/{task.workers.length} workers completed
              </span>
            </div>
          </div>
          <div style={{ display: 'flex', gap: spacing[2] }}>
            {task.selectedSolutionId && (
              <Button onClick={() => setShowMergeModal(true)}>
                🔀 Merge Selected
              </Button>
            )}
            {task.status === 'running' && (
              <Button variant="danger" onClick={onTaskCancel}>
                Cancel Task
              </Button>
            )}
          </div>
        </div>

        {/* Overall Progress */}
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: spacing[1] }}>
            <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Overall Progress</span>
            <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.primary[600] }}>
              {Math.round(task.workers.reduce((acc, w) => acc + w.progress, 0) / task.workers.length)}%
            </span>
          </div>
          <div
            style={{
              height: 6,
              borderRadius: borderRadius.full,
              backgroundColor: colors.slate[100],
              overflow: 'hidden',
              display: 'flex',
            }}
          >
            {task.workers.map((worker) => (
              <div
                key={worker.id}
                style={{
                  flex: 1,
                  height: '100%',
                  backgroundColor:
                    worker.status === 'completed'
                      ? colors.success.main
                      : worker.status === 'failed'
                      ? colors.error.main
                      : worker.status === 'running'
                      ? colors.primary[500]
                      : colors.slate[300],
                  transition: transitions.normal,
                }}
              />
            ))}
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div style={{ padding: `${spacing[3]}px ${spacing[6]}px`, backgroundColor: '#fff', borderBottom: `1px solid ${colors.slate[100]}` }}>
        <Tabs tabs={tabs} activeTab={activeTab} onChange={setActiveTab} variant="pills" />
      </div>

      {/* Content */}
      <div style={{ flex: 1, overflow: 'auto' }}>
        <TabPanel tabId="workers" activeTab={activeTab}>
          <div style={{ padding: spacing[4] }}>
            <div style={{ display: 'flex', gap: spacing[4], overflowX: 'auto', paddingBottom: spacing[2] }}>
              {task.workers.map((worker) => (
                <WorkerCard
                  key={worker.id}
                  worker={worker}
                  isSelected={selectedWorkerId === worker.id}
                  onSelect={() => handleWorkerSelect(worker.id)}
                  onCancel={worker.status === 'running' ? () => onWorkerCancel?.(worker.id) : undefined}
                />
              ))}
            </div>
          </div>
        </TabPanel>
        <TabPanel tabId="compare" activeTab={activeTab}>
          <SolutionCompare
            solutions={solutions}
            selectedId={task.selectedSolutionId}
            onSelect={handleSolutionSelect}
          />
        </TabPanel>
      </div>

      {/* Merge Modal */}
      <MergePreviewModal
        isOpen={showMergeModal}
        solution={selectedSolution}
        onClose={() => setShowMergeModal(false)}
        onMerge={handleMerge}
      />
    </div>
  );
};

export default ParallelPanel;
