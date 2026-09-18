/**
 * Shape-correct answers for every endpoint the interface reads.
 *
 * One copy, used by two things: the mobile-layout guard, and the local browser harness a person
 * runs to look at a screen at phone width. Two copies would eventually disagree, and then the
 * guard would be passing against a page nobody can reproduce by looking.
 *
 * Plain JavaScript on purpose: the harness is run directly by node, outside the TypeScript build.
 */
const OWNER = '10000000-0000-4000-8000-000000000001';
const ABB = '22000000-0000-4000-8000-000000000001';
const VOLVO = '22000000-0000-4000-8000-000000000002';

const instrument = (id, ticker, name, mic, mname, currency, country) => ({
  id, isin: 'CH0012221716', ticker, name,
  exchange: { mic, name: mname, timezone: 'Europe/Stockholm' },
  currency, country, sector: 'industrials', sector_name: 'Industrials',
  industry: 'Electrical Equipment', instrument_type: 'common_stock', status: 'active',
  purchasability_status: 'user_confirmed', active: true,
  latest_session: '2026-09-17', latest_close: '681.400000000000',
  change_absolute: '3.200000000000', change_percent: 0.0047,
  return_20: '0.004873913965', return_90: '-0.019640272896', volatility: '0.184320000000',
  stored_sessions: 2513, freshness: { state: 'current', sessions_behind: 0 },
});

const bars = (n) => Array.from({ length: n }, (_, k) => {
  const day = new Date(Date.UTC(2026, 8, 17) - (n - 1 - k) * 86400000);
  const close = 600 + Math.sin(k / 9) * 60 + k * 0.05;
  return {
    session_date: day.toISOString().slice(0, 10),
    open: close.toFixed(12), high: (close + 4).toFixed(12), low: (close - 4).toFixed(12),
    close: close.toFixed(12), adjusted_close: close.toFixed(12),
    volume: 900000 + k * 13, currency: 'SEK', provider: 'eodhd',
    observed_at: '2026-09-17T18:00:00Z',
  };
});

const STRATEGY = { name: 'momentum_trend', version: 1, title: 'Momentum and trend',
  caveat: 'This strategy exists to prove the platform can hold a view reproducibly and explain it. Its weights are stated rather than fitted, it has not been tested against historical outcomes, and nothing it produces is advice or a prediction.',
  superseded: false };

const contribution = (factor, feature, value, score, weight, amount) => ({
  factor, feature, feature_value: value, feature_session: '2026-09-17',
  factor_score: score, weight, contribution: amount, unavailable_reason: null,
});

const signal = {
  instrument_id: ABB, session_date: '2026-09-17', strategy: STRATEGY,
  score: '-0.120000000000', action: 'hold', confidence: '0.630000000000',
  absence_reason: null, divisor: '1.000000000000', computed_at: '2026-09-17T18:09:00Z',
  contributions: [
    contribution('momentum_90', 'return_90', '-0.019640272896', '-0.516000000000', '0.250000000000', '-0.129000000000'),
    contribution('momentum_20', 'return_20', '0.004873913965', '-0.263000000000', '0.150000000000', '-0.039000000000'),
    contribution('trend', 'trend_50_200', '0.094679994939', '0.631000000000', '0.200000000000', '0.126000000000'),
    contribution('volatility', 'volatility_60', '0.184320000000', '-0.204000000000', '0.200000000000', '-0.041000000000'),
    contribution('liquidity', 'turnover_20', '612400000.000000000000', '0.402000000000', '0.200000000000', '0.080000000000'),
  ],
};

const measures = (total, annual, vol, drawdown, trades, costs, absence = null) => ({
  from_session: '2016-09-30', to_session: '2026-06-30',
  total_return: total, annualised_return: annual, volatility: vol, maximum_drawdown: drawdown,
  trade_count: trades, total_costs: costs, absence_reason: absence,
});

const configuration = {
  name: 'momentum_trend_equal_10', version: 1, title: 'Momentum and trend, ten equal holdings',
  intent: 'Measure the stated strategy against the markets it trades in.',
  caveat: 'A simulation over stored prices. Nothing here was traded and nothing here is advice.',
  strategy: { name: 'momentum_trend', version: 1 }, universe: 'nordic_large_cap',
  from_session: '2016-09-30', to_session: '2026-06-30',
  starting_capital: '1000000.000000000000', accounting_currency: 'EUR',
  sizing: { rule: 'equal_weight', holdings: 10 }, rebalance: { schedule: 'monthly' },
  costs: { brokerage_bps: '10', brokerage_minimum: '5', slippage_bps: '5',
    currency_spread_bps: '10' },
};

