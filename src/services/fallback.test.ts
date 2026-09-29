import { describe, expect, it, vi } from 'vitest';
import { fetchFallbackState, MARKET_DATA_EVENT_TYPES } from './marketData';

describe('the fallback snapshot', () => {
  it('maps the recorded state', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true, json: async () => ({
      active: true, provider: 'yahoo', since: '2026-09-29', instruments: 100,
      pending_reconciliation: false, changed_at: '2026-09-29T18:05:00Z',
    }) });
    const state = await fetchFallbackState(fetcher);
    expect(fetcher).toHaveBeenCalledWith('/api/v1/market-data/fallback', expect.anything());
    expect(state).toEqual({ active: true, since: '2026-09-29', instruments: 100, pendingReconciliation: false });
  });

  it('refuses a body that is not the state', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ items: [] }) });
    await expect(fetchFallbackState(fetcher)).rejects.toThrow();
    const failed = vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) });
    await expect(fetchFallbackState(failed)).rejects.toThrow();
  });

  // A named event reaches only a listener registered for its name, so the banner hears nothing
  // unless the type is in the list the live client subscribes to.
  it('listens for the named change event', () => {
    expect(MARKET_DATA_EVENT_TYPES).toContain('market_data_fallback.changed.v1');
  });
});
