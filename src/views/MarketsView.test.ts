import { flushPromises, mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import MarketsView from './MarketsView.vue';

class QuietEventSource extends EventTarget { close(): void {} }

describe('MarketsView instrument inspection', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', QuietEventSource);
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL) => {
      const url = String(input);
      if (url.includes('/api/v1/instruments?')) return { ok: true, json: async () => ({ items: [], next_cursor: null }) };
      if (url.includes('/api/v1/market-data/imports')) return { ok: true, json: async () => ({ items: [] }) };
      return { ok: false, json: async () => ({ error: 'not found' }) };
    }));
  });

  it('provides accessible identity filters and explicit loading/empty results', async () => {
    const wrapper = mount(MarketsView);
    expect(wrapper.get('input[aria-label="Search instruments"]')).toBeTruthy();
    expect(wrapper.get('select[aria-label="Exchange"]')).toBeTruthy();
    expect(wrapper.get('select[aria-label="Country"]')).toBeTruthy();
    expect(wrapper.get('select[aria-label="Currency"]')).toBeTruthy();
    expect(wrapper.get('select[aria-label="Active status"]')).toBeTruthy();
    expect(wrapper.get('[role="status"]').text()).toContain('Loading instruments');
    await flushPromises();
    expect(wrapper.text()).toContain('No instruments match these filters');
  });

  it('shows safe search failure text without discarding entered filters', async () => {
    vi.mocked(fetch).mockImplementation(async (input) => {
      if (String(input).includes('/api/v1/instruments?')) return { ok: false, json: async () => ({ error: 'token=secret' }) } as Response;
      return { ok: true, json: async () => ({ items: [] }) } as Response;
    });
    const wrapper = mount(MarketsView);
    const search = wrapper.get('input[aria-label="Search instruments"]');
    await search.setValue('ALFA');
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain('Unable to load instruments');
    expect((search.element as HTMLInputElement).value).toBe('ALFA');
    expect(wrapper.text()).not.toContain('token=secret');
  });
});
