export function formatBuildVersion(raw: string): string {
  const value = raw.trim();
  if (!/^\d+\.\d+\.\d+$/.test(value)) {
    return 'development';
  }
  return `v${value}`;
}

export const buildVersion = formatBuildVersion(__APP_VERSION__);
