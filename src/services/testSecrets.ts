import { createHash } from 'node:crypto';

// Test fixtures standing in for credentials are derived rather than written down. A literal
// password beside an SMTP host and username is indistinguishable from a real credential to
// anything scanning this repository, and a derived value cannot collide with a real one.
// It is stable across runs, so a failure stays reproducible.
export function fixtureSecret(label: string): string {
  return `${label}-${createHash('sha256').update(`market-lens/fixtures/${label}`).digest('hex').slice(0, 16)}`;
}

export const mailAccount = fixtureSecret('mail-account');
export const mailSecret = fixtureSecret('mail-secret');
