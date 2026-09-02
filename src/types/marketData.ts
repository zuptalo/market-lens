export type ImportStatus = 'queued' | 'running' | 'succeeded' | 'partial' | 'failed' | 'cancelled';
export type ConnectionState = 'connected' | 'reconnecting' | 'stale' | 'offline';

export interface ImportCounts {
  processed: number;
  accepted: number;
  rejected: number;
  flagged: number;
  /**
   * Sessions this run replaced because the source had changed its answer since they were first
   * observed. Distinct from `accepted`, which counts everything stored: only a correction means
   * every value derived from that session moved underneath. Optional because a server that
   * predates feature 016 does not send it, and absent is not the same claim as zero.
   */
  revised?: number;
}

export interface ImportRunSummary {
  id: string;
  kind: 'universe_sync' | 'backfill' | 'daily_update' | 'retry';
  provider: string;
  status: ImportStatus;
  startedAt: string;
  finishedAt: string | null;
  counts: ImportCounts;
  errorSummary?: string | null;
}

/**
 * One run of the feature engine, as the operational screen reads it.
 *
 * `failedCount` is the field that matters most there. A partial run leaves the previous values
 * standing, which is correct and completely invisible on the market screens: the statistics
 * look current because they are the last ones that computed.
 */
export interface FeatureRunSummary {
  id: string;
  kind: 'full' | 'incremental' | 'definition';
  status: 'running' | 'succeeded' | 'partial' | 'failed';
  startedAt: string;
  finishedAt: string | null;
  instrumentCount: number;
  valueCount: number;
  failedCount: number;
  triggerRunId: string | null;
  definitionName: string | null;
  appVersion: string | null;
}

export interface ExchangeIdentity {
  mic: string;
  name: string;
  timezone: string;
}

export interface InstrumentSummary {
  id: string;
  isin: string;
  ticker: string;
  name: string;
  exchange: ExchangeIdentity;
  currency: string;
  country: string;
  instrumentType: 'common_stock';
  active: boolean;
  purchasabilityStatus: 'user_confirmed' | 'unverified' | 'unavailable';
}

export interface DailyBarSummary {
  sessionDate: string;
  open: string;
  high: string;
  low: string;
  close: string;
  adjustedClose: string | null;
  volume: number;
  currency: string;
  provider: string;
  observedAt: string;
}

export interface HistoryCoverage {
  firstSession: string | null;
  lastSession: string | null;
  barCount: number;
}

export interface QualitySummary {
  openWarnings: number;
  openErrors: number;
}

export interface InstrumentDetail extends InstrumentSummary {
  latestBar: DailyBarSummary | null;
  history: HistoryCoverage;
  qualitySummary: QualitySummary;
}

export interface InstrumentPage {
  items: InstrumentSummary[];
  nextCursor: string | null;
}

export interface PricePage {
  items: DailyBarSummary[];
  nextCursor: string | null;
}

/* --- Instrument exploration read model (feature 005) ---
 *
 * Every derived statistic below is `number | null`, never `number`. An absent statistic is a
 * fact — there were too few stored sessions to compute it — and `0` would be a different,
 * false claim (FR-007). Keeping the absence in the type stops a component rendering one as
 * the other by accident.
 */

export type FreshnessState = 'current' | 'stale' | 'no_history';

export interface Freshness {
  state: FreshnessState;
  /** Open exchange sessions since the latest stored bar; absent when there is no history. */
  sessionsBehind: number | null;
}

export interface InstrumentListingRow {
  id: string;
  ticker: string;
  name: string;
  isin: string;
  exchange: { mic: string; name: string };
  sector: string;
  sectorName: string;
  industry: string | null;
  country: string;
  currency: string;
  status: 'active' | 'inactive';
  latestSession: string | null;
  /** Decimal string in the listing currency — never a float, and never converted. */
  latestClose: string | null;
  changeAbsolute: string | null;
  changePercent: number | null;
  // The feature engine's own decimals, as strings: a JavaScript number cannot hold every
  // numeric(24,12) it can, and a statistic rounded on the way to the screen is no longer the
  // statistic the engine computed (feature 013, US5-2).
  return20: string | null;
  return90: string | null;
  volatility: string | null;
  /** Why a statistic is absent, rather than leaving a reader to guess. */
  storedSessions: number;
  freshness: Freshness;
}

export type ListingSort =
  | 'name' | 'ticker' | 'exchange' | 'sector' | 'country'
  | 'latest_close' | 'change_percent' | 'return_20' | 'return_90'
  | 'volatility' | 'freshness';

export interface ListingQuery {
  query?: string;
  mic?: string;
  country?: string;
  currency?: string;
  sector?: string;
  status?: 'active' | 'inactive';
  sort?: ListingSort;
  order?: 'asc' | 'desc';
  cursor?: string;
  limit?: number;
}

/** One choice the sector filter may offer, as the server defines it. */
export interface SectorOption {
  code: string;
  name: string;
  instrumentCount: number;
}

