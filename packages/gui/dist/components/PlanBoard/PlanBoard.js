import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Plan模式任务看板组件
 * 任务列表+详情面板支持拖拽排序与批量模型切换
 */
import { useState, useMemo, useCallback } from 'react';
import { TASK_STATUS_CONFIG, TASK_PRIORITY_CONFIG, MODEL_CONFIG, formatTaskTime, calculateTaskProgress, } from './types';
import { CustomSelect } from '../shared/CustomSelect';
/**
 * 任务卡片
 */
export const TaskCard = ({ task, isSelected, isDragging, onSelect, onDragStart, onDragEnd, }) => {
    const statusConfig = TASK_STATUS_CONFIG[task.status];
    const priorityConfig = TASK_PRIORITY_CONFIG[task.priority];
    const modelConfig = MODEL_CONFIG[task.model];
    return (_jsxs("div", { draggable: true, onDragStart: () => onDragStart?.(task.id), onDragEnd: onDragEnd, onClick: () => onSelect?.(task.id), style: {
            padding: '12px 14px',
            backgroundColor: isSelected ? '#e3f2fd' : '#fff',
            border: `1px solid ${isSelected ? '#2196F3' : '#e0e0e0'}`,
            borderLeft: `4px solid ${priorityConfig.color}`,
            borderRadius: '0 8px 8px 0',
            marginBottom: 8,
            cursor: 'grab',
            opacity: isDragging ? 0.5 : 1,
            transition: 'all 0.15s',
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: 8,
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("input", { type: "checkbox", checked: task.isSelected, onChange: (e) => {
                                    e.stopPropagation();
                                    onSelect?.(task.id);
                                }, style: { cursor: 'pointer' } }), _jsx("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 6px',
                                    borderRadius: 4,
                                    backgroundColor: `${priorityConfig.color}20`,
                                    color: priorityConfig.color,
                                    fontWeight: 600,
                                }, children: task.priority }), _jsxs("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 6px',
                                    borderRadius: 4,
                                    backgroundColor: `${statusConfig.color}20`,
                                    color: statusConfig.color,
                                }, children: [statusConfig.icon, " ", statusConfig.label] })] }), _jsxs("span", { style: {
                            fontSize: 10,
                            padding: '2px 6px',
                            borderRadius: 4,
                            backgroundColor: `${modelConfig.color}15`,
                            color: modelConfig.color,
                        }, children: [modelConfig.icon, " ", modelConfig.label] })] }), _jsx("div", { style: {
                    fontSize: 13,
                    fontWeight: 500,
                    color: '#333',
                    marginBottom: 6,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                }, children: task.title }), _jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    fontSize: 11,
                    color: '#999',
                }, children: [_jsxs("div", { style: { display: 'flex', gap: 12 }, children: [_jsxs("span", { children: ["\u23F1\uFE0F ", formatTaskTime(task.estimatedTime)] }), task.dependencies.length > 0 && (_jsxs("span", { children: ["\uD83D\uDD17 ", task.dependencies.length, " deps"] }))] }), task.tags.length > 0 && (_jsx("div", { style: { display: 'flex', gap: 4 }, children: task.tags.slice(0, 2).map((tag) => (_jsx("span", { style: {
                                fontSize: 9,
                                padding: '1px 4px',
                                borderRadius: 3,
                                backgroundColor: '#f0f0f0',
                                color: '#666',
                            }, children: tag }, tag))) }))] })] }));
};
/**
 * 任务列表
 */