const backtest = {
  id: 'bbbbbbbb-0021-4000-8000-000000000001', configuration, status: 'succeeded',
  from_session: '2016-09-30', to_session: '2026-06-30',
  started_at: '2026-09-17T19:02:00Z', finished_at: '2026-09-17T19:04:12Z',
  trade_count: 1284, skipped_count: 48, rebalance_count: 122, is_simulation: true,
  measures: measures('1.199700000000', '0.081700000000', '0.171200000000', '-0.342000000000',
    1284, '31000.000000000000'),
  benchmarks: [
    { mic: 'XOSL', series: 'OBX.INDX', absence_reason: null,
      measures: measures('2.715400000000', '0.139600000000', '0.192400000000', '-0.401000000000',
        null, null) },
    { mic: 'XSTO', series: 'OMXS30.INDX', absence_reason: null,
      measures: measures('1.292200000000', '0.087400000000', '0.164300000000', '-0.318000000000',
        null, null) },
    { mic: 'XHEL', series: 'OMXH25.INDX', absence_reason: null,
      measures: measures('0.884300000000', '0.065800000000', '0.158900000000', '-0.296000000000',
        null, null) },
  ],
  skipped: [{ reason: 'no_signal', count: 41 }, { reason: 'no_price', count: 7 }],
};