export interface InstrumentListingPage {
  items: InstrumentListingRow[];
  nextCursor: string | null;
  /**
   * How many instruments match the filter, ignoring the page size. Present on the first page
   * of a result set and null afterwards, where it means "unchanged" rather than "zero" — the
   * server counts only for a cursor-less request (research R-001).
   */
  total: number | null;
}

export type SeriesBasis = 'raw' | 'provider_adjusted';

export interface Bar {
  sessionDate: string;
  open: string;
  high: string;
  low: string;
  close: string;
  adjustedClose: string | null;
  volume: number;
}

export interface CorporateAction {
  id: string;
  actionType: 'split' | 'reverse_split' | 'dividend' | 'symbol_change' | 'delisting';
  exDate: string;
  ratio: string | null;
  amount: string | null;
  currency: string | null;
  oldSymbol: string | null;
  newSymbol: string | null;
}

export interface QualityFinding {
  id: string;
  rule: string;
  status: string;
  sessionDate: string | null;
  detail: string | null;
}

/** A corporate action or a quality finding, reduced to what the chart needs to mark it. */
export interface ChartAnnotation {
  sessionDate: string;
  kind: 'corporate_action' | 'quality_finding';
  label: string;
  detail: string;
}

export interface HistoryWindow {
  instrument: InstrumentListingRow;
  coverage: { firstSession: string | null; lastSession: string | null; storedSessions: number };
  requestedFrom: string | null;
  requestedTo: string | null;
  bars: Bar[];
  /**
   * Sessions the exchange was open for with no stored bar. A day the exchange was closed
   * never appears here, so the chart can interrupt the series at exactly these dates
   * without ever reporting a public holiday as missing data (FR-013).
   */
  missingSessions: string[];
  seriesBasis: SeriesBasis;
  provider: string | null;
  observedAt: string | null;
  actions: CorporateAction[];
  findings: QualityFinding[];
}

/** The optional columns one device has chosen to show. Never a server record (research R4). */
export interface ColumnPreference {
  columns: string[];
}

/**
 * A strategy's stated view, never advice.
 *
 * Every decimal arrives as a string because the server stores them as numeric(24,12); parsing
 * them into JavaScript numbers would round them at the boundary, and a score that renders
 * differently from the one that was recorded is a score nobody can check.
 */
export type SignalAction = 'BUY' | 'HOLD' | 'REDUCE' | 'SELL' | 'WATCH';

export type SignalAbsenceReason =
  | 'insufficient_history'
  | 'feature_unavailable'
  | 'composite_undefined'
  | 'liquidity_excluded';

/**
 * The version that produced a signal, carried on every response that shows one. The caveat is
 * part of it rather than fetched separately, so no surface can show a score without it.
 */
export interface StrategyRef {
  name: string;
  version: number;
  title: string;
  caveat: string;
  superseded: boolean;
}

/** One factor's part of a score, recorded so a reader can check the arithmetic. */
export interface SignalContribution {
  factor: string;
  feature: string;
  featureValue: string | null;
  featureSession: string | null;
  factorScore: string | null;
  weight: string;
  contribution: string | null;
  unavailableReason: string | null;
}

export interface Signal {
  instrumentId: string;
  sessionDate: string;
  strategy: StrategyRef;
  /** Null exactly when absenceReason is set: a signal is a view or a stated absence, never both. */
  score: string | null;
  action: SignalAction | null;
  confidence: string | null;
  absenceReason: SignalAbsenceReason | null;
  contributions: SignalContribution[];
  divisor: string | null;
  computedAt: string;
}

export interface RankedSignal extends Signal {
  ticker: string;
  name: string;
  /** Position among scored instruments. Null for one the strategy could not score. */
  rank: number | null;
}

export interface SignalRankingPage {
  items: RankedSignal[];
  nextCursor: string | null;
  /** Present on a cursor-less request, null afterwards, where it means "unchanged". */
  total: number | null;
  strategy: StrategyRef;
  sessionDate: string;
  scored: number;
  unscored: number;
}

export interface StrategyFactor {
  name: string;
  feature: string;
  mode: 'cross_sectional' | 'absolute';
  weight: string;
  description: string;
}

export interface StrategyActionBand {
  lower: string;
  upper: string;
  action: SignalAction;
}

export interface StrategyDefinition extends StrategyRef {
  intent: string;
  factors: StrategyFactor[];
  actionBands: StrategyActionBand[];
  publishedAt: string;
  supersededAt: string | null;
}

export interface StrategyRunSummary {
  id: string;
  kind: 'full' | 'incremental' | 'strategy';
  status: 'running' | 'succeeded' | 'partial' | 'failed';
  startedAt: string;
  finishedAt: string | null;
  instrumentCount: number;
  signalCount: number;
  failedCount: number;
  triggerFeatureRunId: string | null;
  appVersion: string | null;
}
