import type {
  ConnectionState,
  DailyBarSummary,
  ImportRunSummary,
  InstrumentDetail,
  InstrumentPage,
  InstrumentSummary,
  PricePage,
} from '@/types/marketData';

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

export interface InstrumentSearchParams {
  query?: string;
  mic?: string;
  country?: string;
  currency?: string;
  active?: boolean;
  cursor?: string;
  limit?: number;
}

export interface PriceSearchParams {
  from?: string;
  to?: string;
  cursor?: string;
  limit?: number;
}

interface InstrumentWire {
  id: string;
  isin: string;
  ticker: string;
  name: string;
  exchange: { mic: string; name: string; timezone: string };
  currency: string;
  country: string;
  instrument_type: 'common_stock';
  active: boolean;
  purchasability_status: InstrumentSummary['purchasabilityStatus'];
}

interface DailyBarWire {
  session_date: string;
  open: string;
  high: string;
  low: string;
  close: string;
  adjusted_close?: string | null;
  volume: number;
  currency: string;
  provider: string;
  observed_at: string;
}

interface InstrumentDetailWire extends InstrumentWire {
  latest_bar?: DailyBarWire | null;
  history: { first_session?: string | null; last_session?: string | null; bar_count: number };
  quality_summary: { open_warnings: number; open_errors: number };
}

export async function fetchInstruments(params: InstrumentSearchParams = {}, fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<InstrumentPage> {
  const query = new URLSearchParams();
  if (params.query) query.set('q', params.query);
  if (params.mic) query.set('exchange', params.mic);
  if (params.country) query.set('country', params.country);
  if (params.currency) query.set('currency', params.currency);
  if (params.active !== undefined) query.set('active', String(params.active));
  if (params.cursor) query.set('cursor', params.cursor);
  if (params.limit !== undefined) query.set('limit', String(params.limit));
  const response = await fetcher(`/api/v1/instruments?${query.toString()}`, { signal });
  if (!response.ok) throw new Error('Unable to load instruments.');
  const body = await response.json() as { items?: InstrumentWire[]; next_cursor?: string | null };
  if (!Array.isArray(body.items)) throw new Error('Unable to load instruments.');
  return { items: body.items.map(instrumentFromWire), nextCursor: body.next_cursor ?? null };
}

export async function fetchInstrument(id: string, fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<InstrumentDetail> {
  const response = await fetcher(`/api/v1/instruments/${encodeURIComponent(id)}`, { signal });
  if (!response.ok) throw new Error('Unable to load instrument market data.');
  const body = await response.json() as InstrumentDetailWire;
  return {
    ...instrumentFromWire(body),
    latestBar: body.latest_bar ? barFromWire(body.latest_bar) : null,
    history: { firstSession: body.history.first_session ?? null, lastSession: body.history.last_session ?? null, barCount: body.history.bar_count },
    qualitySummary: { openWarnings: body.quality_summary.open_warnings, openErrors: body.quality_summary.open_errors },
  };
}

export async function fetchDailyPrices(id: string, params: PriceSearchParams = {}, fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<PricePage> {
  const query = new URLSearchParams();
  if (params.from) query.set('from', params.from);
  if (params.to) query.set('to', params.to);
  if (params.cursor) query.set('cursor', params.cursor);
  if (params.limit !== undefined) query.set('limit', String(params.limit));
  const response = await fetcher(`/api/v1/instruments/${encodeURIComponent(id)}/prices?${query.toString()}`, { signal });
  if (!response.ok) throw new Error('Unable to load instrument history.');
  const body = await response.json() as { items?: DailyBarWire[]; next_cursor?: string | null };
  if (!Array.isArray(body.items)) throw new Error('Unable to load instrument history.');
  return { items: body.items.map(barFromWire), nextCursor: body.next_cursor ?? null };
}

export class InstrumentSearchClient {
  private controller?: AbortController;
  private sequence = 0;

  constructor(private readonly fetcher: Fetcher, private readonly onResult: (page: InstrumentPage) => void) {}

  async search(params: InstrumentSearchParams): Promise<void> {
    this.controller?.abort();
    const controller = new AbortController();
    this.controller = controller;
    const sequence = ++this.sequence;
    try {
      const page = await fetchInstruments(params, this.fetcher, controller.signal);
      if (sequence === this.sequence && !controller.signal.aborted) this.onResult(page);
    } catch (error) {
      if (!controller.signal.aborted) throw error;
    }
  }

  cancel(): void {
    this.sequence += 1;
    this.controller?.abort();
    this.controller = undefined;
  }
}

function instrumentFromWire(item: InstrumentWire): InstrumentSummary {
  return { id: item.id, isin: item.isin, ticker: item.ticker, name: item.name, exchange: item.exchange,
    currency: item.currency, country: item.country, instrumentType: item.instrument_type, active: item.active,
    purchasabilityStatus: item.purchasability_status };
}

function barFromWire(bar: DailyBarWire): DailyBarSummary {
  return { sessionDate: bar.session_date, open: bar.open, high: bar.high, low: bar.low, close: bar.close,
    adjustedClose: bar.adjusted_close ?? null, volume: bar.volume, currency: bar.currency,
    provider: bar.provider, observedAt: bar.observed_at };
}

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
    errorSummary: safeImportErrorSummary(run.error_summary ?? run.error?.summary ?? null),
  }));
}

const publicImportErrorSummaries = new Set([
  'Market-data provider request failed.',
  'Market-data request was cancelled.',
  'Market-data provider request timed out.',
  'Market-data provider rate limit was reached.',
  'Market-data provider authentication failed.',
  'Market-data storage request failed.',
  'Market-data validation failed.',
  'Market-data import scope is already active.',
  'One instrument failed safely.',
]);

function safeImportErrorSummary(value: string | null): string | null {
  if (value === null) return null;
  return publicImportErrorSummaries.has(value) ? value : 'Market-data import failed.';
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