export const TaskList = ({ tasks, selectedTaskIds, onTaskSelect, onTaskReorder, onTaskClick, }) => {
    const [draggedTaskId, setDraggedTaskId] = useState(null);
    const [dragOverIndex, setDragOverIndex] = useState(null);
    const handleDragOver = (e, index) => {
        e.preventDefault();
        setDragOverIndex(index);
    };
    const handleDrop = (e, targetIndex) => {
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
    return (_jsx("div", { style: { flex: 1, overflow: 'auto', padding: '0 16px' }, children: sortedTasks.length === 0 ? (_jsx("div", { style: { textAlign: 'center', color: '#999', padding: 40 }, children: "No tasks" })) : (sortedTasks.map((task, index) => (_jsx("div", { onDragOver: (e) => handleDragOver(e, index), onDrop: (e) => handleDrop(e, index), style: {
                borderTop: dragOverIndex === index ? '2px solid #2196F3' : 'none',
            }, children: _jsx(TaskCard, { task: task, isSelected: selectedTaskIds.includes(task.id), isDragging: draggedTaskId === task.id, onSelect: (id) => {
                    onTaskSelect?.(id);
                    onTaskClick?.(id);
                }, onDragStart: setDraggedTaskId, onDragEnd: () => setDraggedTaskId(null) }) }, task.id)))) }));
};
/**
 * 任务详情面板
 */
export const TaskDetailPanel = ({ task, allTasks, modelPresets, onModelChange, onStatusChange, onClose, }) => {
    const statusConfig = TASK_STATUS_CONFIG[task.status];
    const priorityConfig = TASK_PRIORITY_CONFIG[task.priority];
    const dependencies = useMemo(() => {
        return task.dependencies.map((dep) => {
            const depTask = allTasks.find((t) => t.id === dep.taskId);
            return { ...dep, task: depTask };
        });
    }, [task.dependencies, allTasks]);
    return (_jsxs("div", { style: {
            width: 360,
            backgroundColor: '#fff',
            borderLeft: '1px solid #e0e0e0',
            display: 'flex',
            flexDirection: 'column',
        }, children: [_jsxs("div", { style: {
                    padding: '16px 20px',
                    borderBottom: '1px solid #e0e0e0',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                }, children: [_jsx("span", { style: { fontSize: 14, fontWeight: 600, color: '#333' }, children: "Task Details" }), _jsx("button", { onClick: onClose, style: {
                            background: 'none',
                            border: 'none',
                            fontSize: 20,
                            color: '#666',
                            cursor: 'pointer',
                        }, children: "\u00D7" })] }), _jsxs("div", { style: { flex: 1, overflow: 'auto', padding: 20 }, children: [_jsxs("div", { style: { marginBottom: 20 }, children: [_jsxs("div", { style: {
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 8,
                                    marginBottom: 8,
                                }, children: [_jsx("span", { style: {
                                            fontSize: 11,
                                            padding: '2px 8px',
                                            borderRadius: 4,
                                            backgroundColor: `${priorityConfig.color}20`,
                                            color: priorityConfig.color,
                                            fontWeight: 600,
                                        }, children: task.priority }), _jsxs("span", { style: {
                                            fontSize: 11,
                                            padding: '2px 8px',
                                            borderRadius: 4,
                                            backgroundColor: `${statusConfig.color}20`,
                                            color: statusConfig.color,
                                        }, children: [statusConfig.icon, " ", statusConfig.label] })] }), _jsx("div", { style: { fontSize: 16, fontWeight: 600, color: '#333' }, children: task.title })] }), _jsxs("div", { style: { marginBottom: 20 }, children: [_jsx("div", { style: { fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }, children: "Description" }), _jsx("div", { style: { fontSize: 13, color: '#333', lineHeight: 1.5 }, children: task.description || 'No description' })] }), _jsxs("div", { style: { marginBottom: 20 }, children: [_jsx("div", { style: { fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }, children: "Model" }), _jsx(CustomSelect, { options: Object.entries(MODEL_CONFIG).map(([key, config]) => ({
                                    value: key,
                                    label: `${config.icon} ${config.label}`,
                                })), value: task.model, onChange: (value) => onModelChange?.(task.id, value), placeholder: "Select model" }), modelPresets.length > 0 && (_jsxs("div", { style: { marginTop: 8 }, children: [_jsx("div", { style: { fontSize: 11, color: '#666', marginBottom: 4 }, children: "Quick Presets:" }), _jsx("div", { style: { display: 'flex', flexWrap: 'wrap', gap: 4 }, children: modelPresets.map((preset) => (_jsx("button", { onClick: () => onModelChange?.(task.id, preset.model), style: {
                                                padding: '4px 8px',
                                                fontSize: 10,
                                                border: '1px solid #e0e0e0',
                                                borderRadius: 4,
                                                backgroundColor: task.model === preset.model ? '#e3f2fd' : '#fff',
                                                color: '#333',
                                                cursor: 'pointer',
                                            }, children: preset.name }, preset.id))) })] }))] }), _jsxs("div", { style: { marginBottom: 20 }, children: [_jsx("div", { style: { fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }, children: "Status" }), _jsx("div", { style: { display: 'flex', flexWrap: 'wrap', gap: 6 }, children: Object.entries(TASK_STATUS_CONFIG).map(([key, config]) => (_jsxs("button", { onClick: () => onStatusChange?.(task.id, key), style: {
                                        padding: '6px 12px',
                                        fontSize: 11,
                                        border: `1px solid ${task.status === key ? config.color : '#e0e0e0'}`,
                                        borderRadius: 6,
                                        backgroundColor: task.status === key ? `${config.color}20` : '#fff',
                                        color: task.status === key ? config.color : '#666',
                                        cursor: 'pointer',
                                    }, children: [config.icon, " ", config.label] }, key))) })] }), _jsxs("div", { style: { marginBottom: 20 }, children: [_jsxs("div", { style: { fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }, children: ["Dependencies (", dependencies.length, ")"] }), dependencies.length === 0 ? (_jsx("div", { style: { fontSize: 12, color: '#999' }, children: "No dependencies" })) : (_jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: 6 }, children: dependencies.map((dep) => (_jsxs("div", { style: {
                                        padding: '8px 10px',
                                        backgroundColor: '#f5f5f5',
                                        borderRadius: 6,
                                        fontSize: 12,
                                    }, children: [_jsx("span", { style: { color: dep.type === 'blocks' ? '#F44336' : '#FF9800' }, children: dep.type === 'blocks' ? '🚫 Blocks' : '⏳ Blocked by' }), _jsx("span", { style: { marginLeft: 8, color: '#333' }, children: dep.task?.title || dep.taskId })] }, dep.taskId))) }))] }), _jsxs("div", { children: [_jsx("div", { style: { fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }, children: "Time" }), _jsxs("div", { style: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }, children: [_jsxs("div", { style: { padding: 10, backgroundColor: '#f5f5f5', borderRadius: 6 }, children: [_jsx("div", { style: { fontSize: 10, color: '#666' }, children: "Estimated" }), _jsx("div", { style: { fontSize: 14, fontWeight: 500, color: '#333' }, children: formatTaskTime(task.estimatedTime) })] }), _jsxs("div", { style: { padding: 10, backgroundColor: '#f5f5f5', borderRadius: 6 }, children: [_jsx("div", { style: { fontSize: 10, color: '#666' }, children: "Actual" }), _jsx("div", { style: { fontSize: 14, fontWeight: 500, color: '#333' }, children: formatTaskTime(task.actualTime) })] })] })] })] })] }));
};
/**
 * 批量操作栏
 */
