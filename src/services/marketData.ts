import type { ConnectionState, ImportRunSummary } from '@/types/marketData';

export interface LiveEvent {
  lastEventId: string;
  type: string;
  data: string;
}

export interface LiveEventSource {
  addEventListener(type: string, listener: (event: LiveEvent) => void): void;
  close(): void;
}

export type Fetcher = (input: string, init?: RequestInit) => Promise<Pick<Response, 'ok' | 'json'>>;

interface ImportRunWire {
  id: string;
  kind: ImportRunSummary['kind'];
  provider: string;
  status: ImportRunSummary['status'];
  started_at: string;
  finished_at?: string | null;
  counts: ImportRunSummary['counts'];
  error_summary?: string | null;
  error?: { summary?: string } | null;
}

export async function fetchRecentImports(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<ImportRunSummary[]> {
  const response = await fetcher('/api/v1/market-data/imports?limit=20', { signal });
  if (!response.ok) throw new Error('Unable to load recent market-data imports.');
  const body = await response.json() as { items?: ImportRunWire[] };
  if (!Array.isArray(body.items)) throw new Error('Unable to load recent market-data imports.');
  return body.items.map((run) => ({
    id: run.id,
    kind: run.kind,
    provider: run.provider,
    status: run.status,
    startedAt: run.started_at,
    finishedAt: run.finished_at ?? null,
    counts: run.counts,
    errorSummary: run.error_summary ?? run.error?.summary ?? null,
  }));
}

interface LiveOptions {
  sourceFactory: (url: string, lastEventId: string) => LiveEventSource;
  onRefresh: (entityType: string, entityId: string) => void;
  onState: (state: ConnectionState) => void;
  reconnectDelayMs: number;
  staleAfterMs: number;
}

export class MarketDataLive {
  private source?: LiveEventSource;
  private reconnectTimer?: ReturnType<typeof setTimeout>;
  private staleTimer?: ReturnType<typeof setTimeout>;
  private running = false;
  private online = true;
  private lastEventId = '';
  private readonly seen = new Set<string>();

  constructor(private readonly options: LiveOptions) {}

  start(): void {
    if (this.running) return;
    this.running = true;
    if (this.online) this.connect();
    else this.options.onState('offline');
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
      this.options.onState('offline');
      return;
    }
    if (this.running && !this.source) {
      this.options.onState('reconnecting');
      this.connect();
    }
  }

  private connect(): void {
    if (!this.running || !this.online || this.source) return;
    const source = this.options.sourceFactory('/api/v1/events', this.lastEventId);
    this.source = source;
    source.addEventListener('open', () => {
      if (this.source !== source) return;
      if (this.staleTimer) clearTimeout(this.staleTimer);
      this.staleTimer = undefined;
      this.options.onState('connected');
    });
    source.addEventListener('message', (event) => this.onMessage(event));
    source.addEventListener('error', () => {
      if (this.source !== source) return;
      source.close();
      this.source = undefined;
      if (!this.running || !this.online) return;
      this.options.onState('reconnecting');
      if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = undefined;
        this.connect();
      }, this.options.reconnectDelayMs);
      if (this.staleTimer) clearTimeout(this.staleTimer);
      this.staleTimer = setTimeout(() => this.options.onState('stale'), this.options.staleAfterMs);
    });
  }

  private onMessage(event: LiveEvent): void {
    if (event.lastEventId) {
      if (this.seen.has(event.lastEventId)) return;
      this.seen.add(event.lastEventId);
      this.lastEventId = event.lastEventId;
      if (this.seen.size > 200) this.seen.delete(this.seen.values().next().value ?? '');
    }
    try {
      const data = JSON.parse(event.data) as { entity_type?: string; entity_id?: string };
      const entityType = data.entity_type ?? event.type.split('.changed.')[0];
      if (entityType && data.entity_id) this.options.onRefresh(entityType, data.entity_id);
    } catch {
      // Malformed invalidations are ignored; the stream remains usable.
    }
  }

  private clearTimers(): void {
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    if (this.staleTimer) clearTimeout(this.staleTimer);
    this.reconnectTimer = undefined;
    this.staleTimer = undefined;
  }
}
