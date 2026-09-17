import type {
  BacktestConfiguration,
  BacktestDetail,
  BacktestEquityCurve,
  BacktestMeasures,
  BacktestSummary,
  BacktestTradePage,
  ConnectionState,
  LimitKind,
  LimitState,
  Portfolio,
  PortfolioTrade,
  PortfolioTradePage,
  RiskReport,
  TradeDirection,
  TradeInput,
  TradeRefusal,
  TradeStatus,
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
  Signal,
  SignalAbsenceReason,
  SignalAction,
  SignalRankingPage,
  StrategyActionBand,
  StrategyDefinition,
  StrategyFactor,
  StrategyRunSummary,
  QualityFinding,
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

/**
 * The shape of `fetch` this module actually uses. `status` is optional because most reads only
 * need to know whether the request succeeded; the signal read is the exception — for it, "this
 * instrument has no recorded view" is an ordinary answer rather than a failure, and only the
 * status code distinguishes the two.
 */
export type Fetcher = (input: string, init?: RequestInit)
  => Promise<Pick<Response, 'ok' | 'json'> & { status?: number }>;

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
  // A strategy's recomputed views. The event names the instrument and the session range, never
  // the signals themselves: an open ranking re-reads them through the authorized path.
  'signals.changed.v1',
  // A completed backtest. The event carries the run and its configuration, never the result: an
  // open page re-reads that through the authorized path.
  'backtest.completed.v1',
  // A person's own portfolio. Scoped to its owner on the server, so a second person connected to
  // the same stream never receives it — the subscription here is the same either way.
  'portfolio.changed.v1',
  // A person's own limits. Scoped to its owner on the server, like the portfolio's own event.
  'risk_limits.changed.v1',
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


interface StrategyRefWire {
  name: string;
  version: number;
  title: string;
  caveat: string;
  superseded: boolean;
}

interface ContributionWire {
  factor: string;
  feature: string;
  feature_value: string | null;
  feature_session: string | null;
  factor_score: string | null;
  weight: string;
  contribution: string | null;
  unavailable_reason: string | null;
}

interface SignalWire {
  instrument_id: string;
  session_date: string;
  strategy: StrategyRefWire;
  score: string | null;
  action: SignalAction | null;
  confidence: string | null;
  absence_reason: SignalAbsenceReason | null;
  contributions: ContributionWire[];
  divisor: string | null;
  computed_at: string;
}

interface RankedSignalWire extends SignalWire {
  ticker: string;
  name: string;
  rank: number | null;
}

function toSignal(wire: SignalWire): Signal {
  return {
    instrumentId: wire.instrument_id,
    sessionDate: wire.session_date,
    strategy: { ...wire.strategy },
    score: wire.score ?? null,
    action: wire.action ?? null,
    confidence: wire.confidence ?? null,
    absenceReason: wire.absence_reason ?? null,
    divisor: wire.divisor ?? null,
    computedAt: wire.computed_at,
    contributions: (wire.contributions ?? []).map((contribution) => ({
      factor: contribution.factor,
      feature: contribution.feature,
      featureValue: contribution.feature_value ?? null,
      featureSession: contribution.feature_session ?? null,
      factorScore: contribution.factor_score ?? null,
      weight: contribution.weight,
      contribution: contribution.contribution ?? null,
      unavailableReason: contribution.unavailable_reason ?? null,
    })),
  };
}

/**
 * One instrument's signal as of a session.
 *
 * A 404 is returned as null rather than thrown: an instrument with no recorded view is an
 * ordinary state of the world — a newly listed company, a strategy that has not run — and the
 * instrument screen shows the rest of its page rather than an error.
 */
export async function fetchInstrumentSignal(
  id: string,
  options: { asOf?: string; strategy?: string; version?: number } = {},
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<Signal | null> {
  const query = new URLSearchParams();
  if (options.asOf) query.set('as_of', options.asOf);
  if (options.strategy) query.set('strategy', options.strategy);
  if (options.version) query.set('version', String(options.version));
  const suffix = query.toString() ? `?${query.toString()}` : '';
  const response = await fetcher(`/api/v1/instruments/${encodeURIComponent(id)}/signal${suffix}`, { signal });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error('Unable to load this instrument\'s signal.');
  return toSignal(await response.json() as SignalWire);
}

/** One page of the universe in a strategy's order. */
export async function fetchSignalRanking(
  options: { asOf?: string; strategy?: string; version?: number; cursor?: string; limit?: number } = {},
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<SignalRankingPage> {
  const query = new URLSearchParams();
  if (options.asOf) query.set('as_of', options.asOf);
  if (options.strategy) query.set('strategy', options.strategy);
  if (options.version) query.set('version', String(options.version));
  if (options.cursor) query.set('cursor', options.cursor);
  query.set('limit', String(options.limit ?? 50));
  const response = await fetcher(`/api/v1/signals?${query.toString()}`, { signal });
  if (!response.ok) throw new Error('Unable to load the strategy ranking.');
  const body = await response.json() as {
    items?: RankedSignalWire[];
    next_cursor?: string | null;
    total?: number | null;
    strategy?: StrategyRefWire;
    session_date?: string;
    scored?: number;
    unscored?: number;
  };
  if (!Array.isArray(body.items) || !body.strategy) throw new Error('Unable to load the strategy ranking.');
  return {
    items: body.items.map((item) => ({
      ...toSignal(item), ticker: item.ticker, name: item.name, rank: item.rank ?? null,
    })),
    nextCursor: body.next_cursor ?? null,
    total: body.total ?? null,
    strategy: { ...body.strategy },
    sessionDate: body.session_date ?? '',
    scored: body.scored ?? 0,
    unscored: body.unscored ?? 0,
  };
}

/** Every published strategy version, current and superseded. */
export async function fetchStrategies(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<StrategyDefinition[]> {
  const response = await fetcher('/api/v1/strategies', { signal });
  if (!response.ok) throw new Error('Unable to load the published strategies.');
  const body = await response.json() as { items?: (StrategyRefWire & {
    intent: string;
    factors: StrategyFactor[];
    action_bands: StrategyActionBand[];
    published_at: string;
    superseded_at: string | null;
  })[] };
  if (!Array.isArray(body.items)) throw new Error('Unable to load the published strategies.');
  return body.items.map((item) => ({
    name: item.name, version: item.version, title: item.title, caveat: item.caveat,
    superseded: item.superseded, intent: item.intent, factors: item.factors ?? [],
    actionBands: item.action_bands ?? [], publishedAt: item.published_at,
    supersededAt: item.superseded_at ?? null,
  }));
}

interface StrategyRunWire {
  id: string;
  kind: StrategyRunSummary['kind'];
  status: StrategyRunSummary['status'];
  started_at: string;
  finished_at: string | null;
  instrument_count: number;
  signal_count: number;
  failed_count: number;
  trigger_feature_run_id: string | null;
  app_version: string | null;
}

/** Recent strategy computations, newest first, for the operational screen. */
export async function fetchStrategyRuns(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<StrategyRunSummary[]> {
  const response = await fetcher('/api/v1/strategy-runs?limit=10', { signal });
  if (!response.ok) throw new Error('Unable to load recent strategy runs.');
  const body = await response.json() as { items?: StrategyRunWire[] };
  if (!Array.isArray(body.items)) throw new Error('Unable to load recent strategy runs.');
  return body.items.map((run) => ({
    id: run.id,
    kind: run.kind,
    status: run.status,
    startedAt: run.started_at,
    finishedAt: run.finished_at ?? null,
    instrumentCount: run.instrument_count,
    signalCount: run.signal_count,
    failedCount: run.failed_count,
    triggerFeatureRunId: run.trigger_feature_run_id ?? null,
    appVersion: run.app_version ?? null,
  }));
}

interface QualityFindingWire {
  id: string;
  instrument_id: string;
  session_date?: string | null;
  rule: string;
  severity: 'warning' | 'error';
  detail: string;
  status: string;
  reexamined_at?: string | null;
  awaiting_decision?: boolean;
  accepted_at?: string | null;
}

/** The findings that re-observation has already examined twice, and cannot settle. */
export async function fetchFindingsAwaitingDecision(
  fetcher: Fetcher = fetch, signal?: AbortSignal,
): Promise<QualityFinding[]> {
  const response = await fetcher('/api/v1/market-data/quality-findings?awaiting_decision=true&limit=100', { signal });
  if (!response.ok) throw new Error('Unable to load the findings awaiting a decision.');
  const body = await response.json() as { items?: QualityFindingWire[] };
  if (!Array.isArray(body.items)) throw new Error('Unable to load the findings awaiting a decision.');
  return body.items.map((finding) => ({
    id: finding.id,
    rule: finding.rule,
    status: finding.status,
    sessionDate: finding.session_date ?? null,
    detail: finding.detail,
    instrumentId: finding.instrument_id,
    severity: finding.severity,
    reexaminedAt: finding.reexamined_at ?? null,
    awaitingDecision: finding.awaiting_decision ?? false,
    acceptedAt: finding.accepted_at ?? null,
  }));
}

/**
 * Record that the owner judged a condition a limitation of the data.
 *
 * A mutation, so it carries the double-submit token the session cookie alone does not prove.
 */
export async function acceptFinding(
  findingID: string, csrfToken: string, fetcher: Fetcher = fetch,
): Promise<void> {
  const response = await fetcher(`/api/v1/market-data/quality-findings/${encodeURIComponent(findingID)}/accept`, {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
  });
  if (!response.ok) throw new Error('Unable to accept this finding.');
}

/* Backtesting (feature 021). Four reads and nothing that writes: running a backtest is an owner
 * action at the command line, so there is no client call that could make the product produce a
 * result. */

interface BacktestConfigurationWire {
  name: string;
  version: number;
  title: string;
  intent: string;
  caveat: string;
  strategy: { name: string; version: number };
  universe: string;
  from_session: string | null;
  to_session: string | null;
  starting_capital: string;
  accounting_currency: string;
  sizing: { rule: string; holdings: number };
  rebalance: { schedule: string };
  costs: {
    brokerage_bps: string;
    brokerage_minimum: string;
    slippage_bps: string;
    currency_spread_bps: string;
  };
}

interface BacktestMeasuresWire {
  from_session: string | null;
  to_session: string | null;
  total_return: string | null;
  annualised_return: string | null;
  volatility: string | null;
  maximum_drawdown: string | null;
  trade_count: number | null;
  total_costs: string | null;
  absence_reason: string | null;
}

interface BacktestSummaryWire {
  id: string;
  configuration: BacktestConfigurationWire;
  status: BacktestSummary['status'];
  from_session: string;
  to_session: string;
  started_at: string;
  finished_at: string | null;
  trade_count: number;
  skipped_count: number;
  rebalance_count: number;
  is_simulation: boolean;
}

interface BacktestDetailWire extends BacktestSummaryWire {
  measures: BacktestMeasuresWire;
  benchmarks: { mic: string; series: string; measures: BacktestMeasuresWire; absence_reason: string | null }[];
  skipped: { reason: string; count: number }[];
}

function toBacktestConfiguration(wire: BacktestConfigurationWire): BacktestConfiguration {
  return {
    name: wire.name, version: wire.version, title: wire.title, intent: wire.intent,
    caveat: wire.caveat, strategy: { ...wire.strategy }, universe: wire.universe,
    fromSession: wire.from_session ?? null, toSession: wire.to_session ?? null,
    startingCapital: wire.starting_capital, accountingCurrency: wire.accounting_currency,
    sizing: { ...wire.sizing }, rebalance: { ...wire.rebalance },
    costs: {
      brokerageBps: wire.costs.brokerage_bps,
      brokerageMinimum: wire.costs.brokerage_minimum,
      slippageBps: wire.costs.slippage_bps,
      currencySpreadBps: wire.costs.currency_spread_bps,
    },
  };
}

function toBacktestMeasures(wire: BacktestMeasuresWire): BacktestMeasures {
  return {
    fromSession: wire.from_session ?? null, toSession: wire.to_session ?? null,
    totalReturn: wire.total_return ?? null, annualisedReturn: wire.annualised_return ?? null,
    volatility: wire.volatility ?? null, maximumDrawdown: wire.maximum_drawdown ?? null,
    tradeCount: wire.trade_count ?? null, totalCosts: wire.total_costs ?? null,
    absenceReason: wire.absence_reason ?? null,
  };
}

function toBacktestSummary(wire: BacktestSummaryWire): BacktestSummary {
  return {
    id: wire.id, configuration: toBacktestConfiguration(wire.configuration), status: wire.status,
    fromSession: wire.from_session, toSession: wire.to_session, startedAt: wire.started_at,
    finishedAt: wire.finished_at ?? null, tradeCount: wire.trade_count,
    skippedCount: wire.skipped_count, rebalanceCount: wire.rebalance_count,
    isSimulation: wire.is_simulation !== false,
  };
}

/** Completed backtests, newest first. */
export async function fetchBacktests(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<BacktestSummary[]> {
  const response = await fetcher('/api/v1/backtests?limit=20', { signal });
  if (!response.ok) throw new Error('Unable to load the recorded backtests.');
  const body = await response.json() as { items?: BacktestSummaryWire[] };
  if (!Array.isArray(body.items)) throw new Error('Unable to load the recorded backtests.');
  return body.items.map(toBacktestSummary);
}

/** One backtest, its configuration, its measures and the benchmarks it is compared against. */
export async function fetchBacktest(
  id: string,
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<BacktestDetail | null> {
  const response = await fetcher(`/api/v1/backtests/${encodeURIComponent(id)}`, { signal });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error('Unable to load this backtest.');
  const wire = await response.json() as BacktestDetailWire;
  return {
    ...toBacktestSummary(wire),
    measures: toBacktestMeasures(wire.measures),
    benchmarks: (wire.benchmarks ?? []).map((item) => ({
      mic: item.mic, series: item.series, measures: toBacktestMeasures(item.measures),
      absenceReason: item.absence_reason ?? null,
    })),
    skipped: (wire.skipped ?? []).map((item) => ({ reason: item.reason, count: item.count })),
  };
}

/** One page of what the simulation did, and the signal behind each of them. */
export async function fetchBacktestTrades(
  id: string,
  options: { cursor?: string; limit?: number } = {},
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<BacktestTradePage> {
  const query = new URLSearchParams();
  if (options.cursor) query.set('cursor', options.cursor);
  query.set('limit', String(options.limit ?? 50));
  const response = await fetcher(`/api/v1/backtests/${encodeURIComponent(id)}/trades?${query.toString()}`, { signal });
  if (!response.ok) throw new Error('Unable to load this backtest\'s trades.');
  const body = await response.json() as {
    items?: {
      id: string; instrument_id: string; ticker: string; name: string; signal_session: string;
      execution_session: string; direction: 'buy' | 'sell'; quantity: string; price: string;
      currency: string; conversion_rate: string | null; brokerage: string; slippage: string;
      currency_spread: string; cash_effect: string; signal_id: string;
    }[];
    next_cursor?: string | null;
    total?: number | null;
  };
  if (!Array.isArray(body.items)) throw new Error('Unable to load this backtest\'s trades.');
  return {
    items: body.items.map((item) => ({
      id: item.id, instrumentId: item.instrument_id, ticker: item.ticker, name: item.name,
      signalSession: item.signal_session, executionSession: item.execution_session,
      direction: item.direction, quantity: item.quantity, price: item.price,
      currency: item.currency, conversionRate: item.conversion_rate ?? null,
      brokerage: item.brokerage, slippage: item.slippage, currencySpread: item.currency_spread,
      cashEffect: item.cash_effect, signalId: item.signal_id,
    })),
    nextCursor: body.next_cursor ?? null,
    total: body.total ?? null,
  };
}

/**
 * The equity curve as figures.
 *
 * The same information the chart draws, and the reason the chart is allowed to exist: a reader who
 * cannot see a canvas receives the result, not a poorer version of it.
 */
export async function fetchBacktestEquity(
  id: string,
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<BacktestEquityCurve> {
  const response = await fetcher(`/api/v1/backtests/${encodeURIComponent(id)}/equity`, { signal });
  if (!response.ok) throw new Error('Unable to load this backtest\'s equity curve.');
  const body = await response.json() as {
    currency?: string;
    items?: { session_date: string; cash: string; positions_value: string | null; total: string | null; absence_reason: string | null }[];
  };
  if (!Array.isArray(body.items)) throw new Error('Unable to load this backtest\'s equity curve.');
  return {
    currency: body.currency ?? '',
    items: body.items.map((item) => ({
      sessionDate: item.session_date, cash: item.cash,
      positionsValue: item.positions_value ?? null, total: item.total ?? null,
      absenceReason: item.absence_reason ?? null,
    })),
  };
}

/* Personal portfolio (feature 022). Every call is private to the authenticated caller: there is no
 * user identifier in any path, because there is no such thing as reading "the" portfolio. */

interface PortfolioWire {
  accounting_currency: string;
  holdings: {
    instrument_id: string; ticker: string; name: string; currency: string; quantity: string;
    cost: string; unrealised: string | null;
    valuation: { value: string | null; session: string | null; conversion_rate: string | null; absence_reason: string | null };
    comparison: {
      series: string; from_session: string | null; to_session: string | null;
      holding_return: string | null; benchmark_return: string | null; absence_reason: string | null;
    };
  }[];
  realised: {
    instrument_id: string; ticker: string; name: string; quantity: string;
    proceeds: string; cost: string; realised: string; cost_basis: string;
  }[];
  total: {
    value: string | null; cost: string; unrealised: string | null; realised: string;
    complete: boolean; incomplete_reason: string | null; return_absence: string;
  };
  records_what_you_entered: boolean;
}

interface PortfolioTradeWire {
  id: string; instrument_id: string; ticker: string; name: string;
  direction: TradeDirection; quantity: string; price: string; currency: string;
  costs: string; trade_date: string; sequence: number; status: TradeStatus;
  supersedes: string | null; recorded_at: string; changed_at: string | null;
}

function toPortfolioTrade(wire: PortfolioTradeWire): PortfolioTrade {
  return {
    id: wire.id, instrumentId: wire.instrument_id, ticker: wire.ticker, name: wire.name,
    direction: wire.direction, quantity: wire.quantity, price: wire.price,
    currency: wire.currency, costs: wire.costs, tradeDate: wire.trade_date,
    sequence: wire.sequence, status: wire.status, supersedes: wire.supersedes ?? null,
    recordedAt: wire.recorded_at, changedAt: wire.changed_at ?? null,
  };
}

function toPortfolio(wire: PortfolioWire): Portfolio {
  return {
    accountingCurrency: wire.accounting_currency,
    holdings: (wire.holdings ?? []).map((holding) => ({
      instrumentId: holding.instrument_id, ticker: holding.ticker, name: holding.name,
      currency: holding.currency, quantity: holding.quantity, cost: holding.cost,
      unrealised: holding.unrealised ?? null,
      valuation: {
        value: holding.valuation.value ?? null, session: holding.valuation.session ?? null,
        conversionRate: holding.valuation.conversion_rate ?? null,
        absenceReason: holding.valuation.absence_reason ?? null,
      },
      comparison: {
        series: holding.comparison.series,
        fromSession: holding.comparison.from_session ?? null,
        toSession: holding.comparison.to_session ?? null,
        holdingReturn: holding.comparison.holding_return ?? null,
        benchmarkReturn: holding.comparison.benchmark_return ?? null,
        absenceReason: holding.comparison.absence_reason ?? null,
      },
    })),
    realised: (wire.realised ?? []).map((item) => ({
      instrumentId: item.instrument_id, ticker: item.ticker, name: item.name,
      quantity: item.quantity, proceeds: item.proceeds, cost: item.cost,
      realised: item.realised, costBasis: item.cost_basis,
    })),
    totals: {
      value: wire.total.value ?? null, cost: wire.total.cost,
      unrealised: wire.total.unrealised ?? null, realised: wire.total.realised,
      complete: wire.total.complete, incompleteReason: wire.total.incomplete_reason ?? null,
      returnAbsence: wire.total.return_absence,
    },
    recordsWhatYouEntered: wire.records_what_you_entered !== false,
  };
}

/** What a person holds, what it is worth, and what it has made or lost. */
export async function fetchPortfolio(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<Portfolio> {
  const response = await fetcher('/api/v1/portfolio', { signal });
  if (!response.ok) throw new Error('Unable to load your portfolio.');
  return toPortfolio(await response.json() as PortfolioWire);
}

export async function setAccountingCurrency(
  currency: string,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<Portfolio> {
  const response = await fetcher('/api/v1/portfolio', {
    method: 'PUT', headers: writeHeaders(csrfToken),
    body: JSON.stringify({ accounting_currency: currency }),
  });
  if (!response.ok) throw await toTradeRefusal(response);
  return toPortfolio(await response.json() as PortfolioWire);
}

/** Every trade the person recorded, newest first. */
export async function fetchPortfolioTrades(
  options: { cursor?: string; limit?: number; includeWithdrawn?: boolean } = {},
  fetcher: Fetcher = fetch,
  signal?: AbortSignal,
): Promise<PortfolioTradePage> {
  const query = new URLSearchParams();
  if (options.cursor) query.set('cursor', options.cursor);
  if (options.includeWithdrawn) query.set('include_withdrawn', 'true');
  query.set('limit', String(options.limit ?? 50));
  const response = await fetcher(`/api/v1/portfolio/trades?${query.toString()}`, { signal });
  if (!response.ok) throw new Error('Unable to load your recorded trades.');
  const body = await response.json() as {
    items?: PortfolioTradeWire[]; next_cursor?: string | null; total?: number | null;
  };
  if (!Array.isArray(body.items)) throw new Error('Unable to load your recorded trades.');
  return {
    items: body.items.map(toPortfolioTrade),
    nextCursor: body.next_cursor ?? null,
    total: body.total ?? null,
  };
}

/** The token is passed in rather than read from anywhere global, matching how every other
 *  mutation in this client is written: a caller that has no session cannot accidentally send one. */
function writeHeaders(csrfToken: string): Record<string, string> {
  return { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken };
}

function tradeBody(input: TradeInput): string {
  return JSON.stringify({
    instrument_id: input.instrumentId, direction: input.direction, quantity: input.quantity,
    price: input.price, costs: input.costs, trade_date: input.tradeDate,
  });
}

export async function recordTrade(
  input: TradeInput,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<PortfolioTrade> {
  const response = await fetcher('/api/v1/portfolio/trades', {
    method: 'POST', headers: writeHeaders(csrfToken), body: tradeBody(input),
  });
  if (!response.ok) throw await toTradeRefusal(response);
  return toPortfolioTrade(await response.json() as PortfolioTradeWire);
}

/** A correction supersedes; the earlier version stays readable. */
export async function correctTrade(
  id: string,
  input: TradeInput,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<PortfolioTrade> {
  const response = await fetcher(`/api/v1/portfolio/trades/${encodeURIComponent(id)}`, {
    method: 'PATCH', headers: writeHeaders(csrfToken), body: tradeBody(input),
  });
  if (!response.ok) throw await toTradeRefusal(response);
  return toPortfolioTrade(await response.json() as PortfolioTradeWire);
}

export async function withdrawTrade(
  id: string,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<void> {
  const response = await fetcher(`/api/v1/portfolio/trades/${encodeURIComponent(id)}`, {
    method: 'DELETE', headers: writeHeaders(csrfToken),
  });
  if (!response.ok) throw await toTradeRefusal(response);
}

/**
 * A refusal carries what to do about it, so the interface can say "you hold 100" rather than
 * "something went wrong". Anything that is not a stated refusal becomes a plain error.
 */
export class PortfolioRefusalError extends Error {
  constructor(public readonly refusal: TradeRefusal) {
    super(refusal.message);
    this.name = 'PortfolioRefusalError';
  }
}

// The narrow shape this needs, rather than the whole Response type: the module's own Fetcher
// returns a subset, and demanding the full interface would make every caller fabricate headers it
// never reads.
async function toTradeRefusal(response: Pick<Response, 'json'>): Promise<Error> {
  try {
    const body = await response.json() as { error?: { code?: string; message?: string; held_quantity?: string } };
    if (body.error?.code && body.error.message) {
      return new PortfolioRefusalError({
        code: body.error.code, message: body.error.message,
        heldQuantity: body.error.held_quantity ?? null,
      });
    }
  } catch {
    // Falls through to the generic message below.
  }
  return new Error('That could not be recorded.');
}

/* Personal risk limits (feature 023). Private to the caller: no path carries a user identifier,
 * because there is no such thing as reading "the" limits. */

interface RiskReportWire {
  accounting_currency: string;
  limits: {
    kind: LimitKind; threshold: string; state: LimitState;
    measured: string | null; denominator: string | null; absence_reason: string | null;
    contributions: { label: string; value: string; share: string }[];
  }[];
  limits_are_your_own: boolean;
}

function toRiskReport(wire: RiskReportWire): RiskReport {
  return {
    accountingCurrency: wire.accounting_currency,
    limits: (wire.limits ?? []).map((limit) => ({
      kind: limit.kind, threshold: limit.threshold, state: limit.state,
      measured: limit.measured ?? null, denominator: limit.denominator ?? null,
      absenceReason: limit.absence_reason ?? null,
      contributions: (limit.contributions ?? []).map((contribution) => ({ ...contribution })),
    })),
    limitsAreYourOwn: wire.limits_are_your_own !== false,
  };
}

/** Every limit the person stated, and where they stand against each. */
export async function fetchRiskLimits(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<RiskReport> {
  const response = await fetcher('/api/v1/risk-limits', { signal });
  if (!response.ok) throw new Error('Unable to load your limits.');
  return toRiskReport(await response.json() as RiskReportWire);
}

/** State or change one limit. One of each kind, so this replaces rather than adds. */
export async function setRiskLimit(
  kind: LimitKind,
  threshold: string,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<RiskReport> {
  const response = await fetcher(`/api/v1/risk-limits/${encodeURIComponent(kind)}`, {
    method: 'PUT', headers: writeHeaders(csrfToken), body: JSON.stringify({ threshold }),
  });
  if (!response.ok) throw await toTradeRefusal(response);
  return toRiskReport(await response.json() as RiskReportWire);
}

export async function removeRiskLimit(
  kind: LimitKind,
  csrfToken: string,
  fetcher: Fetcher = fetch,
): Promise<RiskReport> {
  const response = await fetcher(`/api/v1/risk-limits/${encodeURIComponent(kind)}`, {
    method: 'DELETE', headers: writeHeaders(csrfToken),
  });
  if (!response.ok) throw await toTradeRefusal(response);
  return toRiskReport(await response.json() as RiskReportWire);
}
