// Copyright 2026 The go-runtime Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.


import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { FeedbackHost } from '@tim-riep/trui';
import { TruiProvider } from '@tim-riep/trui/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { userEvent } from 'vitest/browser';
import { parseValues } from './api.ts';
import { RolesApp } from './RolesApp.tsx';
import { roleTexts } from './texts.ts';

interface Call {
  method: string;
  url: string;
  body: string | undefined;
}
let calls: Call[] = [];

const json = (body: unknown, status = 200) => new Response(body === null ? null : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const local = {
  NAME: '/LOCAL/CLERKS',
  COMPOSITE: false,
  TEXTS: [{ LANGUAGE: 'en', LABEL: 'Clerks', DESCRIPTION: null }],
  ROLES: null,
  AUTHORIZATIONS: [],
  PACKAGE_ID: null,
  ALL: false,
  EFFECT: [
    {
      OBJECT: '/TRBASE/HTTP_SERVICE',
      FIELDS: [{ FIELD: 'SERVICE_PATH', VALUES: [{ KIND: 'PATTERN', LOW: '/trfrontend*', HIGH: null }] }],
      ROLE_NAME: '/LOCAL/CLERKS',
      COMPOSITE: null,
    },
  ],
  USED_BY: [],
  ASSIGNMENTS: 1,
};

beforeEach(() => {
  calls = [];
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const c: Call = { method: init?.method ?? 'GET', url, body: init?.body as string | undefined };
    calls.push(c);
    const u = new URL(url, 'http://x');
    const path = u.pathname.replace('/trfrontend/api/', '');
    if (path === 'roles') return json([{ NAME: '/LOCAL/CLERKS', PACKAGE_ID: null, COMPOSITE: false, LABEL: 'Clerks', INCOMPLETE: 0 }, { NAME: '/TRFRONTEND/USER', PACKAGE_ID: 'TRFRONTEND', COMPOSITE: false, LABEL: 'User', INCOMPLETE: 0 }]);
    if (path === 'role' && c.method === 'GET') return json(local);
    if (path === 'role' && c.method === 'POST') return json({}, 201);
    if (path === 'role' && c.method === 'DELETE') {
      return u.searchParams.get('confirm') === 'true' ? json(null, 204) : json({ code: 'confirmation-required', message: 'the role is assigned to 1 user' }, 409);
    }
    if (path === 'assignments' && c.method === 'GET') return json([{ ID: 'a1', USER_NAME: 'bob', ROLE_NAME: '/LOCAL/CLERKS', TENANT: 'ACME', VALID_FROM: null, VALID_TO: null, CREATED_AT: null, CREATED_BY: null }]);
    if (path === 'users') return json([{ USER_NAME: 'bob', NAME: null, STATUS: 'active', LANGUAGE: 'en', DEFAULT_TENANT: 'ACME', TENANTS: ['ACME'] }]);
    if (path === 'authorization-objects') return json([{ NAME: '/TRBASE/HTTP_SERVICE', PACKAGE_ID: 'TRBASE', LABEL: 'HTTP service', FIELDS: [{ NAME: 'SERVICE_PATH', IS_ACTIVITY: false, LABEL: 'Path' }] }]);
    return json({ code: 'not-found', message: path }, 404);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function show() {
  render(
    <TruiProvider>
      <RolesApp texts={roleTexts('en')} language="en" />
      <FeedbackHost />
    </TruiProvider>,
  );
}

describe('role page', () => {
  it('lists roles and shows a role with authorizations and assignments', async () => {
    show();
    await userEvent.click(await screen.findByText('/LOCAL/CLERKS'));
    const auths = await screen.findByRole('region', { name: 'Authorizations' });
    expect(within(auths).getByText('SERVICE_PATH: /trfrontend*')).toBeTruthy();
    expect(within(screen.getByRole('region', { name: 'Assignments' })).getByText('bob')).toBeTruthy();
  });

  it('creates a local role with authorizations', async () => {
    show();
    await userEvent.click(await screen.findByRole('button', { name: 'Create local role' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.fill(within(dialog).getByLabelText(/^Name/), '/LOCAL/AUDIT');
    await userEvent.fill(within(dialog).getByLabelText(/^Label/), 'Audit');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const body = JSON.parse(calls.find((c) => c.method === 'POST')?.body ?? '{}');
    expect(body).toMatchObject({ NAME: '/LOCAL/AUDIT', COMPOSITE: false, TEXTS: [{ LANGUAGE: 'en', LABEL: 'Audit', DESCRIPTION: null }], AUTHORIZATIONS: [] });
  });

  it('asks before it deletes a role in use and sends the confirmation', async () => {
    show();
    await userEvent.click(await screen.findByText('/LOCAL/CLERKS'));
    await userEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText('the role is assigned to 1 user')).toBeTruthy();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.includes('confirm=true'))).toBe(true));
  });
});

describe('values of authorizations', () => {
  it('reads values, ranges and patterns', () => {
    expect(parseValues('A, B..F, /x*,')).toEqual([
      { KIND: 'VALUE', LOW: 'A', HIGH: null },
      { KIND: 'RANGE', LOW: 'B', HIGH: 'F' },
      { KIND: 'PATTERN', LOW: '/x*', HIGH: null },
    ]);
  });
});
