import { get, post } from '../api';
import { API_ENDPOINTS } from '../api';
import type { Conversation, CallTrace } from '../types';

const getBase = () => API_ENDPOINTS.conversations;

export function getConversationTrace(sessionId: string, signal?: AbortSignal) {
  return get<CallTrace>(`${getBase()}/${sessionId}/trace`, undefined, signal);
}

export function stopConversation(sessionId: string, signal?: AbortSignal) {
  return post<{ stopped: boolean }>(`${getBase()}/${sessionId}/stop`, undefined, signal);
}

export function retryConversation(sessionId: string, signal?: AbortSignal) {
  return post<Conversation>(`${getBase()}/${sessionId}/retry`, undefined, signal);
}
