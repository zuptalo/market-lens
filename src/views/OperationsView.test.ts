import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import OperationsView from './OperationsView.vue';
import { buildFeatureRun, buildStrategyRun } from '@/services/__fixtures__/marketData';

class QuietEventSource extends EventTarget {
  close(): void {}
}

function importRun(overrides: Record<string, unknown> = {}) {
  return {
    id: 'aaaaaaaa-0014-4000-8000-000000000001',
    kind: 'daily_update',
    provider: 'eodhd',
    status: 'succeeded',
    started_at: '2026-09-01T20:00:00Z',
    finished_at: '2026-09-01T20:04:00Z',
    counts: { processed: 100, accepted: 100, rejected: 0, flagged: 0 },
    ...overrides,
  };
}

function featureRunWire(overrides: Record<string, unknown> = {}) {
  const run = buildFeatureRun();
  return {
    id: run.id,
    kind: run.kind,
    status: run.status,
    started_at: run.startedAt,
    finished_at: run.finishedAt,
    instrument_count: run.instrumentCount,
    value_count: run.valueCount,
    failed_count: run.failedCount,
    trigger_run_id: run.triggerRunId,
    definition_name: run.definitionName,
    app_version: run.appVersion,
    ...overrides,
  };
}

function strategyRunWire(overrides: Record<string, unknown> = {}) {
  const run = buildStrategyRun();
  return {
    id: run.id,
    kind: run.kind,
    status: run.status,
    started_at: run.startedAt,
    finished_at: run.finishedAt,
    instrument_count: run.instrumentCount,
    signal_count: run.signalCount,
    failed_count: run.failedCount,
    trigger_feature_run_id: run.triggerFeatureRunId,
    app_version: run.appVersion,
    ...overrides,
  };
}

function stubFetch(options: {
  imports?: unknown[];
  featureRuns?: unknown[];
  strategyRuns?: unknown[];
  findings?: unknown[];
} = {}) {
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
    const url = String(input);
    if (url.includes('/api/v1/market-data/imports')) {
      return { ok: true, json: async () => ({ items: options.imports ?? [importRun()] }) };
    }
    if (url.includes('/api/v1/feature-runs')) {
      return { ok: true, json: async () => ({ items: options.featureRuns ?? [featureRunWire()] }) };
    }
    if (url.includes('/api/v1/strategy-runs')) {
      return { ok: true, json: async () => ({ items: options.strategyRuns ?? [strategyRunWire()] }) };
    }
    if (url.includes('/api/v1/market-data/quality-findings')) {
      return { ok: true, json: async () => ({ items: options.findings ?? [] }) };
    }
    return { ok: false, json: async () => ({ error: 'not found' }) };
  }));
}

describe('OperationsView', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('reports the imports and the engine runs on one screen', async () => {
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    const text = wrapper.text();
    // The import half, which used to live under the instrument table.
    expect(text).toContain('eodhd');
    expect(text).toContain('100');
    // The engine half, which has had no interface at all since the engine shipped.
    expect(text.toLowerCase()).toContain('full');
    expect(text).toContain('5,830,104');
  });

  it('states a failed engine run and how many instruments it left stale', async () => {
    stubFetch({
      featureRuns: [featureRunWire({
        kind: 'incremental', status: 'partial', failed_count: 3, value_count: 7502,
      })],
    });
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).toContain('partial');
    // A partial run leaves the previous values standing. Saying how many failed is the
    // difference between "the numbers are current" and "three of them are not".
    expect(text).toContain('3');
  });

  it('explains a deployment where the engine has never run', async () => {
    stubFetch({ featureRuns: [] });
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    const engine = wrapper.find('[aria-labelledby="feature-runs-heading"]').text().toLowerCase();
    expect(engine).toMatch(/has not run|never run|no feature runs/);
    // The engine section must not imply the statistics elsewhere are current. Scoped to that
    // section: the import above it legitimately says "succeeded".
    expect(engine).not.toContain('succeeded');
    expect(wrapper.find('[data-testid="feature-run-list"]').exists()).toBe(false);
  });

  it('explains a fresh installation with no imports rather than rendering an empty table', async () => {
    stubFetch({ imports: [], featureRuns: [] });
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toMatch(/no import|has not run|not yet/);
  });
});

describe('OperationsView strategy runs', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  it('shows strategy runs beside the feature runs they follow', async () => {
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    const strategy = wrapper.find('[aria-labelledby="strategy-runs-heading"]').text();
    expect(strategy).toContain('25,460');
    expect(strategy.toLowerCase()).toContain('incremental');
    // Both engines are on one screen, in the order they run.
    expect(wrapper.find('[aria-labelledby="feature-runs-heading"]').exists()).toBe(true);
  });

  // A partial run left some instruments with the views a previous run recorded. Nothing on the
  // ranking screen can say so, which is why this screen must.
  it('states how many instruments a partial strategy run left with earlier signals', async () => {
    stubFetch({ strategyRuns: [strategyRunWire({ status: 'partial', failed_count: 4 })] });
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    const strategy = wrapper.find('[aria-labelledby="strategy-runs-heading"]').text().toLowerCase();
    expect(strategy).toContain('partial');
    expect(strategy).toContain('4 instruments kept their earlier signals');
  });

  it('explains a deployment where no strategy has run', async () => {
    stubFetch({ strategyRuns: [] });
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    const strategy = wrapper.find('[aria-labelledby="strategy-runs-heading"]').text().toLowerCase();
    expect(strategy).toContain('no strategy has run');
    expect(wrapper.find('[data-testid="strategy-run-list"]').exists()).toBe(false);
  });

  it('keeps the two reports independent when one of them fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
      const url = String(input);
      if (url.includes('/api/v1/strategy-runs')) return { ok: false, json: async () => ({}) };
      if (url.includes('/api/v1/feature-runs')) {
        return { ok: true, json: async () => ({ items: [featureRunWire()] }) };
      }
      return { ok: true, json: async () => ({ items: [importRun()] }) };
    }));
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();
    expect(wrapper.text()).toContain('Unable to load recent strategy runs.');
    // The feature half still answers the question somebody came here to ask.
    expect(wrapper.find('[data-testid="feature-run-list"]').exists()).toBe(true);
  });
});

describe('OperationsView while loading', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
  });

  // The spinner belongs where the rows will be. A table's own overlay covers the table, and
  // before any rows exist that is only its header row — so it landed on the column headings.
  it('shows a first load where each report will be, not over its headings', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
    const wrapper = mount(OperationsView, { global: { plugins: [PrimeVue] } });
    await flushPromises();

    const blocks = wrapper.findAll('[data-testid="loading-block"]');
    expect(blocks.length).toBeGreaterThanOrEqual(2);
    expect(wrapper.text()).toContain('Loading feature runs…');
    expect(wrapper.text()).toContain('Loading strategy runs…');
    expect(wrapper.find('.p-datatable-mask').exists()).toBe(false);
    // And it must not claim the engine has never run while it is still finding out.
    expect(wrapper.text()).not.toContain('has not run in this deployment');
  });
});
