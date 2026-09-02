import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import OperationsView from './OperationsView.vue';
import { buildFeatureRun } from '@/services/__fixtures__/marketData';

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

function stubFetch(options: {
  imports?: unknown[];
  featureRuns?: unknown[];
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
