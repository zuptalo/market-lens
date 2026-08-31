import { describe, expect, it, vi } from 'vitest';
import { fetchInstrumentListing, listingQueryString } from './marketData';

/**
 * The listing client's one job that is easy to get wrong: an absent statistic must survive
 * the trip to the screen as an absence. Mapping it to 0 turns "there were too few stored
 * sessions to compute this" into "this instrument did not move", which is a different and
 * false claim (FR-007).
 */

function wireRow(overrides: Record<string, unknown> = {}) {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    isin: 'DK0000000300',
    ticker: 'SHORT',
    name: 'Barely Listed A/S',
    exchange: { mic: 'XCSE', name: 'Nasdaq Copenhagen' },
    currency: 'DKK',
    country: 'DK',
    sector: 'Health Care',
    industry: 'Biotechnology',
    instrument_type: 'common_stock',
    status: 'active',
    purchasability_status: 'unverified',
    latest_session: '2026-06-30',
    latest_close: '101.25',
    change_absolute: '0.25',
    change_percent: 0.0025,
    return_20: null,
    return_90: null,
    volatility: null,
    stored_sessions: 20,
    freshness: { state: 'current', sessions_behind: 0 },
    ...overrides,
  };
}

function respondWith(items: unknown[]) {
  return vi.fn(async () => ({ ok: true, json: async () => ({ items, next_cursor: null }) }));
}

describe('fetchInstrumentListing', () => {
  it('keeps an uncomputable statistic absent rather than turning it into zero', async () => {
    const page = await fetchInstrumentListing({}, respondWith([wireRow()]));
    const row = page.items[0];
    expect(row.return20).toBeNull();
    expect(row.return90).toBeNull();
    expect(row.volatility).toBeNull();
    // A statistic that *was* computed must still arrive intact, so absence is not achieved
    // by discarding everything.
    expect(row.changePercent).toBe(0.0025);
  });

  it('distinguishes an instrument with no history from one that is merely current', async () => {
    const page = await fetchInstrumentListing({}, respondWith([
      wireRow({
        latest_session: null, latest_close: null, change_absolute: null, change_percent: null,
        stored_sessions: 0, freshness: { state: 'no_history', sessions_behind: null },
      }),
    ]));
    const row = page.items[0];
    expect(row.freshness.state).toBe('no_history');
    // Nothing to be behind is not the same as being zero sessions behind, which is what
    // "current" means.
    expect(row.freshness.sessionsBehind).toBeNull();
    expect(row.latestClose).toBeNull();
  });

  it('keeps money as a decimal string so no value is quietly rounded', async () => {
    const page = await fetchInstrumentListing({}, respondWith([
      wireRow({ latest_close: '1234.56789012', change_absolute: '-0.00000001' }),
    ]));
    expect(page.items[0].latestClose).toBe('1234.56789012');
    expect(page.items[0].changeAbsolute).toBe('-0.00000001');
  });

  it('sends the contract query vocabulary rather than the words the old search used', () => {
    const query = listingQueryString({
      query: 'alfa', mic: 'XSTO', country: 'SE', sector: 'Technology',
      status: 'active', sort: 'return_20', order: 'desc', limit: 25,
    });
    expect(query).toContain('mic=XSTO');
    expect(query).toContain('sector=Technology');
    expect(query).toContain('status=active');
    expect(query).toContain('sort=return_20');
    expect(query).toContain('order=desc');
    expect(query).not.toContain('exchange=');
    expect(query).not.toContain('active=true');
  });
});
