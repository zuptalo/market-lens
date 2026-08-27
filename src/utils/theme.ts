export type ThemePreference = 'system' | 'light' | 'dark';

export function normalizeTheme(value: string | null): ThemePreference {
  return value === 'light' || value === 'dark' ? value : 'system';
}

export function isDarkTheme(preference: ThemePreference, systemIsDark: boolean): boolean {
  return preference === 'dark' || (preference === 'system' && systemIsDark);
}
