import { get, post, patch } from '../api';
import { API_ENDPOINTS } from '../api';
import type { PluginCatalogResponse, PluginDetailResponse, PluginListResponse } from '../types';

const getBase = () => API_ENDPOINTS.plugins;

export interface PluginListParams {
  scope?: string;
  status?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

export function listPlugins(params?: PluginListParams, signal?: AbortSignal) {
  return get<PluginListResponse>(getBase(), params as Record<string, string | number | undefined>, signal);
}

export function getPlugin(pluginId: string, signal?: AbortSignal) {
  return get<PluginDetailResponse>(`${getBase()}/${pluginId}`, undefined, signal);
}

export function listMarketplacePlugins(params?: Pick<PluginListParams, 'search' | 'limit' | 'offset'>, signal?: AbortSignal) {
  return get<PluginCatalogResponse>(`${getBase()}/marketplace`, params as Record<string, string | number | undefined>, signal);
}

export function installPlugin(pluginId: string, signal?: AbortSignal) {
  return post<PluginDetailResponse>(`${getBase()}/${pluginId}/install`, undefined, signal);
}

export function togglePlugin(pluginId: string, enabled: boolean, signal?: AbortSignal) {
  return patch<PluginDetailResponse>(`${getBase()}/${pluginId}`, { enabled }, signal);
}