export const BatchActions = ({ selectedCount, onBatchModelChange, onBatchStatusChange, onClearSelection, }) => {
    if (selectedCount === 0)
        return null;
    return (_jsxs("div", { style: {
            padding: '10px 16px',
            backgroundColor: '#e3f2fd',
            borderBottom: '1px solid #bbdefb',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
        }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 12 }, children: [_jsxs("span", { style: { fontSize: 13, fontWeight: 500, color: '#1976D2' }, children: [selectedCount, " selected"] }), _jsx(CustomSelect, { options: Object.entries(MODEL_CONFIG).map(([key, config]) => ({
                            value: key,
                            label: config.label,
                        })), value: "", onChange: (value) => onBatchModelChange?.(value), placeholder: "Change Model...", style: { minWidth: 140 } }), _jsx(CustomSelect, { options: Object.entries(TASK_STATUS_CONFIG).map(([key, config]) => ({
                            value: key,
                            label: config.label,
                        })), value: "", onChange: (value) => onBatchStatusChange?.(value), placeholder: "Change Status...", style: { minWidth: 140 } })] }), _jsx("button", { onClick: onClearSelection, style: {
                    padding: '6px 12px',
                    fontSize: 12,
                    border: 'none',
                    borderRadius: 4,
                    backgroundColor: '#fff',
                    color: '#666',
                    cursor: 'pointer',
                }, children: "Clear Selection" })] }));
};
/**
 * 简化Gantt图
 */
