/**
 * Plan模式任务看板组件
 * 任务列表+详情面板支持拖拽排序与批量模型切换
 */

import React, { useState, useMemo, useCallback } from 'react';
import {
  PlanBoardProps,
  TaskCardProps,
  TaskDetailPanelProps,
  TaskListProps,
  BatchActionsProps,
  GanttChartProps,
  PlanTask,
  ModelType,
  TaskStatus,
  ViewMode,
  TASK_STATUS_CONFIG,
  TASK_PRIORITY_CONFIG,
  MODEL_CONFIG,
  formatTaskTime,
  calculateTaskProgress,
} from './types';
import { CustomSelect, SelectOption } from '../shared/CustomSelect';

/**
 * 任务卡片
 */
export const TaskCard: React.FC<TaskCardProps> = ({
  task,
  isSelected,
  isDragging,
  onSelect,
  onDragStart,
  onDragEnd,
}) => {
  const statusConfig = TASK_STATUS_CONFIG[task.status];
  const priorityConfig = TASK_PRIORITY_CONFIG[task.priority];
  const modelConfig = MODEL_CONFIG[task.model];

  return (
    <div
      draggable
      onDragStart={() => onDragStart?.(task.id)}
      onDragEnd={onDragEnd}
      onClick={() => onSelect?.(task.id)}
      style={{
        padding: '12px 14px',
        backgroundColor: isSelected ? '#e3f2fd' : '#fff',
        border: `1px solid ${isSelected ? '#2196F3' : '#e0e0e0'}`,
        borderLeft: `4px solid ${priorityConfig.color}`,
        borderRadius: '0 8px 8px 0',
        marginBottom: 8,
        cursor: 'grab',
        opacity: isDragging ? 0.5 : 1,
        transition: 'all 0.15s',
      }}
    >
      {/* 头部 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 8,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {/* 选择框 */}
          <input
            type="checkbox"
            checked={task.isSelected}
            onChange={(e) => {
              e.stopPropagation();
              onSelect?.(task.id);
            }}
            style={{ cursor: 'pointer' }}
          />
          {/* 优先级 */}
          <span
            style={{
              fontSize: 10,
              padding: '2px 6px',
              borderRadius: 4,
              backgroundColor: `${priorityConfig.color}20`,
              color: priorityConfig.color,
              fontWeight: 600,
            }}
          >
            {task.priority}
          </span>
          {/* 状态 */}
          <span
            style={{
              fontSize: 10,
              padding: '2px 6px',
              borderRadius: 4,
              backgroundColor: `${statusConfig.color}20`,
              color: statusConfig.color,
            }}
          >
            {statusConfig.icon} {statusConfig.label}
          </span>
        </div>
        {/* 模型 */}
        <span
          style={{
            fontSize: 10,
            padding: '2px 6px',
            borderRadius: 4,
            backgroundColor: `${modelConfig.color}15`,
            color: modelConfig.color,
          }}
        >
          {modelConfig.icon} {modelConfig.label}
        </span>
      </div>

      {/* 标题 */}
      <div
        style={{
          fontSize: 13,
          fontWeight: 500,
          color: '#333',
          marginBottom: 6,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {task.title}
      </div>

      {/* 底部信息 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          fontSize: 11,
          color: '#999',
        }}
      >
        <div style={{ display: 'flex', gap: 12 }}>
          {/* 预估时间 */}
          <span>⏱️ {formatTaskTime(task.estimatedTime)}</span>
          {/* 依赖数 */}
          {task.dependencies.length > 0 && (
            <span>🔗 {task.dependencies.length} deps</span>
          )}
        </div>
        {/* 标签 */}
        {task.tags.length > 0 && (
          <div style={{ display: 'flex', gap: 4 }}>
            {task.tags.slice(0, 2).map((tag) => (
              <span
                key={tag}
                style={{
                  fontSize: 9,
                  padding: '1px 4px',
                  borderRadius: 3,
                  backgroundColor: '#f0f0f0',
                  color: '#666',
                }}
              >
                {tag}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

/**
 * 任务列表
 */
export const TaskList: React.FC<TaskListProps> = ({
  tasks,
  selectedTaskIds,
  onTaskSelect,
  onTaskReorder,
  onTaskClick,
}) => {
  const [draggedTaskId, setDraggedTaskId] = useState<string | null>(null);
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    setDragOverIndex(index);
  };

  const handleDrop = (e: React.DragEvent, targetIndex: number) => {
    e.preventDefault();
    if (draggedTaskId) {
      onTaskReorder?.(draggedTaskId, targetIndex);
    }
    setDraggedTaskId(null);
    setDragOverIndex(null);
  };

  const sortedTasks = useMemo(() => {
    return [...tasks].sort((a, b) => a.order - b.order);
  }, [tasks]);

  return (
    <div style={{ flex: 1, overflow: 'auto', padding: '0 16px' }}>
      {sortedTasks.length === 0 ? (
        <div style={{ textAlign: 'center', color: '#999', padding: 40 }}>
          No tasks
        </div>
      ) : (
        sortedTasks.map((task, index) => (
          <div
            key={task.id}
            onDragOver={(e) => handleDragOver(e, index)}
            onDrop={(e) => handleDrop(e, index)}
            style={{
              borderTop: dragOverIndex === index ? '2px solid #2196F3' : 'none',
            }}
          >
            <TaskCard
              task={task}
              isSelected={selectedTaskIds.includes(task.id)}
              isDragging={draggedTaskId === task.id}
              onSelect={(id) => {
                onTaskSelect?.(id);
                onTaskClick?.(id);
              }}
              onDragStart={setDraggedTaskId}
              onDragEnd={() => setDraggedTaskId(null)}
            />
          </div>
        ))
      )}
    </div>
  );
};

/**
 * 任务详情面板
 */
export const TaskDetailPanel: React.FC<TaskDetailPanelProps> = ({
  task,
  allTasks,
  modelPresets,
  onModelChange,
  onStatusChange,
  onClose,
}) => {
  const statusConfig = TASK_STATUS_CONFIG[task.status];
  const priorityConfig = TASK_PRIORITY_CONFIG[task.priority];

  const dependencies = useMemo(() => {
    return task.dependencies.map((dep) => {
      const depTask = allTasks.find((t) => t.id === dep.taskId);
      return { ...dep, task: depTask };
    });
  }, [task.dependencies, allTasks]);

  return (
    <div
      style={{
        width: 360,
        backgroundColor: '#fff',
        borderLeft: '1px solid #e0e0e0',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {/* 头部 */}
      <div
        style={{
          padding: '16px 20px',
          borderBottom: '1px solid #e0e0e0',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <span style={{ fontSize: 14, fontWeight: 600, color: '#333' }}>Task Details</span>
        <button
          onClick={onClose}
          style={{
            background: 'none',
            border: 'none',
            fontSize: 20,
            color: '#666',
            cursor: 'pointer',
          }}
        >
          ×
        </button>
      </div>

      {/* 内容 */}
      <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
        {/* 标题 */}
        <div style={{ marginBottom: 20 }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              marginBottom: 8,
            }}
          >
            <span
              style={{
                fontSize: 11,
                padding: '2px 8px',
                borderRadius: 4,
                backgroundColor: `${priorityConfig.color}20`,
                color: priorityConfig.color,
                fontWeight: 600,
              }}
            >
              {task.priority}
            </span>
            <span
              style={{
                fontSize: 11,
                padding: '2px 8px',
                borderRadius: 4,
                backgroundColor: `${statusConfig.color}20`,
                color: statusConfig.color,
              }}
            >
              {statusConfig.icon} {statusConfig.label}
            </span>
          </div>
          <div style={{ fontSize: 16, fontWeight: 600, color: '#333' }}>{task.title}</div>
        </div>

        {/* 描述 */}
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }}>
            Description
          </div>
          <div style={{ fontSize: 13, color: '#333', lineHeight: 1.5 }}>
            {task.description || 'No description'}
          </div>
        </div>

        {/* 模型选择器 */}
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }}>
            Model
          </div>
          <CustomSelect
            options={Object.entries(MODEL_CONFIG).map(([key, config]) => ({
              value: key,
              label: `${config.icon} ${config.label}`,
            }))}
            value={task.model}
            onChange={(value) => onModelChange?.(task.id, value as ModelType)}
            placeholder="Select model"
          />

          {/* 模型预设 */}
          {modelPresets.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <div style={{ fontSize: 11, color: '#666', marginBottom: 4 }}>Quick Presets:</div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                {modelPresets.map((preset) => (
                  <button
                    key={preset.id}
                    onClick={() => onModelChange?.(task.id, preset.model)}
                    style={{
                      padding: '4px 8px',
                      fontSize: 10,
                      border: '1px solid #e0e0e0',
                      borderRadius: 4,
                      backgroundColor: task.model === preset.model ? '#e3f2fd' : '#fff',
                      color: '#333',
                      cursor: 'pointer',
                    }}
                  >
                    {preset.name}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* 状态切换 */}
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }}>
            Status
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {Object.entries(TASK_STATUS_CONFIG).map(([key, config]) => (
              <button
                key={key}
                onClick={() => onStatusChange?.(task.id, key as TaskStatus)}
                style={{
                  padding: '6px 12px',
                  fontSize: 11,
                  border: `1px solid ${task.status === key ? config.color : '#e0e0e0'}`,
                  borderRadius: 6,
                  backgroundColor: task.status === key ? `${config.color}20` : '#fff',
                  color: task.status === key ? config.color : '#666',
                  cursor: 'pointer',
                }}
              >
                {config.icon} {config.label}
              </button>
            ))}
          </div>
        </div>

        {/* 依赖关系 */}
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }}>
            Dependencies ({dependencies.length})
          </div>
          {dependencies.length === 0 ? (
            <div style={{ fontSize: 12, color: '#999' }}>No dependencies</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {dependencies.map((dep) => (
                <div
                  key={dep.taskId}
                  style={{
                    padding: '8px 10px',
                    backgroundColor: '#f5f5f5',
                    borderRadius: 6,
                    fontSize: 12,
                  }}
                >
                  <span style={{ color: dep.type === 'blocks' ? '#F44336' : '#FF9800' }}>
                    {dep.type === 'blocks' ? '🚫 Blocks' : '⏳ Blocked by'}
                  </span>
                  <span style={{ marginLeft: 8, color: '#333' }}>
                    {dep.task?.title || dep.taskId}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* 时间信息 */}
        <div>
          <div style={{ fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }}>
            Time
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            <div style={{ padding: 10, backgroundColor: '#f5f5f5', borderRadius: 6 }}>
              <div style={{ fontSize: 10, color: '#666' }}>Estimated</div>
              <div style={{ fontSize: 14, fontWeight: 500, color: '#333' }}>
                {formatTaskTime(task.estimatedTime)}
              </div>
            </div>
            <div style={{ padding: 10, backgroundColor: '#f5f5f5', borderRadius: 6 }}>
              <div style={{ fontSize: 10, color: '#666' }}>Actual</div>
              <div style={{ fontSize: 14, fontWeight: 500, color: '#333' }}>
                {formatTaskTime(task.actualTime)}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

/**
 * 批量操作栏
 */
export const BatchActions: React.FC<BatchActionsProps> = ({
  selectedCount,
  onBatchModelChange,
  onBatchStatusChange,
  onClearSelection,
}) => {
  if (selectedCount === 0) return null;

  return (
    <div
      style={{
        padding: '10px 16px',
        backgroundColor: '#e3f2fd',
        borderBottom: '1px solid #bbdefb',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <span style={{ fontSize: 13, fontWeight: 500, color: '#1976D2' }}>
          {selectedCount} selected
        </span>

        {/* 批量切换模型 */}
        <CustomSelect
          options={Object.entries(MODEL_CONFIG).map(([key, config]) => ({
            value: key,
            label: config.label,
          }))}
          value=""
          onChange={(value) => onBatchModelChange?.(value as ModelType)}
          placeholder="Change Model..."
          style={{ minWidth: 140 }}
        />

        {/* 批量切换状态 */}
        <CustomSelect
          options={Object.entries(TASK_STATUS_CONFIG).map(([key, config]) => ({
            value: key,
            label: config.label,
          }))}
          value=""
          onChange={(value) => onBatchStatusChange?.(value as TaskStatus)}
          placeholder="Change Status..."
          style={{ minWidth: 140 }}
        />
      </div>

      <button
        onClick={onClearSelection}
        style={{
          padding: '6px 12px',
          fontSize: 12,
          border: 'none',
          borderRadius: 4,
          backgroundColor: '#fff',
          color: '#666',
          cursor: 'pointer',
        }}
      >
        Clear Selection
      </button>
    </div>
  );
};

/**
 * 简化Gantt图
 */
export const GanttChart: React.FC<GanttChartProps> = ({ tasks, startDate, endDate, onTaskClick }) => {
  const totalDays = Math.ceil((endDate.getTime() - startDate.getTime()) / (1000 * 60 * 60 * 24));
  const dayWidth = 40;

  const sortedTasks = useMemo(() => {
    return [...tasks].sort((a, b) => (a.startTime || 0) - (b.startTime || 0));
  }, [tasks]);

  return (
    <div style={{ flex: 1, overflow: 'auto' }}>
      {/* 时间轴头部 */}
      <div
        style={{
          display: 'flex',
          borderBottom: '1px solid #e0e0e0',
          backgroundColor: '#fafafa',
          position: 'sticky',
          top: 0,
        }}
      >
        <div style={{ width: 200, padding: '10px 12px', borderRight: '1px solid #e0e0e0' }}>
          <span style={{ fontSize: 12, fontWeight: 500, color: '#666' }}>Task</span>
        </div>
        <div style={{ display: 'flex' }}>
          {Array.from({ length: totalDays }).map((_, i) => {
            const date = new Date(startDate.getTime() + i * 24 * 60 * 60 * 1000);
            return (
              <div
                key={i}
                style={{
                  width: dayWidth,
                  padding: '10px 4px',
                  textAlign: 'center',
                  borderRight: '1px solid #f0f0f0',
                  fontSize: 10,
                  color: '#666',
                }}
              >
                {date.getDate()}
              </div>
            );
          })}
        </div>
      </div>

      {/* 任务行 */}
      {sortedTasks.map((task) => {
        const priorityConfig = TASK_PRIORITY_CONFIG[task.priority];
        const taskStart = task.startTime
          ? Math.max(0, Math.floor((task.startTime - startDate.getTime()) / (1000 * 60 * 60 * 24)))
          : 0;
        const taskDuration = task.estimatedTime
          ? Math.ceil(task.estimatedTime / (8 * 60))
          : 1;

        return (
          <div
            key={task.id}
            style={{
              display: 'flex',
              borderBottom: '1px solid #f0f0f0',
              cursor: 'pointer',
            }}
            onClick={() => onTaskClick?.(task.id)}
          >
            {/* 任务名称 */}
            <div
              style={{
                width: 200,
                padding: '12px',
                borderRight: '1px solid #e0e0e0',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                fontSize: 12,
                color: '#333',
              }}
            >
              <span
                style={{
                  display: 'inline-block',
                  width: 8,
                  height: 8,
                  borderRadius: '50%',
                  backgroundColor: priorityConfig.color,
                  marginRight: 8,
                }}
              />
              {task.title}
            </div>

            {/* 甘特条 */}
            <div style={{ position: 'relative', flex: 1, height: 40 }}>
              <div
                style={{
                  position: 'absolute',
                  left: taskStart * dayWidth,
                  top: 10,
                  width: taskDuration * dayWidth - 4,
                  height: 20,
                  backgroundColor: priorityConfig.color,
                  borderRadius: 4,
                  opacity: 0.8,
                }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
};

/**
 * Plan模式任务看板
 */
export const PlanBoard: React.FC<PlanBoardProps> = ({
  tasks,
  modelPresets,
  viewMode = 'list',
  onTaskSelect,
  onTaskReorder,
  onTaskModelChange,
  onTaskStatusChange,
  onBatchModelChange,
  onViewModeChange,
  className,
  style,
}) => {
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([]);
  const [detailTaskId, setDetailTaskId] = useState<string | null>(null);

  const progress = useMemo(() => calculateTaskProgress(tasks), [tasks]);
  const detailTask = useMemo(
    () => tasks.find((t) => t.id === detailTaskId),
    [tasks, detailTaskId]
  );

  const handleTaskSelect = useCallback(
    (taskId: string, multiSelect?: boolean) => {
      setSelectedTaskIds((prev) => {
        if (multiSelect) {
          return prev.includes(taskId)
            ? prev.filter((id) => id !== taskId)
            : [...prev, taskId];
        }
        return prev.includes(taskId) ? [] : [taskId];
      });
      onTaskSelect?.(taskId, multiSelect);
    },
    [onTaskSelect]
  );

  const handleBatchModelChange = useCallback(
    (model: ModelType) => {
      onBatchModelChange?.(selectedTaskIds, model);
      setSelectedTaskIds([]);
    },
    [selectedTaskIds, onBatchModelChange]
  );

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        backgroundColor: '#f5f5f5',
        ...style,
      }}
    >
      {/* 头部 */}
      <div
        style={{
          padding: '16px 20px',
          backgroundColor: '#fff',
          borderBottom: '1px solid #e0e0e0',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: 12,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ fontSize: 18 }}>📋</span>
            <span style={{ fontSize: 16, fontWeight: 600, color: '#333' }}>Plan Board</span>
          </div>

          {/* 视图切换 */}
          <div style={{ display: 'flex', gap: 4 }}>
            <button
              onClick={() => onViewModeChange?.('list')}
              style={{
                padding: '6px 12px',
                fontSize: 12,
                border: '1px solid #e0e0e0',
                borderRadius: '4px 0 0 4px',
                backgroundColor: viewMode === 'list' ? '#e3f2fd' : '#fff',
                color: viewMode === 'list' ? '#1976D2' : '#666',
                cursor: 'pointer',
              }}
            >
              📝 List
            </button>
            <button
              onClick={() => onViewModeChange?.('gantt')}
              style={{
                padding: '6px 12px',
                fontSize: 12,
                border: '1px solid #e0e0e0',
                borderLeft: 'none',
                borderRadius: '0 4px 4px 0',
                backgroundColor: viewMode === 'gantt' ? '#e3f2fd' : '#fff',
                color: viewMode === 'gantt' ? '#1976D2' : '#666',
                cursor: 'pointer',
              }}
            >
              📊 Gantt
            </button>
          </div>
        </div>

        {/* 进度条 */}
        <div>
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              marginBottom: 6,
              fontSize: 12,
            }}
          >
            <span style={{ color: '#666' }}>
              {progress.completed}/{progress.total} tasks completed
            </span>
            <span style={{ color: '#4CAF50', fontWeight: 500 }}>
              {progress.percentage.toFixed(0)}%
            </span>
          </div>
          <div
            style={{
              height: 6,
              backgroundColor: '#e0e0e0',
              borderRadius: 3,
              overflow: 'hidden',
            }}
          >
            <div
              style={{
                height: '100%',
                width: `${progress.percentage}%`,
                backgroundColor: '#4CAF50',
                transition: 'width 0.3s',
              }}
            />
          </div>
        </div>
      </div>

      {/* 批量操作栏 */}
      <BatchActions
        selectedCount={selectedTaskIds.length}
        onBatchModelChange={handleBatchModelChange}
        onBatchStatusChange={(status) => {
          selectedTaskIds.forEach((id) => onTaskStatusChange?.(id, status));
          setSelectedTaskIds([]);
        }}
        onClearSelection={() => setSelectedTaskIds([])}
      />

      {/* 主内容区 */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* 任务列表/Gantt图 */}
        {viewMode === 'list' ? (
          <TaskList
            tasks={tasks}
            selectedTaskIds={selectedTaskIds}
            onTaskSelect={handleTaskSelect}
            onTaskReorder={onTaskReorder}
            onTaskClick={setDetailTaskId}
          />
        ) : (
          <GanttChart
            tasks={tasks}
            startDate={new Date()}
            endDate={new Date(Date.now() + 14 * 24 * 60 * 60 * 1000)}
            onTaskClick={setDetailTaskId}
          />
        )}

        {/* 详情面板 */}
        {detailTask && (
          <TaskDetailPanel
            task={detailTask}
            allTasks={tasks}
            modelPresets={modelPresets}
            onModelChange={onTaskModelChange}
            onStatusChange={onTaskStatusChange}
            onClose={() => setDetailTaskId(null)}
          />
        )}
      </div>
    </div>
  );
};

export default PlanBoard;