const routes = new Map(Object.entries({
  '/api/v1/health': { service: 'market-lens', status: 'ok', version: '0.21.1' },
  '/api/v1/ready': { status: 'ready' },
  '/api/v1/setup/status': { setup_complete: true },
  '/api/v1/account': { id: OWNER, email: 'kamran.alipour@gmail.com', display_name: 'Kamran',
    role: 'owner', status: 'active', email_verified_at: '2026-08-30T08:00:00Z' },
  '/api/v1/account/sessions': { items: [
    { id: 's1', current: true,
      device_label: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.0 Mobile/15E148 Safari/604.1',
      created_at: '2026-09-18T06:02:00Z', last_seen_at: '2026-09-18T07:41:06Z',
      idle_expires_at: '2026-09-18T15:41:06Z', absolute_expires_at: '2026-09-25T06:02:00Z',
      revoked: false },
    { id: 's2', current: false,
      device_label: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
      created_at: '2026-09-10T09:14:00Z', last_seen_at: '2026-09-17T22:18:41Z',
      idle_expires_at: '2026-09-18T06:18:41Z', absolute_expires_at: '2026-09-17T09:14:00Z',
      revoked: false },
  ] },
  '/api/v1/owner/members': { next_cursor: '', members: [
    { id: OWNER, email: 'kamran.alipour@gmail.com', display_name: 'Kamran', status: 'active',
      login_state: 'available', blocked_until: null, locked_at: null,
      active_session_count: 2, created_at: '2026-08-30T08:00:00Z' },
  ] },
  '/api/v1/owner/invitations': { items: [] },
  '/api/v1/owner/integrations': { settings: { smtp: { host: 'smtp.example.com', port: 587,
    from: 'market-lens@example.com', username: 'market-lens', password_set: true },
    eodhd: { api_key_set: true } } },
  '/api/v1/instruments/sectors': { items: [{ code: 'industrials', name: 'Industrials' }] },
  '/api/v1/strategies': { items: [{ name: 'momentum_trend', version: 1, title: 'Momentum and trend',
    caveat: STRATEGY.caveat, superseded: false,
    factors: signal.contributions.map((c) => ({ factor: c.factor, feature: c.feature, weight: c.weight })) }] },
  '/api/v1/risk-limits': { accounting_currency: 'SEK', limits_are_your_own: true, limits: [
    { kind: 'instrument_share', threshold: '0.250000000000', state: 'exceeded',
      measured: '0.412300000000', denominator: '284000.000000000000', absence_reason: null,
      contributions: [{ label: 'VOLV-B', value: '117093.200000000000', share: '0.412300000000' },
                      { label: 'ABB', value: '166906.800000000000', share: '0.587700000000' }] },
    { kind: 'holding_count', threshold: '12', state: 'within', measured: '2',
      denominator: null, absence_reason: null, contributions: [] },
  ] },
  '/api/v1/notifications/preferences': {
    preferences: [
      { kind: 'decision_waiting', channel: 'email', enabled: true },
      { kind: 'decision_waiting', channel: 'web_push', enabled: false },
      { kind: 'paper_fill', channel: 'email', enabled: false },
      { kind: 'paper_fill', channel: 'web_push', enabled: true },
      { kind: 'pipeline_failure', channel: 'email', enabled: false },
      { kind: 'pipeline_failure', channel: 'web_push', enabled: false },
      { kind: 'signal_change', channel: 'email', enabled: false },
      { kind: 'signal_change', channel: 'web_push', enabled: false },
    ],
    quiet_hours: { starts_at: '22:00', ends_at: '07:00', timezone: 'Europe/Stockholm' },
    nothing_is_on_by_default: true,
  },
  '/api/v1/notifications/subscriptions': { subscriptions: [
    // A digest no browser in a test will match, so the screen reports this device as uncovered —
    // which is the state the fix exists for.
    { id: 'sub1', label: 'iPhone, added 2026-09-01', endpoint_digest: 'anotherdevices0',
      created_at: '2026-09-01T08:00:00Z', last_used_at: '2026-09-18T07:00:00Z' },
  ] },
  '/api/v1/notifications/history': { notifications: [
    { kind: 'decision_waiting', channel: 'email', state: 'sent', count: 3, attempts: 1,
      last_error: null, created_at: '2026-09-18T06:00:00Z', sent_at: '2026-09-18T06:00:04Z' },
  ] },
  // Shaped like a key and deliberately not one: no test here subscribes a real browser, so real
  // key material in a fixture would be a high-entropy string that means nothing.
  '/api/v1/notifications/push-key': { public_key: 'a-public-key-in-base64url' },
  '/api/v1/paper-account': { starting_cash: '1000000.000000000000', accounting_currency: 'SEK',
    opened_at: '2026-06-01T08:00:00Z',
    costs: { brokerage_bps: '10', brokerage_minimum: '5', slippage_bps: '5',
      currency_spread_bps: '10' },
    cash: '727440.000000000000',
    holdings: [{ instrument_id: ABB, ticker: 'ABB', name: 'ABB Ltd', currency: 'SEK',
      quantity: '400.000000000000', cost: '240120.000000000000',
      value: '272560.000000000000', unrealised: '32440.000000000000', session: '2026-09-17',
      absence_reason: null,
      comparison: { series: 'OMXS30.INDX', holding_return: '0.135600000000',
        benchmark_return: '0.092100000000', absence_reason: null } }],
    orders: [
      { id: 'po1', intent_id: 'i1', instrument_id: ABB, ticker: 'ABB', name: 'ABB Ltd',
        currency: 'SEK', direction: 'buy', quantity: '400.000000000000',
        expected_price: '600.000000000000', placed_session: '2026-09-16', state: 'filled',
        absence_reason: null, placed_at: '2026-09-16T18:00:00Z',
        settled_at: '2026-09-17T06:00:00Z',
        fill: { fill_session: '2026-09-17', open_price: '600.300000000000',
          quantity: '400.000000000000', costs: '252.120000000000',
          cash_effect: '-240372.120000000000', conversion_rate: '1.000000000000',
          bar_diverged: false, filled_at: '2026-09-17T06:00:00Z' } },
      { id: 'po2', intent_id: 'i3', instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo B',
        currency: 'SEK', direction: 'buy', quantity: '9000.000000000000',
        expected_price: '284.100000000000', placed_session: '2026-09-17', state: 'unfillable',
        absence_reason: 'insufficient_cash', placed_at: '2026-09-17T18:00:00Z',
        settled_at: '2026-09-18T06:00:00Z', fill: null },
      { id: 'po3', intent_id: 'i4', instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo B',
        currency: 'SEK', direction: 'buy', quantity: '20.000000000000',
        expected_price: '284.100000000000', placed_session: '2026-09-18', state: 'pending',
        absence_reason: null, placed_at: '2026-09-18T07:00:00Z', settled_at: null, fill: null },
    ],
    total: { value: '272560.000000000000', cost: '240120.000000000000',
      unrealised: '32440.000000000000', realised: '2410.000000000000',
      total_return: '0.043210000000', complete: true, incomplete_reason: null },
    is_a_simulation: true },
  '/api/v1/order-intents': { evaluated_independently: true, records_what_you_are_considering: true,
    intents: [
      { id: 'i1', instrument_id: ABB, ticker: 'ABB', name: 'ABB Ltd', currency: 'SEK',
        direction: 'buy', quantity: '120.000000000000', price: '681.400000000000',
        costs: '0.000000000000', status: 'considering', recorded_at: '2026-09-18T06:12:00Z',
        settled_at: null,
        consequence: { resulting_quantity: '520.000000000000', resulting_value: '354328.000000000000',
          resulting_share: '0.412300000000', denominator: '859900.000000000000',
          absence_reason: null,
          limits: [{ kind: 'instrument_share', threshold: '0.250000000000', state: 'exceeded',
            measured: '0.412300000000', denominator: '859900.000000000000', absence_reason: null,
            contributions: [] }] } },
      { id: 'i2', instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo B', currency: 'SEK',
        direction: 'sell', quantity: '50.000000000000', price: '284.100000000000',
        costs: '0.000000000000', status: 'acted_on', recorded_at: '2026-09-16T08:00:00Z',
        settled_at: '2026-09-17T09:30:00Z', consequence: null },
    ] },
  '/api/v1/portfolio': { accounting_currency: 'SEK', records_what_you_entered: true,
    holdings: [
      { instrument_id: ABB, ticker: 'ABB', name: 'ABB Ltd', currency: 'SEK',
        quantity: '400.000000000000', cost: '240120.000000000000',
        unrealised: '32440.000000000000',
        valuation: { value: '272560.000000000000', session: '2026-09-17',
          conversion_rate: '1.000000000000', absence_reason: null },
        comparison: { series: 'OMXS30.INDX', from_session: '2025-11-28',
          to_session: '2026-09-17', holding_return: '0.135600000000',
          benchmark_return: '0.092100000000', absence_reason: null } },
      { instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo B', currency: 'SEK',
        quantity: '300.000000000000', cost: '78104.000000000000',
        unrealised: '7126.000000000000',
        valuation: { value: '85230.000000000000', session: '2026-09-17',
          conversion_rate: '1.000000000000', absence_reason: null },
        comparison: { series: 'OMXS30.INDX', from_session: '2026-02-14',
          to_session: '2026-09-17', holding_return: '0.092700000000',
          benchmark_return: '0.092100000000', absence_reason: null } },
    ],
    realised: [{ instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo B',
      quantity: '100.000000000000', proceeds: '28410.000000000000',
      cost: '26000.000000000000', realised: '2410.000000000000', cost_basis: 'fifo' }],
    total: { value: '357790.000000000000', cost: '318224.000000000000',
      unrealised: '39566.000000000000', realised: '2410.000000000000',
      complete: true, incomplete_reason: null, return_absence: 'cash_is_not_tracked' } },
  '/api/v1/portfolio/trades': { next_cursor: null, total: 2, items: [
    { id: 't1', instrument_id: ABB, ticker: 'ABB', name: 'ABB Ltd', direction: 'buy',
      quantity: '400.000000000000', price: '600.000000000000', currency: 'SEK',
      costs: '120.000000000000', trade_date: '2025-11-28', sequence: 1, status: 'live',
      supersedes: null, recorded_at: '2025-11-28T10:04:00Z', changed_at: null },
    { id: 't2', instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo B', direction: 'buy',
      quantity: '400.000000000000', price: '260.000000000000', currency: 'SEK',
      costs: '104.000000000000', trade_date: '2026-02-14', sequence: 2, status: 'live',
      supersedes: null, recorded_at: '2026-02-14T09:31:00Z', changed_at: null },
  ] },
  '/api/v1/backtests': { items: [backtest] },
  '/api/v1/feature-runs': { items: [{ id: 'f1', status: 'succeeded', kind: 'scheduled',
    started_at: '2026-09-17T18:05:00Z', finished_at: '2026-09-17T18:07:02Z',
    instruments: 96, failed: 0, values: 622080 }] },
  '/api/v1/strategy-runs': { items: [{ id: 'r1', strategy: 'momentum_trend', version: 1,
    status: 'succeeded', started_at: '2026-09-17T18:08:00Z', finished_at: '2026-09-17T18:09:11Z',
    instruments: 96, signals: 96, failed: 0 }] },
  '/api/v1/market-data/imports': { items: [{ id: 'im1', kind: 'daily_update', provider: 'eodhd',
    status: 'succeeded', started_at: '2026-09-17T18:00:00Z', finished_at: '2026-09-17T18:04:00Z',
    counts: { processed: 4325, accepted: 4325, rejected: 0, flagged: 0 }, revised_count: 7 }] },
  '/api/v1/market-data/quality-findings': { items: [{ id: 'q1', instrument_id: ABB,
    session_date: '2017-04-13', rule: 'provider_gap', severity: 'warning',
    detail: 'the source has no bar for this session', status: 'open',
    reexamined_at: '2026-09-16T18:00:00Z', awaiting_decision: true, accepted_at: null }] },
}));

