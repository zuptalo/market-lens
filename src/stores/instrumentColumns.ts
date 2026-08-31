import { ref, type Ref } from 'vue';
import type { ColumnPreference } from '@/types/marketData';

/**
 * The optional-column choice lives in browser storage on the device that made it, not in a
 * per-user table on the server (research R4). Putting it on the server would have introduced
 * this feature's only private table, its only migration, its only private authorization
 * scope and its only per-user event — all for a display preference.
 *
 * The cost of that decision is that browser storage is unreliable by nature: it can be
 * empty, it can hold something an older version wrote, and in a locked-down browser or a
 * thumbnail renderer it throws on access. Every read and write below is therefore guarded.
 * A preference about which columns to show must never be able to break the table it
 * decorates.
 */

export const OPTIONAL_COLUMNS = [
  'sector', 'country', 'return20', 'return90', 'volatility', 'storedSessions',
] as const;

export type OptionalColumn = (typeof OPTIONAL_COLUMNS)[number];

export const DEFAULT_COLUMNS: OptionalColumn[] = ['sector', 'return20', 'volatility'];

const STORAGE_KEY = 'market-lens.instrument-columns';

function isOptionalColumn(value: unknown): value is OptionalColumn {
  return typeof value === 'string' && (OPTIONAL_COLUMNS as readonly string[]).includes(value);
}

export function loadColumnPreference(): ColumnPreference {
  let raw: string | null = null;
  try {
    raw = window.localStorage.getItem(STORAGE_KEY);
  } catch {
    // Storage is unreachable — a private window, blocked site data, a preview renderer.
    return { columns: [...DEFAULT_COLUMNS] };
  }
  if (raw === null) return { columns: [...DEFAULT_COLUMNS] };

  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return { columns: [...DEFAULT_COLUMNS] };
    const columns = (parsed as { columns?: unknown }).columns;
    if (!Array.isArray(columns)) return { columns: [...DEFAULT_COLUMNS] };
    // A column this version no longer offers is dropped rather than passed on: a stale
    // preference should degrade to a smaller table, never to a broken one. An empty result
    // is kept as an empty result, because someone who turned every optional column off meant
    // it and restoring the defaults would undo that choice on every visit.
    return { columns: columns.filter(isOptionalColumn) };
  } catch {
    return { columns: [...DEFAULT_COLUMNS] };
  }
}

export function saveColumnPreference(preference: ColumnPreference): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({
      columns: preference.columns.filter(isOptionalColumn),
    }));
  } catch {
    // Out of quota, or storage denied. The choice still applies to the session in front of
    // the person; only its persistence is lost, and that is not worth an error.
  }
}

export function useColumnPreference(): {
  columns: Ref<string[]>;
  setColumns: (next: string[]) => void;
  isVisible: (column: OptionalColumn) => boolean;
} {
  const columns = ref<string[]>(loadColumnPreference().columns);
  return {
    columns,
    setColumns(next: string[]) {
      columns.value = next;
      saveColumnPreference({ columns: next });
    },
    isVisible: (column: OptionalColumn) => columns.value.includes(column),
  };
}
