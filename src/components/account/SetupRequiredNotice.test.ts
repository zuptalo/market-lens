import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import SetupRequiredNotice from './SetupRequiredNotice.vue';

describe('SetupRequiredNotice', () => {
  it('explains why setup needs a link from the server and names the command', () => {
    const wrapper = mount(SetupRequiredNotice);
    const text = wrapper.text();

    // Somebody who has just deployed this and lands on a sign-in form needs to be told
    // what is happening, not left guessing why they cannot sign in.
    expect(text).toContain('has not been set up');
    expect(text).toContain('market-lens auth setup-link');
    // The reason matters: without it the extra step reads as pointless ceremony.
    expect(text.toLowerCase()).toMatch(/anyone|first person|whoever|claim/);
  });

  it('describes where to run the command without assuming how it was deployed', () => {
    const text = mount(SetupRequiredNotice).text();

    // A self-hosted product is run under Compose, a container platform, or straight on a
    // host. Naming one of those in the instruction strands everybody using another.
    for (const assumption of ['kubectl', 'k3s', 'kubernetes', 'docker compose exec', 'helm']) {
      expect(text.toLowerCase()).not.toContain(assumption);
    }
    expect(text.toLowerCase()).toContain('wherever');
  });

  it('offers the command for copying and reports the result', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const wrapper = mount(SetupRequiredNotice);

    await wrapper.get('[data-copy-setup-command]').trigger('click');
    await Promise.resolve();

    expect(writeText).toHaveBeenCalledWith('market-lens auth setup-link');
    expect(wrapper.text()).toContain('Copied');
  });

  it('stays usable when the clipboard is unavailable', async () => {
    Object.assign(navigator, { clipboard: undefined });
    const wrapper = mount(SetupRequiredNotice);

    await wrapper.get('[data-copy-setup-command]').trigger('click');
    await Promise.resolve();

    // The command is on screen either way; only the convenience is lost.
    expect(wrapper.text()).toContain('market-lens auth setup-link');
    expect(wrapper.text()).not.toContain('Copied');
  });
});
