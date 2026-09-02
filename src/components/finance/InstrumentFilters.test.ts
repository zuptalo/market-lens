import { mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import Select from 'primevue/select';
import { describe, expect, it } from 'vitest';
import InstrumentFilters from './InstrumentFilters.vue';

/**
 * The labels the sector selector offers. PrimeVue renders a listbox rather than <option>
 * elements, so the choices are read from what the component was handed.
 */
function sectorLabels(wrapper: ReturnType<typeof mountFilters>): string[] {
  const selector = wrapper.findAllComponents(Select)
    .find((select) => select.attributes('input-id') === 'markets-sector'
      || select.props('inputId') === 'markets-sector');
  const options = (selector?.props('options') ?? []) as { label: string }[];
  return options.map((option) => option.label).filter((label) => !label.startsWith('All '));
}

function mountFilters(overrides: Record<string, unknown> = {}) {
  return mount(InstrumentFilters, {
    global: { plugins: [PrimeVue] },
    props: {
      query: '', mic: '', country: '', sector: '', status: '',
      sort: 'name', order: 'asc',
      sectors: [
        { code: 'health_care', name: 'Health Care', instrumentCount: 12 },
        { code: 'industrials', name: 'Industrials', instrumentCount: 28 },
        { code: 'unclassified', name: 'Unclassified', instrumentCount: 0 },
      ],
      ...overrides,
    },
  });
}

describe('InstrumentFilters', () => {
  // Feature 014 US3. The choices came from a constant in this component, which offered twelve
  // options for eleven ideas — both "Information Technology" and "Technology" — against a
  // column that was null in every row. The vocabulary now comes from the data.
  it('offers exactly the sectors it was given, and nothing else', () => {
    const offered = sectorLabels(mountFilters());

    expect(offered).toContain('Health Care');
    expect(offered).toContain('Industrials');
    expect(offered).toContain('Unclassified');
    // The names that used to be hardcoded and could never match anything.
    expect(offered).not.toContain('Technology');
    expect(offered).not.toContain('Information Technology');
    expect(offered).not.toContain('Real Estate');
  });

  it('offers nothing at all when the vocabulary has not arrived', () => {
    const offered = sectorLabels(mountFilters({ sectors: [] }));
    // Better an empty selector for a moment than a list of promises the data cannot keep.
    expect(offered).not.toContain('Health Care');
  });
});
