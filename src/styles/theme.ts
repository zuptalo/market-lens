import Aura from '@primeuix/themes/aura';
import { definePreset } from '@primeuix/themes';

// Aura's stock light theme paints a primary button as white on emerald-500, which measures
// 2.54:1 - well under the 4.5:1 the accessibility suite enforces. Rather than restyle buttons
// component by component, the palette itself is corrected once here: light mode uses a darker
// emerald behind white text, and dark mode keeps the light emerald with dark text it already
// had. Every component that draws on the primary colour inherits the fix.
//
// It lives in its own module so tests can render the exact palette production does without
// importing the application entry point and mounting it.
export const marketLensTheme = definePreset(Aura, {
  semantic: {
    // Aura indicates focus on a form field with a border-colour change and nothing else, which
    // is not a visible focus indicator: it fails the accessibility suite's ring check and is
    // easy to miss for anybody navigating by keyboard. A real outline is defined once here and
    // every field inherits it.
    formField: {
      focusRing: {
        width: '2px',
        style: 'solid',
        color: '{primary.color}',
        offset: '2px',
        shadow: 'none',
      },
    },
    colorScheme: {
      light: {
        primary: {
          color: '{emerald.700}',
          contrastColor: '#ffffff',
          hoverColor: '{emerald.800}',
          activeColor: '{emerald.900}',
        },
      },
      dark: {
        primary: {
          color: '{emerald.400}',
          contrastColor: '{surface.900}',
          hoverColor: '{emerald.300}',
          activeColor: '{emerald.200}',
        },
      },
    },
  },
  components: {
    button: {
      colorScheme: {
        light: {
          root: {
          // Aura's danger button is white on red-500, which measures 3.76:1 - the same class
          // of gap as the primary one, and hidden until the hand-rolled button styles that
          // were overriding it were removed. A darker red carries white text at 6.5:1.
          danger: {
            background: '{red.700}',
            hoverBackground: '{red.800}',
            activeBackground: '{red.900}',
            borderColor: '{red.700}',
            hoverBorderColor: '{red.800}',
            activeBorderColor: '{red.900}',
            color: '#ffffff',
            hoverColor: '#ffffff',
            activeColor: '#ffffff',
          },
          },
        },
      },
    },
  },
});

export const themeOptions = { darkModeSelector: '.market-lens-dark' };
