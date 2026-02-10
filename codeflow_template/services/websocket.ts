import { WS_ENDPOINTS } from '../api';

type EventHandler = (data: unknown) => void;

export interface WebSocketServiceOptions {
  url?: string;
  maxRetries?: number;
  heartbeatInterval?: number;
}

/**
 * WebSocket service with auto-reconnect, heartbeat, and event dispatch.
 */
export class WebSocketService {
  private ws: WebSocket | null = null;
  private url: string;
  private maxRetries: number;
  private heartbeatInterval: number;
  private retryCount = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private handlers = new Map<string, Set<EventHandler>>();
  private _connected = false;
  private _destroyed = false;

  constructor(options?: WebSocketServiceOptions) {
    this.url = options?.url ?? WS_ENDPOINTS.events;
    this.maxRetries = options?.maxRetries ?? 5;
    this.heartbeatInterval = options?.heartbeatInterval ?? 30000;
  }

  get connected(): boolean {
    return this._connected;
  }

  connect(): void {
    if (this._destroyed) return;
    this.cleanup();

    try {
      this.ws = new WebSocket(this.url);
    } catch {
      this.scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this._connected = true;
      this.retryCount = 0;
      this.startHeartbeat();
      this.emit('connected', null);
    };

    this.ws.onclose = () => {
      this._connected = false;
      this.stopHeartbeat();
      this.emit('disconnected', null);
      this.scheduleReconnect();
    };

    this.ws.onerror = () => {
      // onclose will fire after onerror
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        const type = data?.type ?? 'message';
        this.emit(type, data);
        this.emit('message', data);
      } catch {
        this.emit('message', event.data);
      }
    };
  }

  on(event: string, handler: EventHandler): () => void {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, new Set());
    }
    this.handlers.get(event)!.add(handler);
    return () => this.off(event, handler);
  }

  off(event: string, handler: EventHandler): void {
    this.handlers.get(event)?.delete(handler);
  }

  send(data: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(typeof data === 'string' ? data : JSON.stringify(data));
    }
  }

  destroy(): void {
    this._destroyed = true;
    this.cleanup();
    this.handlers.clear();
  }

  private emit(event: string, data: unknown): void {
    this.handlers.get(event)?.forEach(h => {
      try { h(data); } catch { /* ignore handler errors */ }
    });
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      this.send({ type: 'ping' });
    }, this.heartbeatInterval);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  private scheduleReconnect(): void {
    if (this._destroyed || this.retryCount >= this.maxRetries) return;
    const delay = Math.min(1000 * Math.pow(2, this.retryCount), 16000);
    this.retryCount++;
    this.retryTimer = setTimeout(() => this.connect(), delay);
  }

  private cleanup(): void {
    this.stopHeartbeat();
    if (this.retryTimer) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.onmessage = null;
      if (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING) {
        this.ws.close();
      }
      this.ws = null;
    }
  }
}

// Singleton instance
let defaultInstance: WebSocketService | null = null;

export function getWebSocketService(): WebSocketService {
  if (!defaultInstance) {
    defaultInstance = new WebSocketService();
  }
  return defaultInstance;
}