export function api(pathname) {
  if (routes.has(pathname)) return routes.get(pathname);
  if (pathname === '/api/v1/instruments') {
    return { items: [instrument(ABB, 'ABB', 'ABB Ltd', 'XSTO', 'Nasdaq Stockholm', 'SEK', 'SE'),
                     instrument(VOLVO, 'VOLV-B', 'Volvo B', 'XSTO', 'Nasdaq Stockholm', 'SEK', 'SE')],
             next_cursor: null, total: 2 };
  }
  const detail = pathname.match(/^\/api\/v1\/instruments\/([^/]+)$/);
  if (detail) {
    const base = instrument(detail[1], detail[1] === VOLVO ? 'VOLV-B' : 'ABB',
      detail[1] === VOLVO ? 'Volvo B' : 'ABB Ltd', 'XSTO', 'Nasdaq Stockholm', 'SEK', 'SE');
    return { ...base, latest_bar: bars(1)[0],
      history: { first_session: '2016-09-19', last_session: '2026-09-17', bar_count: 2513 },
      quality_summary: { open_warnings: 1, open_errors: 0 } };
  }
  if (/\/history$/.test(pathname)) {
    const series = bars(180);
    return {
      instrument: instrument(ABB, 'ABB', 'ABB Ltd', 'XSTO', 'Nasdaq Stockholm', 'SEK', 'SE'),
      coverage: { first_session: '2016-09-19', last_session: '2026-09-17', stored_sessions: 2513 },
      requested_from: series[0].session_date, requested_to: series[series.length - 1].session_date,
      bars: series, missing_sessions: [], series_basis: 'adjusted_close', provider: 'eodhd',
      observed_at: '2026-09-17T18:00:00Z',
      actions: [{ id: 'ca1', action_type: 'dividend', ex_date: '2026-03-26', ratio: null,
        amount: '9.000000000000', currency: 'SEK', old_symbol: null, new_symbol: null }],
      findings: [{ id: 'q1', rule: 'provider_gap', status: 'open', session_date: '2017-04-13',
        detail: 'the source has no bar for this session' }],
    };
  }
  if (/\/signal$/.test(pathname)) return signal;
  if (/^\/api\/v1\/signals/.test(pathname)) {
    return { items: [{ ...signal, ticker: 'ABB', name: 'ABB Ltd', rank: 1 },
      { ...signal, instrument_id: VOLVO, ticker: 'VOLV-B', name: 'Volvo B', rank: 2,
        score: '0.410000000000', action: 'buy' }],
      next_cursor: null, total: 2, session_date: '2026-09-17', scored: 2, unscored: 0,
      strategy: STRATEGY };
  }
  if (/^\/api\/v1\/backtests\/[^/]+\/trades$/.test(pathname)) {
    return { items: [{ id: 'bt1', instrument_id: ABB, ticker: 'ABB', name: 'ABB Ltd',
      direction: 'buy', signal_session: '2016-09-30', execution_session: '2016-10-03',
      quantity: '164.000000000000', price: '412.300000000000', costs: '68.400000000000',
      currency: 'SEK', value: '67617.200000000000' }], next_cursor: null, total: 1 };
  }
  if (/^\/api\/v1\/backtests\/[^/]+\/equity$/.test(pathname)) {
    return { items: Array.from({ length: 120 }, (_, k) => ({
      session_date: new Date(Date.UTC(2016, 9, 1) + k * 30 * 86400000).toISOString().slice(0, 10),
      total: (1000000 * (1 + k * 0.0092)).toFixed(12),
      position_value: (1000000 * (1 + k * 0.0092) * 0.98).toFixed(12),
      absence_reason: null })) };
  }
  if (/^\/api\/v1\/backtests\/[^/]+$/.test(pathname)) return backtest;
  return { items: [], next_cursor: null, total: 0 };
}


/** Every destination the primary navigation reaches, plus the two detail screens behind it. */
export const ROUTES = [
  { path: '/', name: 'Overview' },
  { path: '/markets', name: 'Market data' },
  { path: '/markets/' + ABB, name: 'Instrument detail' },
  { path: '/signals', name: 'Signals' },
  { path: '/portfolio', name: 'Portfolio' },
  { path: '/risk', name: 'Limits' },
  { path: '/intents', name: 'Intents' },
  { path: '/paper', name: 'Paper trading' },
  // The backtest result is a selector on the same page rather than a route of its own.
  { path: '/backtests', name: 'Backtests' },
  { path: '/operations', name: 'Operations' },
  { path: '/account', name: 'Account' },
];

export { ABB, VOLVO, OWNER, backtest };
