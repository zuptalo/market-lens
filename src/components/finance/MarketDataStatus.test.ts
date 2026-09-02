import { mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import MarketDataStatus from './MarketDataStatus.vue';
import type { ImportRunSummary } from '@/types/marketData';

const partialRun: ImportRunSummary = {
  id: '22000000-0000-4000-8000-000000000001',
  kind: 'backfill',
  provider: 'fixture',
  status: 'partial',
  startedAt: '2026-08-29T08:00:00Z',
  finishedAt: '2026-08-29T08:01:00Z',
  counts: { processed: 3, accepted: 2, rejected: 1, flagged: 1 },
  errorSummary: 'One instrument failed safely.',
};

describe('MarketDataStatus', () => {
  beforeEach(() => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });

  it.each(['connected', 'reconnecting', 'stale', 'offline'] as const)('shows %s connection state as text', (state) => {
    const wrapper = mount(MarketDataStatus, { props: { runs: [], connectionState: state } });
    expect(wrapper.get('[data-testid="connection-state"]').text().toLowerCase()).toContain(state);
  });

  it('shows counts, safe errors, warning text plus color semantics, and copies the host retry command', async () => {
    const wrapper = mount(MarketDataStatus, { props: { runs: [partialRun], connectionState: 'connected' } });
    expect(wrapper.text()).toContain('Accepted 2');
    expect(wrapper.text()).toContain('Rejected 1');
    expect(wrapper.text()).toContain('Flagged 1');
    expect(wrapper.text()).toContain('One instrument failed safely.');
    const status = wrapper.get('[data-testid="run-status"]');
    expect(status.text().toLowerCase()).toContain('partial');
    expect(status.classes()).toContain('severity-warning');
    await wrapper.get('[data-testid="copy-retry"]').trigger('click');
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      'market-lens marketdata retry --run 22000000-0000-4000-8000-000000000001',
    );
  });

  it('provides explicit loading and failure states', () => {
    const loading = mount(MarketDataStatus, { props: { runs: [], connectionState: 'reconnecting', loading: true } });
    expect(loading.get('[role="status"]').text()).toContain('Loading market-data status');
    const failed = mount(MarketDataStatus, { props: { runs: [], connectionState: 'offline', error: 'Unable to load market-data status.' } });
    expect(failed.get('[role="alert"]').text()).toBe('Unable to load market-data status.');
  });
});

describe('MarketDataStatus corrected sessions', () => {
  function run(revised: number | undefined): ImportRunSummary {
    return {
      ...partialRun,
      status: 'succeeded',
      errorSummary: null,
      counts: { processed: 5, accepted: 5, rejected: 0, flagged: 0, revised },
    };
  }

  // A run that corrected a session it had already stored is the one worth noticing: every
  // feature and every signal derived from that session moved underneath it.
  it('states how many sessions a run corrected', () => {
    const wrapper = mount(MarketDataStatus, {
      props: { runs: [run(2)], connectionState: 'connected' },
    });
    const text = wrapper.text();
    expect(text).toContain('Corrected');
    expect(text).toMatch(/Corrected\s*2/);
    // The correction must not be conveyed by replacing another count.
    expect(text).toMatch(/Accepted\s*5/);
  });

  // Zero is shown the way the other zero counts are shown. Hiding it would make an ordinary
  // night indistinguishable from a screen that cannot report corrections at all.
  it('does not imply a correction occurred when none did', () => {
    const wrapper = mount(MarketDataStatus, {
      props: { runs: [run(0)], connectionState: 'connected' },
    });
    const text = wrapper.text();
    expect(text).toMatch(/Corrected\s*0/);
    expect(text).not.toMatch(/Corrected\s*[1-9]/);
  });

  // A server that predates this feature sends no count. Absent and zero are different claims,
  // so the row is omitted rather than rendered as a confident zero the server never made.
  it('says nothing when the server did not report a count', () => {
    const wrapper = mount(MarketDataStatus, {
      props: { runs: [run(undefined)], connectionState: 'connected' },
    });
    expect(wrapper.text()).not.toContain('Corrected');
  });
});
