import React, { useEffect, useMemo, useState } from 'react';
import {
  ArrowUpRight,
  Blocks,
  CheckCircle2,
  Clock,
  Download,
  ExternalLink,
  LayoutGrid,
  Plug,
  RefreshCw,
  Search,
  Settings,
  Shield,
  SlidersHorizontal,
  Sparkles,
  Store,
  Users,
  Wrench,
  XCircle,
} from 'lucide-react';
import { EmptyState } from './EmptyState';
import { ErrorState } from './ErrorState';
import { LoadingSkeleton } from './LoadingSkeleton';
import { useApi, useMutation } from '../hooks/useApi';
import {
  buildPluginOverview,
  getPluginPermissionTone,
  maskPluginConfigValue,
  normalizePluginManifest,
  toPluginViewModel,
  toPluginViewModels,
} from '../adapters/plugins';
import type { PluginViewModel } from '../adapters/plugins';
import { getGlobalConfig } from '../services/config';
import { getPlugin, installPlugin, listMarketplacePlugins, listPlugins, togglePlugin } from '../services/plugins';
import { ViewMode } from '../types';
import type {
  GlobalConfig,
  PluginDetailResponse,
  PluginManifest,
} from '../types';

const MetricCard = ({ label, value, hint, icon }: { label: string; value: string | number; hint: string; icon: React.ReactNode }) => (
  <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-4">
    <div className="flex items-start justify-between gap-3">
      <div>
        <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-400">{label}</p>
        <p className="mt-3 text-2xl font-bold text-slate-900">{value}</p>
        <p className="mt-1 text-xs text-slate-500">{hint}</p>
      </div>
      <div className="rounded-xl bg-white p-2.5 text-slate-500 shadow-sm">{icon}</div>
    </div>
  </div>
);

