// Core Components - explicit exports to avoid conflicts
export * from './Chat';
export * from './Graph';
export * from './Timeline';
export * from './MemoryIndicator';
export * from './MemoryDashboard';
export * from './ContextBuilder';
export * from './SemanticSearch';
export * from './NestedConversation';
export * from './AgentBoard';
export * from './DebateView';
export * from './PlanBoard';

// HotSwap - explicit to avoid ModelOption conflict
export {
  HotSwapDropdown,
  type HotSwapDropdownProps,
} from './HotSwap';

// ModelSelector - explicit to avoid conflicts
export {
  ModelSelector,
  ModelCard,
  ModelFilter,
  ModelSearch,
  type ModelSelectorProps,
} from './ModelSelector';

// New Components (P017-P031) - explicit exports to avoid conflicts
export {
  colors,
  spacing,
  borderRadius,
  fontSize,
  fontWeight,
  shadows,
  transitions,
  componentStyles,
} from './shared/tokens';

export {
  CustomSelect,
  type SelectOption,
  type CustomSelectProps,
} from './shared/CustomSelect';

export { Button, type ButtonProps } from './shared/Button';
export { Card, CardHeader, CardContent, CardFooter, type CardProps } from './shared/Card';
export { Badge, StatusBadge, type BadgeProps } from './shared/Badge';
export { Input, type InputProps } from './shared/Input';
export { Toggle, type ToggleProps } from './shared/Toggle';
export { Tooltip, type TooltipProps } from './shared/Tooltip';
export { Modal, type ModalProps } from './shared/Modal';
export { Tabs, TabPanel, type TabsProps, type TabItem } from './shared/Tabs';

export {
  CostIndicator,
  CostBreakdown as CostBreakdownComponent,
  CostChart,
  BudgetAlert,
  type CostData,
  type CostBreakdown,
  type CostIndicatorProps,
} from './CostIndicator';

export {
  PlanModePanel,
  type PlanModePanelProps,
  type PlanPhase,
  type PlanArtifact,
  type VisionData,
  type ConstraintItem,
} from './PlanModePanel';

export {
  ParallelPanel,
  type ParallelPanelProps,
  type ParallelTask,
  type ParallelWorker,
  type WorkerSolution,
} from './ParallelPanel';

export {
  PhaseModelConfigPanel,
  AgentModelConfigPanel,
  ParallelModelConfigPanel,
  ModelSettingsPage,
  type ModelInfo as ConfigModelInfo,
  type PhaseModelConfig,
  type AgentModelConfig,
  type ParallelWorkerConfig,
  type ModelPreset as ConfigModelPreset,
} from './ModelConfig';

// Layout Components (FE-020)
export {
  Sidebar,
  MobileNav,
  AppLayout,
  ViewMode,
  type NavItem,
  type SidebarProps,
  type MobileNavProps,
  type AppLayoutProps,
} from './Layout';

// Common Components (FE-030)
export {
  Avatar,
  AvatarStack,
  type AvatarProps,
  type AvatarStackProps,
  type AvatarSize,
} from './shared/Avatar';

export {
  ToastContainer,
  ToastItem,
  ToastProvider,
  useToast,
  type Toast,
  type ToastType,
  type ToastProps,
  type ToastContainerProps,
} from './shared/Toast';

export {
  ProgressBar,
  CircularProgress,
  type ProgressBarProps,
  type CircularProgressProps,
  type ProgressStatus,
} from './shared/ProgressBar';

// Views (FE-040)
export {
  HomeView,
  type HomeViewProps,
} from './views/HomeView';

export {
  SessionsView,
  type SessionsViewProps,
  type Session,
  type Message,
} from './views/SessionsView';

export {
  ProjectsView,
  type ProjectsViewProps,
  type Project,
  type ProjectStatus,
} from './views/ProjectsView';

export {
  MemoryView,
  type MemoryViewProps,
  type MemoryNode,
} from './views/MemoryView';

export {
  AgentsView,
  type AgentsViewProps,
  type AgentPreset,
} from './views/AgentsView';

export {
  PlanView,
  type PlanViewProps,
  type PlanStep,
  type PlanPhaseType,
  type PlanPhaseStatus,
  type RuntimeStatus,
} from './views/PlanView';

export {
  SettingsView,
  type SettingsViewProps,
  type UserProfile,
  type ConnectionStatus,
  type InterfacePreferences,
} from './views/SettingsView';
