import type {
  ConnectionState,
  DailyBarSummary,
  FeatureRunSummary,
  ImportRunSummary,
  InstrumentDetail,
  InstrumentListingPage,
  InstrumentListingRow,
  InstrumentSummary,
  ListingQuery,
  SectorOption,
  HistoryWindow,
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

/** Wire shape of one listing row. Mirrors the contract exactly so the mapping stays honest. */
interface ListingRowWire {
  id: string;
  isin: string;
  ticker: string;
  name: string;
  exchange: { mic: string; name: string; timezone?: string };
  currency: string;
  country: string;
  sector: string;
  sector_name: string;
  industry: string;
  instrument_type: 'common_stock';
  status: 'active' | 'inactive';
  purchasability_status: InstrumentSummary['purchasabilityStatus'];
  latest_session: string | null;
  latest_close: string | null;
  change_absolute: string | null;
  change_percent: number | null;
  return_20: string | null;
  return_90: string | null;
  volatility: string | null;
  stored_sessions: number;
  freshness: { state: 'current' | 'stale' | 'no_history'; sessions_behind: number | null };
}

function listingRowFromWire(row: ListingRowWire): InstrumentListingRow {
  return {
    id: row.id,
    ticker: row.ticker,
    name: row.name,
    isin: row.isin,
    exchange: { mic: row.exchange.mic, name: row.exchange.name },
    sector: row.sector,
    sectorName: row.sector_name,
    industry: row.industry || null,
    country: row.country,
    currency: row.currency,
    status: row.status,
    latestSession: row.latest_session,
    latestClose: row.latest_close,
    changeAbsolute: row.change_absolute,
    // `?? null` and never `?? 0`: an absent statistic stays absent all the way to the screen.
    changePercent: row.change_percent ?? null,
    return20: row.return_20 ?? null,
    return90: row.return_90 ?? null,
    volatility: row.volatility ?? null,
    storedSessions: row.stored_sessions,
    freshness: {
      state: row.freshness.state,
      sessionsBehind: row.freshness.sessions_behind ?? null,
    },
  };
}

export function listingQueryString(query: ListingQuery): string {
  const params = new URLSearchParams();
  if (query.query) params.set('q', query.query);
  if (query.mic) params.set('mic', query.mic);
  if (query.country) params.set('country', query.country);
  if (query.currency) params.set('currency', query.currency);
  if (query.sector) params.set('sector', query.sector);
  if (query.status) params.set('status', query.status);
  if (query.sort) params.set('sort', query.sort);
  if (query.order) params.set('order', query.order);
  if (query.cursor) params.set('cursor', query.cursor);
  if (query.limit !== undefined) params.set('limit', String(query.limit));
  return params.toString();
}

/** The classification vocabulary the sector filter is rendered from. */
export async function fetchSectors(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<SectorOption[]> {
  const response = await fetcher('/api/v1/instruments/sectors', { signal });
  if (!response.ok) throw new Error('Unable to load sectors.');
  const body = await response.json() as {
    items?: { code: string; name: string; instrument_count: number }[];
  };
  if (!Array.isArray(body.items)) throw new Error('Unable to load sectors.');
  return body.items.map((sector) => ({
    code: sector.code, name: sector.name, instrumentCount: sector.instrument_count,
  }));
}

export async function fetchInstrumentListing(
  query: ListingQuery = {},
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<InstrumentListingPage> {
  const response = await fetcher(`/api/v1/instruments?${listingQueryString(query)}`, { signal });
  if (!response.ok) throw new Error('Unable to load instruments.');
  const body = await response.json() as {
    items?: ListingRowWire[];
    next_cursor?: string | null;
    total?: number | null;
  };
  if (!Array.isArray(body.items)) throw new Error('Unable to load instruments.');
  return {
    items: body.items.map(listingRowFromWire),
    nextCursor: body.next_cursor ?? null,
    // `?? null` and never `?? 0`: a page that carries no total says "unchanged", and reading
    // that as zero would tell the reader the result set had emptied underneath them.
    total: body.total ?? null,
  };
}

interface HistoryWindowWire {
  instrument: ListingRowWire;
  coverage: { first_session: string | null; last_session: string | null; stored_sessions: number };
  requested_from: string | null;
  requested_to: string | null;
  bars: Array<{
    session_date: string; open: string; high: string; low: string; close: string;
    adjusted_close: string | null; volume: number;
  }>;
  missing_sessions: string[];
  series_basis: 'raw' | 'provider_adjusted';
  provider: string | null;
  observed_at: string | null;
  actions: Array<{
    id: string; action_type: HistoryWindow['actions'][number]['actionType']; ex_date: string;
    ratio: string | null; amount: string | null; currency: string | null;
    old_symbol: string | null; new_symbol: string | null;
  }>;
  findings: Array<{
    id: string; rule: string; status: string; session_date: string | null; detail: string | null;
  }>;
}

export async function fetchInstrumentHistory(
  id: string,
  params: { sessions?: number; to?: string } = {},
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<HistoryWindow> {
  const query = new URLSearchParams();
  if (params.sessions !== undefined) query.set('sessions', String(params.sessions));
  if (params.to) query.set('to', params.to);
  const response = await fetcher(
    `/api/v1/instruments/${encodeURIComponent(id)}/history?${query.toString()}`, { signal });
  if (!response.ok) throw new Error('Unable to load instrument history.');
  const body = await response.json() as HistoryWindowWire;
  return {
    instrument: listingRowFromWire(body.instrument),
    coverage: {
      firstSession: body.coverage.first_session,
      lastSession: body.coverage.last_session,
      storedSessions: body.coverage.stored_sessions,
    },
    requestedFrom: body.requested_from,
    requestedTo: body.requested_to,
    bars: body.bars.map((bar) => ({
      sessionDate: bar.session_date,
      open: bar.open,
      high: bar.high,
      low: bar.low,
      close: bar.close,
      adjustedClose: bar.adjusted_close,
      volume: bar.volume,
    })),
    // Kept as dates rather than collapsed to a count: the chart has to interrupt the series
    // at exactly these sessions, and a number cannot say where.
    missingSessions: [...body.missing_sessions],
    seriesBasis: body.series_basis,
    provider: body.provider,
    observedAt: body.observed_at,
    actions: body.actions.map((action) => ({
      id: action.id,
      actionType: action.action_type,
      exDate: action.ex_date,
      ratio: action.ratio,
      amount: action.amount,
      currency: action.currency,
      oldSymbol: action.old_symbol,
      newSymbol: action.new_symbol,
    })),
    findings: body.findings.map((finding) => ({
      id: finding.id,
      rule: finding.rule,
      status: finding.status,
      sessionDate: finding.session_date,
      detail: finding.detail,
    })),
  };
}

export class InstrumentSearchClient {
  private controller?: AbortController;
  private sequence = 0;

  constructor(private readonly fetcher: Fetcher, private readonly onResult: (page: InstrumentListingPage) => void) {}

  async search(params: ListingQuery): Promise<void> {
    this.controller?.abort();
    const controller = new AbortController();
    this.controller = controller;
    const sequence = ++this.sequence;
    try {
      const page = await fetchInstrumentListing(params, this.fetcher, controller.signal);
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

interface FeatureRunWire {
  id: string;
  kind: FeatureRunSummary['kind'];
  status: FeatureRunSummary['status'];
  started_at: string;
  finished_at: string | null;
  instrument_count: number;
  value_count: number;
  failed_count: number;
  trigger_run_id: string | null;
  definition_name: string | null;
  app_version: string | null;
}

/** The engine's recent runs, newest first, for the operational screen. */
export async function fetchFeatureRuns(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<FeatureRunSummary[]> {
  const response = await fetcher('/api/v1/feature-runs?limit=10', { signal });
  if (!response.ok) throw new Error('Unable to load recent feature runs.');
  const body = await response.json() as { items?: FeatureRunWire[] };
  if (!Array.isArray(body.items)) throw new Error('Unable to load recent feature runs.');
  return body.items.map((run) => ({
    id: run.id,
    kind: run.kind,
    status: run.status,
    startedAt: run.started_at,
    finishedAt: run.finished_at ?? null,
    instrumentCount: run.instrument_count,
    valueCount: run.value_count,
    failedCount: run.failed_count,
    triggerRunId: run.trigger_run_id ?? null,
    definitionName: run.definition_name ?? null,
    appVersion: run.app_version ?? null,
  }));
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
  onRefresh: (entityType: string, entityId: string, payload: LiveEventPayload) => void;
  onState: (state: ConnectionState) => void;
  reconnectDelayMs: number;
  staleAfterMs: number;
}

/**
 * The market-data events this client applies.
 *
 * The server writes each one as a *named* SSE event ("event: daily_bar.changed.v1"), and a
 * browser EventSource routes a named event to a listener registered for that name — never to
 * the generic 'message' one. Subscribing only to 'message' therefore receives nothing at all,
 * which is a silent failure: the connection is open, the state says connected, and no update
 * ever arrives.
 */
export const MARKET_DATA_EVENT_TYPES = [
  'daily_bar.changed.v1',
  'import_run.changed.v1',
  'import_item.changed.v1',
  'quality_finding.changed.v1',
  'corporate_action.changed.v1',
  // The feature engine's own change: the three statistics on the Markets list are its values,
  // so the page hears about a recomputation the same way it hears about a new bar.
  'feature_values.changed.v1',
] as const;

/** What a market-data event says about the change it reports. */
export interface LiveEventPayload {
  entity_id?: string;
  entity_type?: string;
  instrument_id?: string;
  session_date?: string;
  ex_date?: string;
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
    // Both: the named types the server actually sends, and 'message' so an unnamed event
    // (or a future one) is still applied rather than dropped.
    for (const type of MARKET_DATA_EVENT_TYPES) {
      source.addEventListener(type, (event) => this.onMessage(event));
    }
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
      const data = JSON.parse(event.data) as LiveEventPayload;
      const entityType = data.entity_type ?? event.type.split('.changed.')[0];
      // The payload travels with the callback so a view can refresh only the row or window
      // the change concerns, instead of refetching everything and losing the person's place.
      if (entityType && data.entity_id) this.options.onRefresh(entityType, data.entity_id, data);
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
