/**
 * MemoryView - 记忆节点视图
 * 交互式节点图、节点详情面板、关系匹配度显示
 */

import React, { useState, useEffect, useCallback, useRef } from 'react';
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
import { Card, CardContent, CardHeader } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { ProgressBar } from '../shared/ProgressBar';
import { Button } from '../shared/Button';

export interface MemoryNode {
  id: string;
  label: string;
  type: 'concept' | 'entity' | 'relation' | 'fact';
  content: string;
  connections: string[];
  relevance: number;
  x?: number;
  y?: number;
}

export interface MemoryViewProps {
  nodes?: MemoryNode[];
  onSelectNode?: (nodeId: string) => void;
  onDeleteNode?: (nodeId: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

// Icons
const BrainIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M9.5 2A2.5 2.5 0 0 1 12 4.5v15a2.5 2.5 0 0 1-4.96.44 2.5 2.5 0 0 1-2.96-3.08 3 3 0 0 1-.34-5.58 2.5 2.5 0 0 1 1.32-4.24 2.5 2.5 0 0 1 4.44-1.54" />
    <path d="M14.5 2A2.5 2.5 0 0 0 12 4.5v15a2.5 2.5 0 0 0 4.96.44 2.5 2.5 0 0 0 2.96-3.08 3 3 0 0 0 .34-5.58 2.5 2.5 0 0 0-1.32-4.24 2.5 2.5 0 0 0-4.44-1.54" />
  </svg>
);

const CloseIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <line x1="18" y1="6" x2="6" y2="18" />
    <line x1="6" y1="6" x2="18" y2="18" />
  </svg>
);

const TrashIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="3 6 5 6 21 6" />
    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
  </svg>
);

// Demo data
const demoNodes: MemoryNode[] = [
  { id: '1', label: 'React Hooks', type: 'concept', content: 'React Hooks are functions that let you use state and other React features in functional components.', connections: ['2', '3'], relevance: 95 },
  { id: '2', label: 'useState', type: 'entity', content: 'useState is a Hook that lets you add React state to function components.', connections: ['1', '4'], relevance: 88 },
  { id: '3', label: 'useEffect', type: 'entity', content: 'useEffect lets you perform side effects in function components.', connections: ['1', '5'], relevance: 85 },
  { id: '4', label: 'State Management', type: 'concept', content: 'State management is the process of managing the state of an application.', connections: ['2', '6'], relevance: 78 },
  { id: '5', label: 'Side Effects', type: 'relation', content: 'Side effects are operations that affect something outside the scope of the function.', connections: ['3'], relevance: 72 },
  { id: '6', label: 'Redux', type: 'entity', content: 'Redux is a predictable state container for JavaScript apps.', connections: ['4'], relevance: 65 },
  { id: '7', label: 'Component Lifecycle', type: 'fact', content: 'React components have a lifecycle that includes mounting, updating, and unmounting phases.', connections: ['1', '3'], relevance: 60 },
];

// Node type colors
const nodeTypeColors: Record<string, { bg: string; border: string; text: string }> = {
  concept: { bg: colors.primary[100], border: colors.primary[400], text: colors.primary[700] },
  entity: { bg: colors.indigo[100], border: colors.indigo[400], text: colors.indigo[700] },
  relation: { bg: colors.success.light, border: colors.success.main, text: colors.success.dark },
  fact: { bg: colors.warning.light, border: colors.warning.main, text: colors.warning.dark },
};

// Initialize node positions
const initializePositions = (nodes: MemoryNode[]): MemoryNode[] => {
  const centerX = 300;
  const centerY = 200;
  const radius = 150;

  return nodes.map((node, index) => {
    const angle = (index / nodes.length) * 2 * Math.PI;
    return {
      ...node,
      x: centerX + radius * Math.cos(angle) + (Math.random() - 0.5) * 50,
      y: centerY + radius * Math.sin(angle) + (Math.random() - 0.5) * 50,
    };
  });
};

