import { flushPromises, mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { RouterLinkStub } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import DashboardView from './DashboardView.vue';

class QuietEventSource extends EventTarget {
  close(): void {}
}

function importRun(overrides: Record<string, unknown> = {}) {
  return {
    id: 'aaaaaaaa-0024-4000-8000-000000000001',
    kind: 'daily_update',
    provider: 'eodhd',
    status: 'succeeded',
    started_at: '2026-09-17T18:00:00Z',
    finished_at: '2026-09-17T18:04:00Z',
    counts: { processed: 4325, accepted: 4325, rejected: 0, flagged: 0 },
    revised_count: 7,
    ...overrides,
  };
}

function finding(overrides: Record<string, unknown> = {}) {
  return {
    id: 'dddddddd-0024-4000-8000-000000000001',
    instrument_id: '44000000-0000-4000-8000-000000000001',
    session_date: '2017-04-13',
    rule: 'provider_gap',
    severity: 'warning',
    detail: 'the source has no bar for this session',
    status: 'open',
    reexamined_at: '2026-09-16T18:00:00Z',
    awaiting_decision: true,
    accepted_at: null,
    ...overrides,
  };
}

function limit(overrides: Record<string, unknown> = {}) {
  return {
    kind: 'instrument_share',
    threshold: '0.250000000000',
    state: 'exceeded',
    measured: '0.412300000000',
    denominator: '284000.000000000000',
    absence_reason: null,
    contributions: [],
    ...overrides,
  };
}

function portfolio(overrides: Record<string, unknown> = {}) {
  return {
    accounting_currency: 'SEK',
    holdings: [],
    realised: [],
    total: {
      value: '0', cost: '0', unrealised: '0', realised: '0',
      complete: true, incomplete_reason: null, return_absence: 'cash_is_not_tracked',
    },
    records_what_you_entered: true,
    ...overrides,
  };
}

interface Stubs {
  imports?: unknown[];
  featureRuns?: unknown[];
  strategyRuns?: unknown[];
  findings?: unknown[];
  limits?: unknown[];
  portfolio?: Record<string, unknown>;
  failing?: string[];
}

function stubFetch(options: Stubs = {}) {
  const failing = options.failing ?? [];
  vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
    const url = String(input);
    const fails = failing.some((fragment) => url.includes(fragment));
    if (fails) return { ok: false, status: 500, json: async () => ({ error: 'unavailable' }) };
    if (url.includes('/api/v1/market-data/imports')) {
      return { ok: true, json: async () => ({ items: options.imports ?? [importRun()] }) };
    }
    if (url.includes('/api/v1/feature-runs')) {
      return { ok: true, json: async () => ({ items: options.featureRuns ?? [] }) };
    }
    if (url.includes('/api/v1/strategy-runs')) {
      return { ok: true, json: async () => ({ items: options.strategyRuns ?? [] }) };
    }
    if (url.includes('/api/v1/market-data/quality-findings')) {
      return { ok: true, json: async () => ({ items: options.findings ?? [] }) };
    }
    if (url.includes('/api/v1/risk-limits')) {
      return { ok: true, json: async () => ({
        accounting_currency: 'SEK', limits: options.limits ?? [], limits_are_your_own: true }) };
    }
    if (url.includes('/api/v1/portfolio')) {
      return { ok: true, json: async () => options.portfolio ?? portfolio() };
    }
    return { ok: false, json: async () => ({ error: 'not found' }) };
  }));
}

const global = { plugins: [PrimeVue], stubs: { RouterLink: RouterLinkStub } };

describe('DashboardView', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  // The reason the screen exists. This product has deliberately accumulated decisions only a person
  // can make and scattered them across three screens; nothing tells anybody they are there.
  it('says what is waiting, with a count and somewhere to go', async () => {
    stubFetch({ findings: [finding(), finding({ id: 'second' })], limits: [limit()] });
    const wrapper = mount(DashboardView, { global });
    await flushPromises();

    const waiting = wrapper.find('[data-testid="waiting"]');
    expect(waiting.exists()).toBe(true);
    const text = waiting.text();
    // Two findings, one limit — counted, not listed.
    expect(text).toContain('2');
    expect(text.toLowerCase()).toContain('decision');
    expect(text.toLowerCase()).toContain('limit');

    // Every item leads to the screen that owns it, which is the only place the figures live.
    const destinations = wrapper.findAllComponents(RouterLinkStub).map((link) => String(link.props('to')));
    expect(destinations).toContain('/operations');
    expect(destinations).toContain('/risk');
  });

  // Most days this is the answer, and it is the answer somebody came for.
  it('says plainly when nothing needs anybody', async () => {
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain('nothing needs you');
  });

  // Every figure on a dashboard is a second copy of something another screen owns. This is where
  // that discipline is most likely to be quietly abandoned.
  it('states no value, no percentage and no return', async () => {
    stubFetch({
      findings: [finding()],
      limits: [limit()],
      portfolio: portfolio({
        holdings: [{
          instrument_id: 'x', ticker: 'VOLV-B', name: 'Volvo AB', currency: 'SEK',
          quantity: '100', cost: '24619', unrealised: null,
          valuation: { value: null, session: null, conversion_rate: null, absence_reason: 'no_price' },
          comparison: {
            series: 'OMXS30.INDX', from_session: null, to_session: null,
            holding_return: null, benchmark_return: null, absence_reason: 'holding_not_valued',
          },
        }],
        total: {
          value: null, cost: '24619', unrealised: null, realised: '0',
          complete: false, incomplete_reason: 'at least one holding could not be valued',
          return_absence: 'cash_is_not_tracked',
        },
      }),
    });
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    const text = wrapper.text();

    expect(text).not.toContain('%');
    for (const currency of ['SEK', 'EUR', 'DKK', 'NOK']) {
      expect(text, `the Overview states a value in ${currency}`).not.toContain(currency);
    }
    // And none of the figures the portfolio owns leaked in.
    expect(text).not.toContain('24,619');
    expect(text).not.toContain('24619');
  });

  // The specific defect this feature exists to remove: the stub told a signed-in owner that shipped
  // features were unimplemented.
  it('makes no claim that shipped features are unimplemented', async () => {
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    const text = wrapper.text().toLowerCase();
    expect(text).not.toContain('will be implemented');
    expect(text).not.toContain('foundation stage');
  });
});