const SectionCard = ({
  title,
  subtitle,
  action,
  children,
}: {
  title: string;
  subtitle: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) => (
  <section className="rounded-[28px] border border-slate-200 bg-white shadow-sm overflow-hidden">
    <div className="flex items-center justify-between gap-4 border-b border-slate-100 px-5 py-4">
      <div>
        <h2 className="text-base font-semibold text-slate-900">{title}</h2>
        <p className="mt-1 text-xs text-slate-400">{subtitle}</p>
      </div>
      {action}
    </div>
    <div className="p-5">{children}</div>
  </section>
);

const PluginListItem = ({
  plugin,
  selected,
  onSelect,
  onToggle,
  busy,
}: {
  plugin: PluginViewModel;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
  busy: boolean;
}) => (
  <div
    role="button"
    tabIndex={0}
    onClick={onSelect}
    onKeyDown={(event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        onSelect();
      }
    }}
    className={`rounded-2xl border p-4 text-left transition-all focus:outline-none focus:ring-2 focus:ring-blue-500/20 ${selected ? 'border-blue-200 bg-blue-50/70 shadow-sm' : 'border-slate-200 bg-white hover:border-slate-300 hover:shadow-sm'}`}
  >
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-sm font-semibold text-slate-900">{plugin.displayName}</p>
          <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${plugin.healthTone}`}>
            {plugin.healthText}
          </span>
        </div>
        <p className="mt-2 text-sm text-slate-500">{plugin.summaryText}</p>
        <div className="mt-3 flex flex-wrap gap-2 text-[11px] text-slate-500">
          <span className="rounded-full bg-slate-100 px-2 py-1">{plugin.versionText}</span>
          <span className="rounded-full bg-slate-100 px-2 py-1">{plugin.scopeText}</span>
          <span className="rounded-full bg-slate-100 px-2 py-1">{plugin.sourceText}</span>
        </div>
      </div>
      <button
        type="button"
        disabled={busy}
        onClick={(event) => {
          event.stopPropagation();
          onToggle();
        }}
        className="rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold text-slate-600 hover:bg-slate-100 disabled:cursor-not-allowed disabled:opacity-50"
      >
        {busy ? 'Saving...' : plugin.toggleActionLabel}
      </button>
    </div>
  </div>
);

const MarketplaceCard = ({
  plugin,
  installed,
  busy,
  onPrimaryAction,
}: {
  plugin: PluginViewModel;
  installed: boolean;
  busy: boolean;
  onPrimaryAction: () => void;
}) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <div>
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-base font-semibold text-slate-900">{plugin.displayName}</p>
          {plugin.featured && <span className="rounded-full bg-blue-50 px-2 py-0.5 text-[10px] font-semibold text-blue-700">Featured</span>}
          {plugin.verified && <span className="rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">Verified</span>}
        </div>
        <p className="mt-2 text-sm text-slate-500">{plugin.summaryText}</p>
      </div>
      {plugin.healthText === 'healthy' ? <CheckCircle2 size={18} className="text-emerald-500" /> : <XCircle size={18} className="text-slate-300" />}
    </div>

    <div className="mt-4 flex flex-wrap gap-2 text-[11px] text-slate-500">
      <span className="rounded-full bg-slate-100 px-2 py-1">{plugin.scopeText}</span>
      <span className="rounded-full bg-slate-100 px-2 py-1">{plugin.versionText}</span>
      {plugin.categoryText && <span className="rounded-full bg-slate-100 px-2 py-1">{plugin.categoryText}</span>}
    </div>

    <div className="mt-5 flex items-center justify-between gap-3 border-t border-slate-100 pt-4 text-sm text-slate-500">
      <span>{plugin.installsText} installs</span>
      <div className="flex items-center gap-3">
        {plugin.docsUrl && (
          <a
            className="inline-flex items-center gap-1 font-medium text-slate-500 hover:text-blue-600"
            href={plugin.docsUrl}
            target="_blank"
            rel="noreferrer"
          >
            Learn more <ExternalLink size={14} />
          </a>
        )}
        <button
          onClick={onPrimaryAction}
          disabled={busy}
          className={`inline-flex items-center gap-2 rounded-xl px-3 py-2 font-semibold disabled:opacity-50 ${installed ? 'border border-slate-200 text-slate-600 hover:bg-slate-100' : 'bg-slate-900 text-white hover:bg-slate-800'}`}
        >
          {installed ? <ArrowUpRight size={14} /> : <Download size={14} />}
          {busy ? 'Working...' : installed ? 'Open' : 'Install'}
        </button>
      </div>
    </div>
  </div>
);

interface PluginsViewProps {
  showToast: (message: string) => void;
  onNavigate: (mode: ViewMode) => void;
}

export const PluginsView: React.FC<PluginsViewProps> = ({ showToast, onNavigate }) => {
  const [search, setSearch] = useState('');
  const [selectedPluginId, setSelectedPluginId] = useState<string | null>(null);
  const [mobileTab, setMobileTab] = useState<'workspace' | 'control'>('workspace');

  const {
    data: installedData,
    loading: installedLoading,
    error: installedError,
    refetch: refetchInstalled,
  } = useApi(
    (signal) => listPlugins({ search: search || undefined }, signal),
    [search],
  );

  const {
    data: marketplaceData,
    loading: marketplaceLoading,
    error: marketplaceError,
    refetch: refetchMarketplace,
  } = useApi(
    (signal) => listMarketplacePlugins({ search: search || undefined, limit: 12 }, signal),
    [search],
  );

  const {
    data: globalConfig,
    loading: globalConfigLoading,
    error: globalConfigError,
    refetch: refetchGlobalConfig,
  } = useApi<GlobalConfig>(
    (signal) => getGlobalConfig(signal),
    [],
  );

  const installedPlugins = useMemo(
    () => (installedData?.plugins ?? []).map(normalizePluginManifest),
    [installedData],
  );
  const installedPluginVMs = useMemo(() => toPluginViewModels(installedPlugins), [installedPlugins]);

  useEffect(() => {
    if (!selectedPluginId && installedPluginVMs.length > 0) {
      setSelectedPluginId(installedPluginVMs[0].id);
    }
  }, [installedPluginVMs, selectedPluginId]);

  const selectedFallbackId = selectedPluginId ?? installedPluginVMs[0]?.id ?? null;

  const {
    data: pluginDetail,
    loading: detailLoading,
    error: detailError,
    refetch: refetchDetail,
  } = useApi<PluginDetailResponse | null>(
    (signal) => selectedFallbackId ? getPlugin(selectedFallbackId, signal) : Promise.resolve(null),
    [selectedFallbackId],
    { enabled: !!selectedFallbackId },
  );

  const selectedPluginRaw = pluginDetail?.plugin
    ? normalizePluginManifest(pluginDetail.plugin)
    : installedPlugins.find((plugin) => plugin.id === selectedFallbackId) ?? null;
  const selectedPlugin = selectedPluginRaw ? toPluginViewModel(selectedPluginRaw) : null;

  const overview = buildPluginOverview(installedPlugins, installedData?.summary);
  const categoryRows = Object.entries(overview.categories ?? {}).sort((a, b) => b[1] - a[1]);
  const featured = useMemo(() => toPluginViewModels((marketplaceData?.featured ?? []).map(normalizePluginManifest)), [marketplaceData]);
  const marketplacePlugins = useMemo(() => toPluginViewModels((marketplaceData?.plugins ?? []).map(normalizePluginManifest)), [marketplaceData]);
  const installedIds = useMemo(() => new Set(installedPluginVMs.map((plugin) => plugin.id)), [installedPluginVMs]);

  const installMutation = useMutation<string, PluginDetailResponse>((pluginId, signal) => installPlugin(pluginId, signal));
  const toggleMutation = useMutation<{ pluginId: string; enabled: boolean }, PluginDetailResponse>(
    ({ pluginId, enabled }, signal) => togglePlugin(pluginId, enabled, signal),
  );

  const enabledChannels = (globalConfig?.api_pool ?? []).filter((channel) => channel.enabled).length;
  const primaryStrategy = globalConfig?.default_model || 'Not configured';
  const runtimeGuardrails = [
    globalConfig?.timeout ? `${globalConfig.timeout}ms timeout` : null,
    typeof globalConfig?.max_retries === 'number' ? `${globalConfig.max_retries} retries` : null,
    typeof globalConfig?.summary_threshold === 'number' ? `summary ${globalConfig.summary_threshold}` : null,
  ].filter(Boolean);

  const handleRefresh = () => {
    refetchInstalled();
    refetchMarketplace();
    refetchDetail();
    refetchGlobalConfig();
  };

  const handleInstall = async (pluginId: string) => {
    try {
      await installMutation.execute(pluginId);
      showToast('Plugin installed');
      setSelectedPluginId(pluginId);
      refetchInstalled();
      refetchMarketplace();
      refetchDetail();
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'Failed to install plugin');
    }
  };

  const handleToggle = async (plugin: PluginManifest) => {
    try {
      await toggleMutation.execute({ pluginId: plugin.id, enabled: !plugin.enabled });
      showToast(plugin.enabled ? 'Plugin disabled' : 'Plugin enabled');
      refetchInstalled();
      refetchDetail();
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'Failed to update plugin');
    }
  };

  const handleMarketplacePrimary = async (plugin: PluginViewModel) => {
    if (installedIds.has(plugin.id)) {
      setSelectedPluginId(plugin.id);
      return;
    }
    await handleInstall(plugin.id);
  };

  const selectedPermissions = selectedPlugin?.permissions ?? [];
  const selectedConfig = selectedPlugin?.config ?? [];

  return (
    <div className="flex-1 flex flex-col h-full bg-slate-50 relative overflow-hidden pb-16 md:pb-0">
      <header className="border-b border-slate-200 bg-white shrink-0">
        <div className="px-4 py-4 md:px-8 space-y-4">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="flex items-start gap-3">
              <div className="size-10 rounded-2xl bg-gradient-to-br from-blue-500 to-indigo-600 text-white flex items-center justify-center shadow-md shadow-blue-500/20">
                <LayoutGrid size={18} />
              </div>
              <div>
                <h1 className="text-xl md:text-2xl font-bold text-slate-900 tracking-tight">Plugin Center</h1>
                <p className="mt-1 text-sm text-slate-500">三段式工作台承载已安装插件、市场发现，以及权限 / 配置 / 全局策略快捷视图。</p>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-3">
              <div className="hidden md:flex relative group">
                <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 group-focus-within:text-blue-500 transition-colors" />
                <input
                  type="text"
                  placeholder="Search plugins..."
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  className="pl-9 pr-4 py-2 bg-slate-100 border-transparent focus:bg-white focus:border-blue-200 focus:ring-2 focus:ring-blue-500/10 rounded-xl text-sm w-72 transition-all"
                />
              </div>
              <button
                onClick={handleRefresh}
                className="inline-flex items-center gap-2 bg-slate-900 hover:bg-slate-800 text-white px-4 py-2 rounded-xl text-sm font-semibold shadow-lg shadow-slate-900/10 transition-transform active:scale-95"
              >
                <RefreshCw size={16} /> Refresh
              </button>
            </div>
          </div>

          <div className="grid grid-cols-2 xl:grid-cols-4 gap-3">
            <MetricCard label="Installed" value={overview.installed} hint={`${overview.enabled} enabled in current workspace`} icon={<Plug size={18} />} />
            <MetricCard label="Marketplace" value={marketplaceData?.total ?? overview.marketplace} hint="Curated plugins ready for review" icon={<Store size={18} />} />
            <MetricCard label="Healthy" value={Math.max(overview.installed - overview.unhealthy, 0)} hint={`${overview.unhealthy} need attention`} icon={<CheckCircle2 size={18} />} />
            <MetricCard label="Policy" value={(globalConfig?.public_mcp ?? []).length} hint="Public MCP channels exposed by strategy" icon={<Shield size={18} />} />
          </div>
        </div>
      </header>

      <div className="md:hidden flex border-b border-slate-200 bg-white shrink-0">
        <button
          className={`flex-1 py-3 text-sm font-bold ${mobileTab === 'workspace' ? 'text-blue-600 border-b-2 border-blue-500' : 'text-slate-500'}`}
          onClick={() => setMobileTab('workspace')}
        >
          Workspace
        </button>
        <button
          className={`flex-1 py-3 text-sm font-bold ${mobileTab === 'control' ? 'text-blue-600 border-b-2 border-blue-500' : 'text-slate-500'}`}
          onClick={() => setMobileTab('control')}
        >
          Control Rail
        </button>
      </div>

      <div className="flex-1 overflow-hidden">
        <div className="h-full flex flex-col xl:grid xl:grid-cols-[320px_minmax(0,1fr)_360px] xl:overflow-hidden">
          <aside className={`${mobileTab === 'control' ? 'hidden' : 'flex'} xl:flex flex-col border-b xl:border-b-0 xl:border-r border-slate-200 bg-white/80 backdrop-blur-sm overflow-y-auto`}>
            <div className="p-4 md:p-5 space-y-4">
              <div className="md:hidden relative group">
                <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 group-focus-within:text-blue-500 transition-colors" />
                <input
                  type="text"
                  placeholder="Search plugins..."
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  className="w-full pl-9 pr-4 py-3 bg-slate-100 border-transparent focus:bg-white focus:border-blue-200 focus:ring-2 focus:ring-blue-500/10 rounded-2xl text-sm transition-all"
                />
              </div>

              <SectionCard title="Workbench lanes" subtitle="Plugins owns the marketplace body. Settings and Agents stay as linked control surfaces.">
                <div className="grid gap-3">
                  <button
                    type="button"
                    onClick={() => onNavigate(ViewMode.SETTINGS)}
                    className="flex items-center justify-between rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-left hover:bg-slate-100"
                  >
                    <div className="flex items-center gap-3">
                      <Settings size={16} className="text-indigo-500" />
                      <div>
                        <p className="text-sm font-semibold text-slate-900">Settings</p>
                        <p className="text-xs text-slate-500">Global style and policy shortcuts remain there.</p>
                      </div>
                    </div>
                    <ArrowUpRight size={14} className="text-slate-400" />
                  </button>
                  <button
                    type="button"
                    onClick={() => onNavigate(ViewMode.AGENTS)}
                    className="flex items-center justify-between rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-left hover:bg-slate-100"
                  >
                    <div className="flex items-center gap-3">
                      <Users size={16} className="text-blue-500" />
                      <div>
                        <p className="text-sm font-semibold text-slate-900">Agents</p>
                        <p className="text-xs text-slate-500">Bindings stay visible there without moving the market body.</p>
                      </div>
                    </div>
                    <ArrowUpRight size={14} className="text-slate-400" />
                  </button>
                </div>
              </SectionCard>

              <SectionCard
                title="Category coverage"
                subtitle="Overview stays in the left rail so the main workspace can focus on discovery and operation."
                action={<span className="rounded-full bg-blue-50 px-3 py-1 text-[11px] font-semibold text-blue-600">/api/v1/plugins</span>}
              >
                {categoryRows.length === 0 ? (
                  <p className="text-sm text-slate-400">No category metrics available.</p>
                ) : (
                  <div className="space-y-3">
                    {categoryRows.map(([category, count]) => (
                      <div key={category}>
                        <div className="mb-1 flex items-center justify-between text-sm text-slate-500">
                          <span className="capitalize">{category}</span>
                          <span>{count}</span>
                        </div>
                        <div className="h-2 rounded-full bg-slate-200">
                          <div
                            className="h-2 rounded-full bg-gradient-to-r from-blue-500 to-indigo-500"
                            style={{ width: `${Math.min(100, Math.max(12, (count / Math.max(overview.total || 1, 1)) * 100))}%` }}
                          />
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </SectionCard>

              <SectionCard title="Installed registry" subtitle="Memory-style list selection drives the inspector and workspace focus.">
                {installedLoading ? (
                  <LoadingSkeleton variant="list" count={5} />
                ) : installedError ? (
                  <ErrorState message={installedError.message} onRetry={refetchInstalled} />
                ) : installedPluginVMs.length === 0 ? (
                  <EmptyState icon={<Plug size={40} />} title="No installed plugins" description="Install a marketplace item to start managing permissions and config." />
                ) : (
                  <div className="space-y-3">
                    {installedPluginVMs.map((plugin) => (
                      <PluginListItem
                        key={plugin.id}
                        plugin={plugin}
                        selected={plugin.id === selectedFallbackId}
                        onSelect={() => setSelectedPluginId(plugin.id)}
                        onToggle={() => {
                          const raw = installedPlugins.find((item) => item.id === plugin.id);
                          if (raw) {
                            void handleToggle(raw);
                          }
                        }}
                        busy={toggleMutation.loading}
                      />
                    ))}
                  </div>
                )}
              </SectionCard>
            </div>
          </aside>

          <main className={`${mobileTab === 'control' ? 'hidden' : 'block'} xl:block overflow-y-auto bg-slate-50`}>
            <div className="p-4 md:p-6 space-y-6">
              <SectionCard
                title="Selected plugin workspace"
                subtitle="Plan-style central workspace keeps the current installed plugin visible while you browse the catalog."
                action={selectedPlugin ? <span className={`rounded-full border px-3 py-1 text-[11px] font-semibold uppercase tracking-wide ${selectedPlugin.healthTone}`}>{selectedPlugin.healthText}</span> : undefined}
              >
                {detailLoading && selectedFallbackId ? (
                  <LoadingSkeleton variant="text" count={4} />
                ) : detailError ? (
                  <ErrorState message={detailError.message} onRetry={refetchDetail} />
                ) : !selectedPlugin || !selectedPluginRaw ? (
                  <EmptyState icon={<Plug size={40} />} title="Select a plugin" description="Choose an installed plugin from the left rail to open the controlled workspace." />
                ) : (
                  <div className="space-y-5">
                    <div className="rounded-[28px] border border-slate-200 bg-gradient-to-br from-slate-50 via-white to-blue-50/40 p-5">
                      <div className="flex flex-wrap items-start justify-between gap-4">
                        <div className="max-w-3xl">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="text-2xl font-bold text-slate-900">{selectedPlugin.displayName}</h3>
                            <span className="rounded-full bg-white px-2 py-1 text-[11px] font-semibold text-slate-500 border border-slate-200">{selectedPlugin.versionText}</span>
                            <span className="rounded-full bg-white px-2 py-1 text-[11px] font-semibold text-slate-500 border border-slate-200">{selectedPlugin.scopeText}</span>
                            <span className="rounded-full bg-white px-2 py-1 text-[11px] font-semibold text-slate-500 border border-slate-200">{selectedPlugin.sourceText}</span>
                          </div>
                          <p className="mt-3 text-sm text-slate-500">{selectedPlugin.descriptionText}</p>
                          <div className="mt-4 flex flex-wrap gap-2 text-[11px] text-slate-500">
                            {selectedPlugin.tags.map((tag) => (
                              <span key={tag} className="rounded-full border border-slate-200 bg-white px-2 py-1">{tag}</span>
                            ))}
                            {selectedPlugin.authorText && <span className="rounded-full border border-slate-200 bg-white px-2 py-1">{selectedPlugin.authorText}</span>}
                            {selectedPlugin.categoryText && <span className="rounded-full border border-slate-200 bg-white px-2 py-1">Category: {selectedPlugin.categoryText}</span>}
                          </div>
                        </div>
                        <div className="flex flex-col gap-2">
                          <button
                            onClick={() => handleToggle(selectedPluginRaw)}
                            disabled={toggleMutation.loading}
                            className="inline-flex items-center justify-center gap-2 rounded-xl bg-slate-900 px-4 py-2 text-sm font-semibold text-white hover:bg-slate-800 disabled:opacity-50"
                          >
                            <Wrench size={14} />
                            {toggleMutation.loading ? 'Saving...' : `${selectedPlugin.toggleActionLabel} plugin`}
                          </button>
                          {selectedPlugin.docsUrl && (
                            <a
                              className="inline-flex items-center justify-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-sm font-semibold text-slate-600 hover:bg-slate-100"
                              href={selectedPlugin.docsUrl}
                              target="_blank"
                              rel="noreferrer"
                            >
                              <ExternalLink size={14} /> Open docs
                            </a>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                      <MetricCard label="Permissions" value={selectedPermissions.length} hint="Declared capability grants" icon={<Shield size={18} />} />
                      <MetricCard label="Config keys" value={selectedConfig.length} hint="Visible fields in config surface" icon={<SlidersHorizontal size={18} />} />
                      <MetricCard label="Sessions" value={selectedPlugin.activeSessionsText} hint="Active runtime sessions" icon={<Blocks size={18} />} />
                      <MetricCard label="Updated" value={selectedPlugin.updatedAtText} hint="Latest manifest or install change" icon={<Clock size={18} />} />
                    </div>
                  </div>
                )}
              </SectionCard>

              <SectionCard title="Marketplace highlights" subtitle="Featured packages keep the discovery flow in the main workspace, not in Settings.">
                {marketplaceLoading ? (
                  <LoadingSkeleton variant="card" count={2} />
                ) : marketplaceError ? (
                  <ErrorState message={marketplaceError.message} onRetry={refetchMarketplace} />
                ) : featured.length === 0 ? (
                  <EmptyState icon={<Sparkles size={40} />} title="No featured plugins" description="Marketplace is reachable, but no featured records were returned." />
                ) : (
                  <div className="grid gap-4 lg:grid-cols-2">
                    {featured.slice(0, 2).map((plugin) => (
                      <MarketplaceCard
                        key={plugin.id}
                        plugin={plugin}
                        installed={installedIds.has(plugin.id)}
                        busy={installMutation.loading}
                        onPrimaryAction={() => handleMarketplacePrimary(plugin)}
                      />
                    ))}
                  </div>
                )}
              </SectionCard>

              <SectionCard
                title="Marketplace catalog"
                subtitle="Browse and install candidate plugins while keeping the current workspace state pinned above."
                action={<span className="rounded-full bg-slate-100 px-3 py-1 text-[11px] font-semibold text-slate-500">{marketplacePlugins.length} items</span>}
              >
                {marketplaceLoading ? (
                  <LoadingSkeleton variant="card" count={4} />
                ) : marketplaceError ? (
                  <ErrorState message={marketplaceError.message} onRetry={refetchMarketplace} />
                ) : marketplacePlugins.length === 0 ? (
                  <EmptyState icon={<Store size={40} />} title="Marketplace empty" description="No catalog entries were returned by the plugin API." />
                ) : (
                  <div className="grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
                    {marketplacePlugins.map((plugin) => (
                      <MarketplaceCard
                        key={plugin.id}
                        plugin={plugin}
                        installed={installedIds.has(plugin.id)}
                        busy={installMutation.loading}
                        onPrimaryAction={() => handleMarketplacePrimary(plugin)}
                      />
                    ))}
                  </div>
                )}
              </SectionCard>
            </div>
          </main>

          <aside className={`${mobileTab === 'workspace' ? 'hidden' : 'flex'} xl:flex flex-col border-t xl:border-t-0 xl:border-l border-slate-200 bg-white/90 backdrop-blur-sm overflow-y-auto`}>
            <div className="p-4 border-b border-slate-200 shrink-0">
              <h2 className="text-base font-semibold text-slate-900">Control rail</h2>
              <p className="mt-1 text-xs text-slate-400">Memory-style detail rail承接权限、配置和全局策略快照。</p>
            </div>

            <div className="p-4 space-y-4">
              <SectionCard title="Permission inspector" subtitle="Current grants and required surfaces for the selected plugin.">
                {detailLoading && selectedFallbackId ? (
                  <LoadingSkeleton variant="text" count={4} />
                ) : detailError ? (
                  <ErrorState message={detailError.message} onRetry={refetchDetail} />
                ) : !selectedPlugin ? (
                  <EmptyState icon={<Shield size={36} />} title="No plugin selected" description="Select an installed plugin to inspect permissions and config." />
                ) : selectedPermissions.length === 0 ? (
                  <p className="text-sm text-slate-400">No permissions declared.</p>
                ) : (
                  <div className="space-y-3">
                    {selectedPermissions.map((permission) => (
                      <div key={permission.id} className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <div className="flex items-start justify-between gap-3">
                          <div>
                            <p className="text-sm font-semibold text-slate-900">{permission.name}</p>
                            <p className="mt-1 text-sm text-slate-500">{permission.description || 'No description provided.'}</p>
                          </div>
                          <span className={`rounded-full border px-2 py-1 text-[11px] font-semibold ${getPluginPermissionTone(permission)}`}>
                            {permission.level || (permission.granted ? 'granted' : permission.required ? 'required' : 'optional')}
                          </span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </SectionCard>

              <SectionCard title="Config surface" subtitle="Settings-like cards keep the configuration preview compact and reviewable.">
                {detailLoading && selectedFallbackId ? (
                  <LoadingSkeleton variant="text" count={4} />
                ) : detailError ? (
                  <ErrorState message={detailError.message} onRetry={refetchDetail} />
                ) : !selectedPlugin ? (
                  <EmptyState icon={<SlidersHorizontal size={36} />} title="No config preview" description="Select an installed plugin to inspect its configuration surface." />
                ) : selectedConfig.length === 0 ? (
                  <p className="text-sm text-slate-400">No config fields exposed.</p>
                ) : (
                  <div className="space-y-3">
                    {selectedConfig.map((field) => (
                      <div key={field.key} className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <div className="flex items-start justify-between gap-3">
                          <div>
                            <p className="text-sm font-semibold text-slate-900">{field.label || field.key}</p>
                            <p className="mt-1 text-sm text-slate-500">{field.description || `Type: ${field.type || 'string'}`}</p>
                          </div>
                          <span className="rounded-full bg-white px-2 py-1 text-[11px] font-semibold text-slate-500 border border-slate-200">
                            {field.required ? 'required' : 'optional'}
                          </span>
                        </div>
                        <div className="mt-3 rounded-xl border border-slate-200 bg-white px-3 py-2 font-mono text-xs text-slate-600">
                          {maskPluginConfigValue(field)}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </SectionCard>

              <SectionCard title="Global strategy shortcuts" subtitle="Settings keeps the long-form surface; Plugin Center only exposes governed runtime and policy snapshots.">
                {globalConfigLoading ? (
                  <LoadingSkeleton variant="text" count={4} />
                ) : globalConfigError ? (
                  <div className="rounded-2xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700">
                    <p>Failed to load global strategy snapshot.</p>
                    <button onClick={refetchGlobalConfig} className="mt-3 inline-flex items-center gap-2 rounded-xl border border-rose-200 px-3 py-2 text-xs font-semibold text-rose-700 hover:bg-rose-100">
                      <RefreshCw size={12} /> Retry
                    </button>
                  </div>
                ) : !globalConfig ? (
                  <p className="text-sm text-slate-400">No global config snapshot available.</p>
                ) : (
                  <div className="space-y-3">
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-400">Default model</p>
                        <p className="mt-2 text-sm font-semibold text-slate-900 break-all">{primaryStrategy}</p>
                      </div>
                      <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-400">API channels</p>
                        <p className="mt-2 text-sm font-semibold text-slate-900">{enabledChannels} / {(globalConfig.api_pool ?? []).length} enabled</p>
                      </div>
                      <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-400">Public MCP</p>
                        <p className="mt-2 text-sm font-semibold text-slate-900">{(globalConfig.public_mcp ?? []).length} channels</p>
                      </div>
                      <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-400">Guardrails</p>
                        <p className="mt-2 text-sm font-semibold text-slate-900">{runtimeGuardrails.length > 0 ? runtimeGuardrails.join(' · ') : 'Using defaults'}</p>
                      </div>
                    </div>

                    <div className="grid gap-3 sm:grid-cols-2">
                      <button
                        type="button"
                        onClick={() => onNavigate(ViewMode.SETTINGS)}
                        className="inline-flex items-center justify-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm font-semibold text-slate-700 hover:bg-slate-100"
                      >
                        <Settings size={14} /> Open Settings
                      </button>
                      <button
                        type="button"
                        onClick={() => onNavigate(ViewMode.AGENTS)}
                        className="inline-flex items-center justify-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm font-semibold text-slate-700 hover:bg-slate-100"
                      >
                        <Users size={14} /> Open Agents
                      </button>
                    </div>
                  </div>
                )}
              </SectionCard>
            </div>
          </aside>
        </div>
      </div>
    </div>
  );
};