// Node Component
const NodeCircle: React.FC<{
  node: MemoryNode;
  isSelected: boolean;
  onClick: () => void;
  onDrag: (x: number, y: number) => void;
}> = ({ node, isSelected, onClick, onDrag }) => {
  const [isDragging, setIsDragging] = useState(false);
  const nodeRef = useRef<HTMLDivElement>(null);
  const typeStyle = nodeTypeColors[node.type] || nodeTypeColors.concept;

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsDragging(true);
  }, []);

  useEffect(() => {
    if (!isDragging) return;

    const handleMouseMove = (e: MouseEvent) => {
      const parent = nodeRef.current?.parentElement;
      if (!parent) return;
      const rect = parent.getBoundingClientRect();
      onDrag(e.clientX - rect.left, e.clientY - rect.top);
    };

    const handleMouseUp = () => {
      setIsDragging(false);
    };

    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);

    return () => {
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
    };
  }, [isDragging, onDrag]);

  return (
    <div
      ref={nodeRef}
      onClick={onClick}
      onMouseDown={handleMouseDown}
      style={{
        position: 'absolute',
        left: (node.x || 0) - 40,
        top: (node.y || 0) - 40,
        width: 80,
        height: 80,
        borderRadius: borderRadius.full,
        backgroundColor: typeStyle.bg,
        border: `3px solid ${isSelected ? colors.primary[500] : typeStyle.border}`,
        boxShadow: isSelected ? `0 0 0 4px ${colors.primary[200]}` : shadows.md,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        cursor: isDragging ? 'grabbing' : 'grab',
        transition: isDragging ? 'none' : transitions.fast,
        zIndex: isSelected ? 10 : 1,
        transform: isSelected ? 'scale(1.1)' : 'scale(1)',
      }}
    >
      <span
        style={{
          fontSize: fontSize.xs,
          fontWeight: fontWeight.semibold,
          color: typeStyle.text,
          textAlign: 'center',
          padding: spacing[1],
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          maxWidth: 70,
        }}
      >
        {node.label}
      </span>
    </div>
  );
};

// Detail Panel
const DetailPanel: React.FC<{
  node: MemoryNode;
  allNodes: MemoryNode[];
  onClose: () => void;
  onDelete: () => void;
}> = ({ node, allNodes, onClose, onDelete }) => {
  const typeStyle = nodeTypeColors[node.type] || nodeTypeColors.concept;
  const connectedNodes = allNodes.filter((n) => node.connections.includes(n.id));

  return (
    <Card style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <CardHeader
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          borderBottom: `1px solid ${colors.slate[200]}`,
        }}
      >
        <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
          Node Details
        </h3>
        <button
          onClick={onClose}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: spacing[1],
            backgroundColor: 'transparent',
            border: 'none',
            cursor: 'pointer',
            color: colors.slate[400],
            borderRadius: borderRadius.md,
          }}
        >
          <CloseIcon />
        </button>
      </CardHeader>
      <CardContent style={{ flex: 1, overflowY: 'auto' }}>
        {/* Node info */}
        <div style={{ marginBottom: spacing[4] }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2], marginBottom: spacing[2] }}>
            <h4 style={{ fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
              {node.label}
            </h4>
            <Badge style={{ backgroundColor: typeStyle.bg, color: typeStyle.text }}>
              {node.type}
            </Badge>
          </div>
          <p style={{ fontSize: fontSize.sm, color: colors.slate[600], lineHeight: 1.6 }}>
            {node.content}
          </p>
        </div>

        {/* Relevance */}
        <div style={{ marginBottom: spacing[4] }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: spacing[1] }}>
            <span style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>Relevance</span>
            <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
              {node.relevance}%
            </span>
          </div>
          <ProgressBar value={node.relevance} status={node.relevance > 80 ? 'success' : 'default'} />
        </div>

        {/* Connections */}
        <div style={{ marginBottom: spacing[4] }}>
          <h5 style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }}>
            Connected Nodes ({connectedNodes.length})
          </h5>
          <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[2] }}>
            {connectedNodes.map((connNode) => {
              const connStyle = nodeTypeColors[connNode.type] || nodeTypeColors.concept;
              return (
                <div
                  key={connNode.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: spacing[2],
                    padding: spacing[2],
                    backgroundColor: colors.slate[50],
                    borderRadius: borderRadius.lg,
                  }}
                >
                  <div
                    style={{
                      width: 8,
                      height: 8,
                      borderRadius: borderRadius.full,
                      backgroundColor: connStyle.border,
                    }}
                  />
                  <span style={{ fontSize: fontSize.sm, color: colors.slate[700] }}>{connNode.label}</span>
                  <span style={{ fontSize: fontSize.xs, color: colors.slate[400], marginLeft: 'auto' }}>
                    {connNode.relevance}%
                  </span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Actions */}
        <Button
          variant="ghost"
          onClick={onDelete}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: spacing[2],
            color: colors.error.main,
            width: '100%',
            justifyContent: 'center',
          }}
        >
          <TrashIcon />
          <span>Delete Node</span>
        </Button>
      </CardContent>
    </Card>
  );
};