describe('DashboardView sections', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
    stubFetch();
  });

  // Feature 016 made the correction count first-class because a restated session moves every value
  // derived from it. Sessions stored is almost always the same number and tells nobody anything.
  it('reports sessions corrected rather than sessions stored', async () => {
    stubFetch({ imports: [importRun({ counts: { processed: 4325, accepted: 4325, rejected: 0, flagged: 0, revised: 7 } })] });
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    const changed = wrapper.find('[data-testid="changed"]').text();
    expect(changed).toContain('7');
    expect(changed.toLowerCase()).toContain('corrected');
    // The processed count is the one that is always the same; it has no business here.
    expect(changed).not.toContain('4325');
    expect(changed).not.toContain('4,325');
  });

  // A run that did not finish cleanly is something waiting, not something that merely happened.
  it('puts a failed run among the things waiting, not among the things that changed', async () => {
    stubFetch({ imports: [importRun({ status: 'partial' })] });
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    expect(wrapper.find('[data-testid="waiting"]').text().toLowerCase()).toContain('partial');
    expect(wrapper.find('[data-testid="waiting-clear"]').exists()).toBe(false);
  });

  // The failure that matters: a source that did not load must not look like one that was empty.
  it('says a source could not be read rather than reporting zero', async () => {
    stubFetch({ failing: ['/quality-findings'] });
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    const waiting = wrapper.find('[data-testid="waiting"]').text().toLowerCase();
    expect(waiting).toContain('could not be read');
    // And it does not claim the all-clear while something is unknown.
    expect(wrapper.find('[data-testid="waiting-clear"]').exists()).toBe(false);
  });

  // A heading that always says "nothing missing" teaches a reader to skip the region.
  it('omits the unknown section entirely when nothing is missing', async () => {
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    expect(wrapper.find('[data-testid="unknown"]').exists()).toBe(false);
  });

  it('collects the absences other screens state one at a time', async () => {
    stubFetch({
      limits: [limit({ state: 'unevaluable', measured: null, denominator: null, absence_reason: 'portfolio_incomplete' })],
      portfolio: portfolio({
        holdings: [{
          instrument_id: 'x', ticker: 'VOLV-B', name: 'Volvo AB', currency: 'SEK',
          quantity: '100', cost: '24619', unrealised: null,
          valuation: { value: null, session: null, conversion_rate: null, absence_reason: 'no_price' },
          comparison: {
            series: 'OMXS30.INDX', from_session: null, to_session: null,
            holding_return: null, benchmark_return: null, absence_reason: 'holding_not_valued',
          },
        }],
        total: {
          value: null, cost: '24619', unrealised: null, realised: '0',
          complete: false, incomplete_reason: 'at least one holding could not be valued',
          return_absence: 'cash_is_not_tracked',
        },
      }),
    });
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    const unknown = wrapper.find('[data-testid="unknown"]').text().toLowerCase();
    expect(unknown).toContain('could not be valued');
    expect(unknown).toContain('could not be evaluated');
    // An unevaluable limit is not reported as satisfied, here or anywhere.
    expect(unknown).not.toContain('within');
  });

  it('sends every item to the screen that owns it', async () => {
    stubFetch({
      findings: [finding()],
      limits: [limit()],
      strategyRuns: [{
        id: 'r', kind: 'full', status: 'succeeded', started_at: '2026-09-17T04:00:00Z',
        finished_at: '2026-09-17T04:02:00Z', instrument_count: 100, signal_count: 243005,
        failed_count: 0, trigger_feature_run_id: null, app_version: '0.19.0',
      }],
    });
    const wrapper = mount(DashboardView, { global });
    await flushPromises();
    const destinations = wrapper.findAllComponents(RouterLinkStub).map((link) => String(link.props('to')));
    expect(destinations).toContain('/operations');
    expect(destinations).toContain('/risk');
    expect(destinations).toContain('/signals');
    // Nothing points at a route that does not exist.
    for (const destination of destinations) {
      expect(['/operations', '/risk', '/signals', '/portfolio']).toContain(destination);
    }
  });
});
