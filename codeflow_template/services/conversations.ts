import { get, post } from '../api';
import { API_ENDPOINTS } from '../api';
import type { Conversation, CallTrace } from '../types';

const BASE = API_ENDPOINTS.conversations;

export function getConversationTrace(sessionId: string, signal?: AbortSignal) {
  return get<CallTrace>(`${BASE}/${sessionId}/trace`, undefined, signal);
}

export function stopConversation(sessionId: string, signal?: AbortSignal) {
  return post<{ stopped: boolean }>(`${BASE}/${sessionId}/stop`, undefined, signal);
}

export function retryConversation(sessionId: string, signal?: AbortSignal) {
  return post<Conversation>(`${BASE}/${sessionId}/retry`, undefined, signal);
}
