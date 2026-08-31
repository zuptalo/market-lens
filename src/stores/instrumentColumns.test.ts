import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  DEFAULT_COLUMNS,
  loadColumnPreference,
  saveColumnPreference,
  useColumnPreference,
} from './instrumentColumns';

/**
 * The column choice lives on the device, not on the server (research R4). That decision buys
 * simplicity at the cost of one obligation: browser storage is allowed to be missing, full,
 * or to throw outright, and the table has to keep working in every one of those cases. A
 * display preference must never be able to break the page it decorates.
 */
describe('instrument column preference', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  it('falls back to the default column set when the device has stored nothing', () => {
    expect(loadColumnPreference().columns).toEqual(DEFAULT_COLUMNS);
  });

  it('remembers a choice across visits on the same device', () => {
    saveColumnPreference({ columns: ['country', 'return90'] });
    expect(loadColumnPreference().columns).toEqual(['country', 'return90']);
  });

  it('remembers an explicitly empty choice rather than treating it as unset', () => {
    saveColumnPreference({ columns: [] });
    // Someone who turned every optional column off meant it; restoring the defaults would
    // undo their choice on every visit.
    expect(loadColumnPreference().columns).toEqual([]);
  });

  it('ignores a stored column this version no longer offers', () => {
    window.localStorage.setItem(
      'market-lens.instrument-columns',
      JSON.stringify({ columns: ['sector', 'a-column-we-removed'] }),
    );
    expect(loadColumnPreference().columns).toEqual(['sector']);
  });

  it('falls back to the defaults when the stored value is not usable', () => {
    window.localStorage.setItem('market-lens.instrument-columns', 'not json at all');
    expect(loadColumnPreference().columns).toEqual(DEFAULT_COLUMNS);
  });

  it('still renders defaults when reading storage throws, as in a locked-down browser', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('The operation is insecure.', 'SecurityError');
    });
    expect(() => loadColumnPreference()).not.toThrow();
    expect(loadColumnPreference().columns).toEqual(DEFAULT_COLUMNS);
  });

  it('does not let a failing write break the interaction that caused it', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('Quota exceeded.', 'QuotaExceededError');
    });
    const preference = useColumnPreference();
    expect(() => preference.setColumns(['country'])).not.toThrow();
    // The choice still applies to the session in front of the person, even unsaved.
    expect(preference.columns.value).toEqual(['country']);
  });
});
