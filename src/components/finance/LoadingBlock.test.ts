import { mount } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { describe, expect, it } from 'vitest';
import LoadingBlock from './LoadingBlock.vue';

describe('LoadingBlock', () => {
  it('says what is loading, so a screen reader hears more than a spinner', () => {
    const wrapper = mount(LoadingBlock, { props: { label: 'Loading instruments…' }, global: { plugins: [PrimeVue] } });
    expect(wrapper.text()).toContain('Loading instruments…');
    expect(wrapper.attributes('role')).toBe('status');
    expect(wrapper.attributes('aria-live')).toBe('polite');
  });

  // The point of the block: it occupies the space the content is about to fill, so the spinner
  // sits where the reader is already looking rather than on a heading that is already finished.
  it('reserves room for the content it stands in for', () => {
    const wrapper = mount(LoadingBlock, { props: { label: 'Loading', rows: 8 }, global: { plugins: [PrimeVue] } });
    expect(wrapper.attributes('style')).toContain('min-height: 26rem');
  });
});