export const GanttChart = ({ tasks, startDate, endDate, onTaskClick }) => {
    const totalDays = Math.ceil((endDate.getTime() - startDate.getTime()) / (1000 * 60 * 60 * 24));
    const dayWidth = 40;
    const sortedTasks = useMemo(() => {
        return [...tasks].sort((a, b) => (a.startTime || 0) - (b.startTime || 0));
    }, [tasks]);
    return (_jsxs("div", { style: { flex: 1, overflow: 'auto' }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    borderBottom: '1px solid #e0e0e0',
                    backgroundColor: '#fafafa',
                    position: 'sticky',
                    top: 0,
                }, children: [_jsx("div", { style: { width: 200, padding: '10px 12px', borderRight: '1px solid #e0e0e0' }, children: _jsx("span", { style: { fontSize: 12, fontWeight: 500, color: '#666' }, children: "Task" }) }), _jsx("div", { style: { display: 'flex' }, children: Array.from({ length: totalDays }).map((_, i) => {
                            const date = new Date(startDate.getTime() + i * 24 * 60 * 60 * 1000);
                            return (_jsx("div", { style: {
                                    width: dayWidth,
                                    padding: '10px 4px',
                                    textAlign: 'center',
                                    borderRight: '1px solid #f0f0f0',
                                    fontSize: 10,
                                    color: '#666',
                                }, children: date.getDate() }, i));
                        }) })] }), sortedTasks.map((task) => {
                const priorityConfig = TASK_PRIORITY_CONFIG[task.priority];
                const taskStart = task.startTime
                    ? Math.max(0, Math.floor((task.startTime - startDate.getTime()) / (1000 * 60 * 60 * 24)))
                    : 0;
                const taskDuration = task.estimatedTime
                    ? Math.ceil(task.estimatedTime / (8 * 60))
                    : 1;
                return (_jsxs("div", { style: {
                        display: 'flex',
                        borderBottom: '1px solid #f0f0f0',
                        cursor: 'pointer',
                    }, onClick: () => onTaskClick?.(task.id), children: [_jsxs("div", { style: {
                                width: 200,
                                padding: '12px',
                                borderRight: '1px solid #e0e0e0',
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                fontSize: 12,
                                color: '#333',
                            }, children: [_jsx("span", { style: {
                                        display: 'inline-block',
                                        width: 8,
                                        height: 8,
                                        borderRadius: '50%',
                                        backgroundColor: priorityConfig.color,
                                        marginRight: 8,
                                    } }), task.title] }), _jsx("div", { style: { position: 'relative', flex: 1, height: 40 }, children: _jsx("div", { style: {
                                    position: 'absolute',
                                    left: taskStart * dayWidth,
                                    top: 10,
                                    width: taskDuration * dayWidth - 4,
                                    height: 20,
                                    backgroundColor: priorityConfig.color,
                                    borderRadius: 4,
                                    opacity: 0.8,
                                } }) })] }, task.id));
            })] }));
};
/**
 * Plan模式任务看板
 */
