// Component tests mount through the same PrimeVue configuration production uses, so a test
// renders what a person actually sees - including the Aura theme's own focus, disabled, and
// invalid states, which are the ones the project stylesheet used to re-implement by hand.
import { config } from '@vue/test-utils';
import PrimeVue from 'primevue/config';
import { marketLensTheme, themeOptions } from '@/styles/theme';

config.global.plugins = [
  [PrimeVue, { theme: { preset: marketLensTheme, options: themeOptions } }],
];

// jsdom implements no matchMedia, and PrimeVue's Select binds an orientation listener on
// mount. Defined only when absent, so a test that installs its own theme-aware stub keeps it.
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}
