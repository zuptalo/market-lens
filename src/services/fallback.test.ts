import { describe, expect, it, vi } from 'vitest';
import { fetchFallbackState } from './marketData';

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
});
