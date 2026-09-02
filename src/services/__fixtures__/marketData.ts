import type {
  Bar,
  CorporateAction,
  Freshness,
  HistoryWindow,
  InstrumentListingRow,
  QualityFinding,
} from '@/types/marketData';

/**
 * One honest sample, shared by every component test.
 *
 * The point of a shared fixture here is not convenience — it is that each test would
 * otherwise invent its own tidy history, and a tidy history is exactly the one that hides
 * this feature's bugs. This sample has a gap in it, a statistic that cannot be computed, a
 * recorded split, and an open finding, so a component that quietly smooths over any of them
 * fails somewhere.
 */

/** Sessions the fixture exchange was open for. 2026-05-25 and 2026-05-26 are deliberately absent. */
const SESSIONS = [
  '2026-05-18', '2026-05-19', '2026-05-20', '2026-05-21', '2026-05-22',
  '2026-05-27', '2026-05-28', '2026-05-29', '2026-06-01', '2026-06-02',
];

/** The two open sessions with no stored bar. Not a weekend, not a holiday — missing data. */
export const MISSING_SESSIONS = ['2026-05-25', '2026-05-26'];

export function buildBars(sessions: string[] = SESSIONS): Bar[] {
  return sessions.map((sessionDate, index) => {
    const base = 100 + index;
    return {
      sessionDate,
      open: base.toFixed(2),
      high: (base + 1.5).toFixed(2),
      low: (base - 1.5).toFixed(2),
      close: (base + 0.5).toFixed(2),
      adjustedClose: null,
      volume: 1000 + index * 7,
    };
  });
}

export function buildFreshness(overrides: Partial<Freshness> = {}): Freshness {
  return { state: 'current', sessionsBehind: 0, ...overrides };
}

export function buildListingRow(
  overrides: Partial<InstrumentListingRow> = {},
): InstrumentListingRow {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    ticker: 'GAPPY',
    name: 'Interrupted History AB',
    isin: 'SE0000000200',
    exchange: { mic: 'XSTO', name: 'Nasdaq Stockholm' },
    sector: 'information_technology',
    sectorName: 'Information Technology',
    industry: 'Software',
    country: 'SE',
    currency: 'SEK',
    status: 'active',
    latestSession: '2026-06-02',
    latestClose: '109.50',
    changeAbsolute: '1.00',
    changePercent: 0.0092,
    return20: '0.041200000000',
    return90: '0.113300000000',
    volatility: '0.187500000000',
    storedSessions: SESSIONS.length,
    freshness: buildFreshness(),
    ...overrides,
  };
}

/**
 * An instrument with too few stored sessions to compute anything. Every derived statistic is
 * null — the state FR-007 forbids rendering as zero.
 */
export function buildRowWithTooFewSessions(): InstrumentListingRow {
  return buildListingRow({
    id: '33333333-3333-4333-8333-333333333333',
    ticker: 'SHORT',
    name: 'Barely Listed A/S',
    isin: 'DK0000000300',
    exchange: { mic: 'XCSE', name: 'Nasdaq Copenhagen' },
    currency: 'DKK',
    country: 'DK',
    return20: null,
    return90: null,
    volatility: null,
    storedSessions: 20,
  });
}

/** An instrument with no stored history at all, which is not the same as a stale one. */
export function buildRowWithNoHistory(): InstrumentListingRow {
  return buildListingRow({
    id: '44444444-4444-4444-8444-444444444444',
    ticker: 'EMPTY',
    name: 'No History A/S',
    isin: 'DK0000000400',
    exchange: { mic: 'XCSE', name: 'Nasdaq Copenhagen' },
    currency: 'DKK',
    country: 'DK',
    latestSession: null,
    latestClose: null,
    changeAbsolute: null,
    changePercent: null,
    return20: null,
    return90: null,
    volatility: null,
    storedSessions: 0,
    freshness: { state: 'no_history', sessionsBehind: null },
  });
}

export function buildAction(overrides: Partial<CorporateAction> = {}): CorporateAction {
  return {
    id: '55555555-5555-4555-8555-555555555555',
    actionType: 'split',
    exDate: '2026-05-28',
    ratio: '2',
    amount: null,
    currency: null,
    oldSymbol: null,
    newSymbol: null,
    ...overrides,
  };
}

export function buildFinding(overrides: Partial<QualityFinding> = {}): QualityFinding {
  return {
    id: '66666666-6666-4666-8666-666666666666',
    rule: 'suspicious_jump',
    status: 'open',
    sessionDate: '2026-05-29',
    detail: 'close moved more than the configured threshold',
    ...overrides,
  };
}

export function buildHistoryWindow(overrides: Partial<HistoryWindow> = {}): HistoryWindow {
  const bars = buildBars();
  return {
    instrument: buildListingRow(),
    coverage: {
      firstSession: bars[0].sessionDate,
      lastSession: bars[bars.length - 1].sessionDate,
      storedSessions: bars.length,
    },
    requestedFrom: bars[0].sessionDate,
    requestedTo: bars[bars.length - 1].sessionDate,
    bars,
    missingSessions: [...MISSING_SESSIONS],
    seriesBasis: 'raw',
    provider: 'fixture',
    observedAt: '2026-06-02T17:30:00Z',
    actions: [buildAction()],
    findings: [buildFinding()],
    ...overrides,
  };
}

/** A window with no interruptions, for asserting that a gap is what *causes* a break. */
export function buildContinuousHistoryWindow(): HistoryWindow {
  const sessions = SESSIONS.slice(0, 5);
  const bars = buildBars(sessions);
  return buildHistoryWindow({
    bars,
    missingSessions: [],
    coverage: {
      firstSession: sessions[0],
      lastSession: sessions[sessions.length - 1],
      storedSessions: sessions.length,
    },
    requestedFrom: sessions[0],
    requestedTo: sessions[sessions.length - 1],
    actions: [],
    findings: [],
  });
}

/** One engine run as the operational screen receives it. */
export function buildFeatureRun(overrides: Record<string, unknown> = {}) {
  return {
    id: 'eeeeeeee-0014-4000-8000-000000000001',
    kind: 'full' as const,
    status: 'succeeded' as const,
    startedAt: '2026-09-01T23:08:45Z',
    finishedAt: '2026-09-01T23:12:54Z',
    instrumentCount: 100,
    valueCount: 5830104,
    failedCount: 0,
    triggerRunId: null,
    definitionName: null,
    appVersion: '0.9.0',
    ...overrides,
  };
}
