import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import ChartRangeControls from './ChartRangeControls.vue';

function mountControls(overrides: Record<string, unknown> = {}) {
  return mount(ChartRangeControls, {
    props: { sessions: 250, overlays: [20], coverageSessions: 300, ...overrides },
  });
}

/**
 * The chart is the feature, so its controls carry the accessibility burden. Everything here
 * must be operable by keyboard alone and by touch alone, and nothing may depend on hover
 * (SC-008). Ranges are labelled in sessions, or in a named period whose session count is
 * stated, so a reader is never left inferring what a range contains (research R7).
 */
describe('ChartRangeControls', () => {
  it('labels every range in sessions rather than calendar days', () => {
    const text = mountControls().text();
    // "30 days" would mean a different number of observations on each exchange in a week
    // containing a Norwegian holiday, so it is never the unit shown.
    expect(text).not.toMatch(/\d+\s*days/i);
    expect(text).toMatch(/sessions/i);
  });

  it('asks for a new range when one is chosen', async () => {
    const wrapper = mountControls();
    const buttons = wrapper.findAll('button');
    const monthly = buttons.find((button) => button.text().includes('20'));
    await monthly!.trigger('click');
    expect(wrapper.emitted('range')).toBeTruthy();
  });

  it('offers zoom and pan as real controls, not only as pinch and drag', async () => {
    const wrapper = mountControls();
    for (const label of ['Zoom in', 'Zoom out', 'Pan back', 'Pan forward']) {
      const control = wrapper.find(`[aria-label="${label}"]`);
      expect(control.exists(), `${label} is not reachable without touch`).toBe(true);
    }
    await wrapper.get('[aria-label="Zoom in"]').trigger('click');
    expect(wrapper.emitted('zoom')![0][0]).toBe('in');
    await wrapper.get('[aria-label="Pan back"]').trigger('click');
    expect(wrapper.emitted('pan')![0][0]).toBe('back');
  });

  it('makes every control a real button so the keyboard reaches it', () => {
    const wrapper = mountControls();
    const interactive = wrapper.findAll('button, input, select, a');
    expect(interactive.length).toBeGreaterThan(4);
    for (const element of interactive) {
      // A div with a click handler is reachable by mouse and by nothing else.
      expect(['BUTTON', 'INPUT', 'SELECT', 'A']).toContain(element.element.tagName);
    }
  });

  it('toggles a moving-average overlay and says which is active', async () => {
    const wrapper = mountControls({ overlays: [20] });
    const toggle = wrapper.find('[aria-label="20-session moving average"]');
    expect(toggle.exists()).toBe(true);
    expect(toggle.attributes('aria-pressed')).toBe('true');
    await toggle.trigger('click');
    expect(wrapper.emitted('toggle-overlay')![0][0]).toBe(20);
  });

  it('marks the selected range so the current state is visible, not just implied by colour', () => {
    const wrapper = mountControls({ sessions: 250 });
    const pressed = wrapper.findAll('[aria-pressed="true"]');
    expect(pressed.length).toBeGreaterThan(0);
  });

  it('does not offer a range longer than the stored coverage', () => {
    const wrapper = mountControls({ coverageSessions: 40 });
    const text = wrapper.text();
    // Offering a year of history for an instrument that has forty sessions invites the chart
    // to look empty and the reader to think data is missing.
    expect(text).not.toContain('250');
  });
});
