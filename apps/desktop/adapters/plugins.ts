import type {
  PluginConfigField,
  PluginHealth,
  PluginManifest,
  PluginMetrics,
  PluginOverview,
  PluginPermission,
} from '../types';

export interface PluginViewModel {
  id: string;
  displayName: string;
  summaryText: string;
  descriptionText: string;
  versionText: string;
  scopeText: string;
  sourceText: string;
  categoryText?: string;
  healthText: string;
  healthTone: string;
  enabled: boolean;
  featured: boolean;
  verified: boolean;
  tags: string[];
  authorText?: string;
  permissions: PluginPermission[];
  config: PluginConfigField[];
  metrics: PluginMetrics;
  installsText: string;
  activeSessionsText: string;
  updatedAtText: string;
  docsUrl?: string;
  toggleActionLabel: string;
}

function formatCount(value?: number): string {
  return typeof value === 'number' ? value.toLocaleString() : '—';
}

export function formatPluginRelativeTime(timestamp?: number): string {
  if (!timestamp) return 'Unknown';
  const diff = Math.max(0, Math.floor(Date.now() / 1000) - timestamp);
  if (diff < 60) return 'Just now';
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

export function getPluginHealthTone(health?: PluginHealth | string): string {
  switch (health) {
    case 'healthy':
      return 'bg-emerald-50 text-emerald-700 border-emerald-200';
    case 'degraded':
      return 'bg-amber-50 text-amber-700 border-amber-200';
    case 'disabled':
      return 'bg-slate-100 text-slate-500 border-slate-200';
    default:
      return 'bg-slate-50 text-slate-500 border-slate-200';
  }
}

export function getPluginPermissionTone(permission: PluginPermission): string {
  if (permission.granted) {
    return 'bg-emerald-50 text-emerald-700 border-emerald-200';
  }
  if (permission.required) {
    return 'bg-rose-50 text-rose-700 border-rose-200';
  }
  return 'bg-slate-50 text-slate-500 border-slate-200';
}

export function maskPluginConfigValue(field: PluginConfigField): string {
  if (field.value == null || field.value === '') return 'Not configured';
  if (field.masked) return '••••••••';
  if (typeof field.value === 'boolean') return field.value ? 'true' : 'false';
  return String(field.value);
}

export function buildPluginOverview(
  plugins: PluginManifest[],
  summary?: PluginOverview,
): PluginOverview {
  if (summary) return summary;
  return {
    total: plugins.length,
    installed: plugins.filter((plugin) => plugin.installed).length,
    enabled: plugins.filter((plugin) => plugin.enabled).length,
    marketplace: plugins.filter((plugin) => plugin.source === 'marketplace').length,
    unhealthy: plugins.filter((plugin) => plugin.health && plugin.health !== 'healthy').length,
    categories: plugins.reduce<Record<string, number>>((acc, plugin) => {
      const key = plugin.category || 'uncategorized';
      acc[key] = (acc[key] ?? 0) + 1;
      return acc;
    }, {}),
  };
}

export function normalizePluginManifest(plugin: PluginManifest): PluginManifest {
  return {
    ...plugin,
    display_name: plugin.display_name || plugin.name,
    summary: plugin.summary || plugin.description || 'No summary available.',
    version: plugin.version || '0.0.0',
    source: plugin.source || (plugin.installed ? 'installed' : 'marketplace'),
    scope: plugin.scope || 'workspace',
    health: plugin.health || (plugin.enabled ? 'healthy' : 'disabled'),
    permissions: plugin.permissions ?? [],
    config: plugin.config ?? [],
    metrics: plugin.metrics ?? {},
    tags: plugin.tags ?? [],
  };
}

export function toPluginViewModel(plugin: PluginManifest): PluginViewModel {
  const normalized = normalizePluginManifest(plugin);
  const healthText = String(normalized.health || 'unknown');
  return {
    id: normalized.id,
    displayName: normalized.display_name || normalized.name,
    summaryText: normalized.summary || 'No summary available.',
    descriptionText: normalized.description || normalized.summary || 'No description available.',
    versionText: `v${normalized.version}`,
    scopeText: String(normalized.scope),
    sourceText: String(normalized.source),
    categoryText: normalized.category,
    healthText,
    healthTone: getPluginHealthTone(normalized.health),
    enabled: !!normalized.enabled,
    featured: !!normalized.featured,
    verified: !!normalized.verified,
    tags: normalized.tags ?? [],
    authorText: normalized.author ? `Author: ${normalized.author}` : undefined,
    permissions: normalized.permissions ?? [],
    config: normalized.config ?? [],
    metrics: normalized.metrics ?? {},
    installsText: formatCount(normalized.metrics?.downloads ?? normalized.metrics?.installs),
    activeSessionsText: formatCount(normalized.metrics?.active_sessions),
    updatedAtText: formatPluginRelativeTime(normalized.updated_at),
    docsUrl: normalized.homepage || normalized.repository,
    toggleActionLabel: normalized.enabled ? 'Disable' : 'Enable',
  };
}

export function toPluginViewModels(plugins: PluginManifest[]): PluginViewModel[] {
  return plugins.map(toPluginViewModel);
}
