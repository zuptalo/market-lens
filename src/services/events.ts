import type { ConnectionState } from '@/types/marketData';

export interface AuthInvalidation {
  id: string;
  type: string;
  entityType: string;
  entityId: string;
}

export interface AuthEventCallbacks {
  onInvalidate(event: AuthInvalidation): void;
  onState(state: ConnectionState): void;
  onUnauthorized(): void;
}

export interface StreamEvent {
  lastEventId: string;
  type: string;
  data: string;
  status?: number;
}

export interface EventSourceLike {
  addEventListener(type: string, listener: (event: StreamEvent) => void): void;
  close(): void;
}

export interface StreamAudience {
  userId: string;
  role: 'owner' | 'member';
}

interface AuthorizedEventOptions {
  sourceFactory?: (url: string) => EventSourceLike;
  role?: 'owner' | 'member';
  reconnectDelayMs?: number;
  staleAfterMs?: number;
}

export class AuthorizedEventStream {
  private callbacks: AuthEventCallbacks = { onInvalidate: () => {}, onState: () => {}, onUnauthorized: () => {} };
  private source?: EventSourceLike;
  private reconnectTimer?: ReturnType<typeof setTimeout>;
  private staleTimer?: ReturnType<typeof setTimeout>;
  private readonly seen = new Set<string>();
  private lastEventID = '';
  private running = false;
  private online = true;
  private role?: 'owner' | 'member';
  private userId?: string;

  constructor(private readonly options: AuthorizedEventOptions) { this.role = options.role; }
  configure(callbacks: AuthEventCallbacks): void { this.callbacks = callbacks; }
  // The server scopes replay already. Knowing which account this stream belongs to lets the
  // client refuse anything addressed elsewhere, so a server-side mistake cannot invalidate or
  // reveal the wrong person's snapshot.
  setAudience(audience: StreamAudience): void {
    this.userId = audience.userId;
    this.role = audience.role;
  }

  start(): void {
    if (this.running) return;
    this.running = true;
    if (this.online) this.connect();
    else this.callbacks.onState('offline');
  }

  stop(): void {
    this.running = false;
    this.source?.close();
    this.source = undefined;
    this.clearTimers();
  }

  setOnline(online: boolean): void {
    this.online = online;
    if (!online) {
      this.source?.close();
      this.source = undefined;
      this.clearTimers();
      this.callbacks.onState('offline');
      return;
    }
    if (this.running && !this.source) {
      this.callbacks.onState('reconnecting');
      this.connect();
    }
  }

  private connect(): void {
    if (!this.running || !this.online || this.source) return;
    const query = this.lastEventID ? `?last_event_id=${encodeURIComponent(this.lastEventID)}` : '';
    const source = (this.options.sourceFactory ?? browserEventSource)(`/api/v1/events${query}`);
    this.source = source;
    source.addEventListener('open', () => {
      if (this.source !== source) return;
      if (this.staleTimer) clearTimeout(this.staleTimer);
      this.staleTimer = undefined;
      this.callbacks.onState('connected');
    });
    source.addEventListener('message', (event) => this.onMessage(event));
    for (const type of authorizedEventTypes) {
      source.addEventListener(type, (event) => this.onMessage(event));
    }
    source.addEventListener('error', (event) => {
      if (this.source !== source) return;
      source.close();
      this.source = undefined;
      if (event.status === 401 || event.status === 403) {
        this.running = false;
        this.clearTimers();
        this.callbacks.onUnauthorized();
        return;
      }
      if (!this.running || !this.online) return;
      this.callbacks.onState('reconnecting');
      if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = undefined;
        this.connect();
      }, this.options.reconnectDelayMs ?? 1_000);
      if (this.staleTimer) clearTimeout(this.staleTimer);
      this.staleTimer = setTimeout(() => this.callbacks.onState('stale'), this.options.staleAfterMs ?? 10_000);
    });
  }

  private onMessage(event: StreamEvent): void {
    if (!event.lastEventId || this.seen.has(event.lastEventId)) return;
    try {
      const data = JSON.parse(event.data) as {
        scope?: unknown; entity_type?: unknown; entity_id?: unknown; subject_user_id?: unknown;
      };
      if (!validScope(data.scope) || typeof data.entity_type !== 'string' || typeof data.entity_id !== 'string') return;
      if (this.role === 'member' && data.scope === 'owner') return;
      // A private event is only ever this account's. Anything else, including an event with no
      // subject at all, is dropped rather than trusted.
      if (data.scope === 'user' && this.userId !== undefined && data.subject_user_id !== this.userId) return;
      this.seen.add(event.lastEventId);
      this.lastEventID = event.lastEventId;
      if (this.seen.size > 200) this.seen.delete(this.seen.values().next().value ?? '');
      this.callbacks.onInvalidate({ id: event.lastEventId, type: event.type, entityType: data.entity_type, entityId: data.entity_id });
    } catch {
      // Malformed or unauthorized envelopes never invalidate a client snapshot.
    }
  }

  private clearTimers(): void {
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    if (this.staleTimer) clearTimeout(this.staleTimer);
    this.reconnectTimer = undefined;
    this.staleTimer = undefined;
  }
}

const authorizedEventTypes = [
  'account.changed.v1', 'setup.changed.v1', 'credential.changed.v1', 'credential.key_rotated.v1',
  'owner.password_reset.v1',
  'session.created.v1', 'session.revoked.v1', 'sessions.revoked.v1',
  'daily_bar.changed.v1', 'import_item.changed.v1', 'import_run.changed.v1', 'quality_finding.changed.v1',
  'corporate_action.changed.v1',
] as const;

function validScope(value: unknown): value is 'shared' | 'user' | 'owner' {
  return value === 'shared' || value === 'user' || value === 'owner';
}

function browserEventSource(url: string): EventSourceLike {
  const source = new EventSource(url);
  return {
    addEventListener(type, listener) {
      source.addEventListener(type, (event) => listener({
        lastEventId: event instanceof MessageEvent ? event.lastEventId : '',
        type: event.type,
        data: event instanceof MessageEvent ? String(event.data) : '',
        // The browser hides the response status of a stream, but it only closes one for good
        // when the server refused it. A dropped connection stays in CONNECTING and retries,
        // so a closed stream is the signal that authorization is gone.
        status: event.type === 'error' && source.readyState === EventSource.CLOSED ? 401 : undefined,
      }));
    },
    close: () => source.close(),
  };
}