export const MemoryView: React.FC<MemoryViewProps> = ({
  nodes: initialNodes = demoNodes,
  onSelectNode,
  onDeleteNode,
  className,
  style,
}) => {
  const [nodes, setNodes] = useState<MemoryNode[]>(() => initializePositions(initialNodes));
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [isMobile, setIsMobile] = useState(false);

  const selectedNode = nodes.find((n) => n.id === selectedNodeId);

  useEffect(() => {
    const checkMobile = () => setIsMobile(window.innerWidth < breakpoints.md);
    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  const handleNodeClick = useCallback(
    (nodeId: string) => {
      setSelectedNodeId(nodeId);
      onSelectNode?.(nodeId);
    },
    [onSelectNode]
  );

  const handleNodeDrag = useCallback((nodeId: string, x: number, y: number) => {
    setNodes((prev) =>
      prev.map((node) => (node.id === nodeId ? { ...node, x, y } : node))
    );
  }, []);

  const handleDeleteNode = useCallback(() => {
    if (selectedNodeId) {
      onDeleteNode?.(selectedNodeId);
      setSelectedNodeId(null);
    }
  }, [selectedNodeId, onDeleteNode]);

  // Draw connections
  const renderConnections = () => {
    const lines: React.ReactElement[] = [];
    nodes.forEach((node) => {
      node.connections.forEach((connId) => {
        const connNode = nodes.find((n) => n.id === connId);
        if (connNode && node.id < connId) {
          lines.push(
            <line
              key={`${node.id}-${connId}`}
              x1={node.x || 0}
              y1={node.y || 0}
              x2={connNode.x || 0}
              y2={connNode.y || 0}
              stroke={colors.slate[300]}
              strokeWidth={2}
              strokeDasharray={selectedNodeId === node.id || selectedNodeId === connId ? '0' : '4'}
              opacity={selectedNodeId === node.id || selectedNodeId === connId ? 1 : 0.5}
            />
          );
        }
      });
    });
    return lines;
  };

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        height: '100%',
        backgroundColor: colors.slate[50],
        ...style,
      }}
    >
      {/* Graph Area */}
      <div
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          minWidth: 0,
        }}
      >
        {/* Header */}
        <div
          style={{
            padding: `${spacing[4]}px ${spacing[6]}px`,
            backgroundColor: '#fff',
            borderBottom: `1px solid ${colors.slate[200]}`,
            display: 'flex',
            alignItems: 'center',
            gap: spacing[3],
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
            <BrainIcon />
          </div>
          <div>
            <h1 style={{ fontSize: fontSize.xl, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
              Memory Graph
            </h1>
            <p style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>
              {nodes.length} nodes • Click to select, drag to move
            </p>
          </div>
        </div>

        {/* Graph Canvas */}
        <div
          style={{
            flex: 1,
            position: 'relative',
            overflow: 'hidden',
            backgroundColor: '#fff',
            margin: spacing[4],
            borderRadius: borderRadius.xl,
            boxShadow: shadows.sm,
          }}
        >
          <svg
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              width: '100%',
              height: '100%',
              pointerEvents: 'none',
            }}
          >
            {renderConnections()}
          </svg>
          {nodes.map((node) => (
            <NodeCircle
              key={node.id}
              node={node}
              isSelected={node.id === selectedNodeId}
              onClick={() => handleNodeClick(node.id)}
              onDrag={(x, y) => handleNodeDrag(node.id, x, y)}
            />
          ))}
        </div>
      </div>

      {/* Detail Panel */}
      {selectedNode && !isMobile && (
        <div style={{ width: 320, flexShrink: 0, padding: spacing[4], paddingLeft: 0 }}>
          <DetailPanel
            node={selectedNode}
            allNodes={nodes}
            onClose={() => setSelectedNodeId(null)}
            onDelete={handleDeleteNode}
          />
        </div>
      )}
    </div>
  );
};

export default MemoryView;
