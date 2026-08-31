import { describe, expect, it, vi } from 'vitest';
import { AuthClient, AuthRequestError } from '@/services/auth';

function rejecting(body: unknown, status = 400) {
  return vi.fn(async () => ({ ok: false, status, json: async () => body }));
}

const setupInput = {
  capability: 'c', displayName: 'Owner', email: 'owner@example.com', password: 'a-long-enough-password',
  eodhdApiKey: 'key', smtp: { host: 'smtp.example.test', port: 587, from: 'a@example.test', username: '', password: '' },
};

describe('setup field errors', () => {
  it('maps each reported field to a local message naming what to change', async () => {
    const client = new AuthClient(rejecting({
      error: {
        code: 'invalid_setup', message: 'ignored',
        fields: [
          { field: 'password', code: 'too_short', message: 'ignored' },
          { field: 'smtp_port', code: 'out_of_range', message: 'ignored' },
        ],
      },
    }));

    const failure = await client.completeOwnerSetup(setupInput).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(AuthRequestError);
    const { fieldErrors } = failure as AuthRequestError;
    expect(fieldErrors.password).toContain('at least 12 characters');
    expect(fieldErrors.smtp_port).toContain('between 1 and 65535');
  });

  it('never shows text the server sent, even when the server supplies a message', async () => {
    const hostile = 'Your password is hunter2 <img src=x onerror=alert(1)>';
    const client = new AuthClient(rejecting({
      error: {
        code: 'invalid_setup', message: hostile,
        fields: [{ field: 'password', code: 'too_short', message: hostile }],
      },
    }));

    const failure = await client.completeOwnerSetup(setupInput).catch((error: unknown) => error) as AuthRequestError;
    expect(failure.message).not.toContain('hunter2');
    expect(failure.fieldErrors.password).not.toContain('hunter2');
    expect(failure.fieldErrors.password).toContain('at least 12 characters');
  });

  it('ignores fields the form does not have and codes it does not know', async () => {
    const client = new AuthClient(rejecting({
      error: {
        code: 'invalid_setup',
        fields: [
          { field: 'not_a_field', code: 'too_short' },
          { field: 'password', code: 'a_code_from_a_newer_server' },
        ],
      },
    }));

    const failure = await client.completeOwnerSetup(setupInput).catch((error: unknown) => error) as AuthRequestError;
    expect(failure.fieldErrors.not_a_field).toBeUndefined();
    // An unknown code still marks the input, rather than silently showing nothing.
    expect(failure.fieldErrors.password).toBe('Check this value and try again.');
  });

  it('distinguishes an unreachable dependency from a value that is wrong', async () => {
    const client = new AuthClient(rejecting({
      error: {
        code: 'dependency_unreachable',
        fields: [{ field: 'smtp_host', code: 'unreachable' }],
      },
    }, 503));

    const failure = await client.completeOwnerSetup(setupInput).catch((error: unknown) => error) as AuthRequestError;
    expect(failure.status).toBe(503);
    expect(failure.code).toBe('dependency_unreachable');
    expect(failure.fieldErrors.smtp_host).toContain('Could not reach the mail server');
  });
});
