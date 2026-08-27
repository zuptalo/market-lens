import { onBeforeUnmount, ref, watch } from 'vue';
import { isDarkTheme, normalizeTheme, type ThemePreference } from '@/utils/theme';

const storageKey = 'market-lens-theme';
const preference = ref<ThemePreference>(normalizeTheme(localStorage.getItem(storageKey)));
const media = window.matchMedia('(prefers-color-scheme: dark)');

function applyTheme(): void {
  document.documentElement.classList.toggle(
    'market-lens-dark',
    isDarkTheme(preference.value, media.matches),
  );
}

export function useTheme() {
  const onSystemChange = () => applyTheme();
  media.addEventListener('change', onSystemChange);
  watch(preference, (value) => {
    localStorage.setItem(storageKey, value);
    applyTheme();
  }, { immediate: true });
  onBeforeUnmount(() => media.removeEventListener('change', onSystemChange));

  function cycleTheme(): void {
    const order: ThemePreference[] = ['system', 'light', 'dark'];
    preference.value = order[(order.indexOf(preference.value) + 1) % order.length];
  }

  return { preference, cycleTheme };
}