export const PlanBoard = ({ tasks, modelPresets, viewMode = 'list', onTaskSelect, onTaskReorder, onTaskModelChange, onTaskStatusChange, onBatchModelChange, onViewModeChange, className, style, }) => {
    const [selectedTaskIds, setSelectedTaskIds] = useState([]);
    const [detailTaskId, setDetailTaskId] = useState(null);
    const progress = useMemo(() => calculateTaskProgress(tasks), [tasks]);
    const detailTask = useMemo(() => tasks.find((t) => t.id === detailTaskId), [tasks, detailTaskId]);
    const handleTaskSelect = useCallback((taskId, multiSelect) => {
        setSelectedTaskIds((prev) => {
            if (multiSelect) {
                return prev.includes(taskId)
                    ? prev.filter((id) => id !== taskId)
                    : [...prev, taskId];
            }
            return prev.includes(taskId) ? [] : [taskId];
        });
        onTaskSelect?.(taskId, multiSelect);
    }, [onTaskSelect]);
    const handleBatchModelChange = useCallback((model) => {
        onBatchModelChange?.(selectedTaskIds, model);
        setSelectedTaskIds([]);
    }, [selectedTaskIds, onBatchModelChange]);
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            backgroundColor: '#f5f5f5',
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: '16px 20px',
                    backgroundColor: '#fff',
                    borderBottom: '1px solid #e0e0e0',
                }, children: [_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            marginBottom: 12,
                        }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: { fontSize: 18 }, children: "\uD83D\uDCCB" }), _jsx("span", { style: { fontSize: 16, fontWeight: 600, color: '#333' }, children: "Plan Board" })] }), _jsxs("div", { style: { display: 'flex', gap: 4 }, children: [_jsx("button", { onClick: () => onViewModeChange?.('list'), style: {
                                            padding: '6px 12px',
                                            fontSize: 12,
                                            border: '1px solid #e0e0e0',
                                            borderRadius: '4px 0 0 4px',
                                            backgroundColor: viewMode === 'list' ? '#e3f2fd' : '#fff',
                                            color: viewMode === 'list' ? '#1976D2' : '#666',
                                            cursor: 'pointer',
                                        }, children: "\uD83D\uDCDD List" }), _jsx("button", { onClick: () => onViewModeChange?.('gantt'), style: {
                                            padding: '6px 12px',
                                            fontSize: 12,
                                            border: '1px solid #e0e0e0',
                                            borderLeft: 'none',
                                            borderRadius: '0 4px 4px 0',
                                            backgroundColor: viewMode === 'gantt' ? '#e3f2fd' : '#fff',
                                            color: viewMode === 'gantt' ? '#1976D2' : '#666',
                                            cursor: 'pointer',
                                        }, children: "\uD83D\uDCCA Gantt" })] })] }), _jsxs("div", { children: [_jsxs("div", { style: {
                                    display: 'flex',
                                    justifyContent: 'space-between',
                                    marginBottom: 6,
                                    fontSize: 12,
                                }, children: [_jsxs("span", { style: { color: '#666' }, children: [progress.completed, "/", progress.total, " tasks completed"] }), _jsxs("span", { style: { color: '#4CAF50', fontWeight: 500 }, children: [progress.percentage.toFixed(0), "%"] })] }), _jsx("div", { style: {
                                    height: 6,
                                    backgroundColor: '#e0e0e0',
                                    borderRadius: 3,
                                    overflow: 'hidden',
                                }, children: _jsx("div", { style: {
                                        height: '100%',
                                        width: `${progress.percentage}%`,
                                        backgroundColor: '#4CAF50',
                                        transition: 'width 0.3s',
                                    } }) })] })] }), _jsx(BatchActions, { selectedCount: selectedTaskIds.length, onBatchModelChange: handleBatchModelChange, onBatchStatusChange: (status) => {
                    selectedTaskIds.forEach((id) => onTaskStatusChange?.(id, status));
                    setSelectedTaskIds([]);
                }, onClearSelection: () => setSelectedTaskIds([]) }), _jsxs("div", { style: { flex: 1, display: 'flex', overflow: 'hidden' }, children: [viewMode === 'list' ? (_jsx(TaskList, { tasks: tasks, selectedTaskIds: selectedTaskIds, onTaskSelect: handleTaskSelect, onTaskReorder: onTaskReorder, onTaskClick: setDetailTaskId })) : (_jsx(GanttChart, { tasks: tasks, startDate: new Date(), endDate: new Date(Date.now() + 14 * 24 * 60 * 60 * 1000), onTaskClick: setDetailTaskId })), detailTask && (_jsx(TaskDetailPanel, { task: detailTask, allTasks: tasks, modelPresets: modelPresets, onModelChange: onTaskModelChange, onStatusChange: onTaskStatusChange, onClose: () => setDetailTaskId(null) }))] })] }));
};
export default PlanBoard;
//# sourceMappingURL=PlanBoard.js.map