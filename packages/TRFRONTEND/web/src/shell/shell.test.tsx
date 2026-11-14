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


import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { userEvent } from 'vitest/browser';
import type { Home, Session } from './api.ts';
import { App } from './App.tsx';

const session: Session = {
  user: 'ALICE',
  name: null,
  tenants: ['T1', 'T2'],
  tenant: 'T1',
  language: 'en',
  languages: [
    { code: 'en', name: 'English' },
    { code: 'de', name: 'German' },
  ],
  csrfToken: 'token-1',
  expiresAt: '2026-11-14T20:00:00Z',
};

const home: Home = {
  SPACES: [
    {
      NAME: '/TRFDEMO/SPACE',
      TITLE: 'Finance',
      SHIPPED: true,
      PAGES: [{ NAME: '/TRFDEMO/PAGE', TITLE: 'Overview', SECTIONS: [{ ID: 'MAIN', TITLE: 'Companies', TILES: ['/TRFDEMO/TILE', '/TRFDEMO/METRIC'] }] }],
    },
  ],
  TILES: [
    { NAME: '/TRFDEMO/TILE', APP: '/TRFDEMO/COMPANIES', KIND: 'SIMPLE', SIZE: 'SMALL', TITLE: 'Companies', SUBTITLE: 'Manage', FOOTER: null, UNIT: null, CONTENT: null, ICON: null, TONE: null },
    { NAME: '/TRFDEMO/METRIC', APP: '/TRFDEMO/COMPANIES', KIND: 'METRIC', SIZE: 'SMALL', TITLE: 'Active companies', SUBTITLE: null, FOOTER: null, UNIT: null, CONTENT: null, ICON: null, TONE: 'GOOD' },
  ],
  APPS: [
    {
      NAME: '/TRFDEMO/COMPANIES',
      TITLE: 'Companies',
      SUBTITLE: null,
      ICON: null,
      TARGET: { KIND: 'LIST', SERVICE: '/TRFDEMO/SERVICE', ENTITY_SET: 'Companies', URL: '/rt/odata/trfdemo/service/', MODULE: null },
      PARAMETERS: [],
    },
  ],
};

interface Call {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string | undefined;
}

let calls: Call[] = [];
let signedIn = false;

function answer(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

beforeEach(() => {
  calls = [];
  signedIn = false;
  window.location.hash = '#/';
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const c: Call = { method: init?.method ?? 'GET', url, headers: (init?.headers ?? {}) as Record<string, string>, body: init?.body as string | undefined };
    calls.push(c);
    if (url === '/rt/session') {
      if (c.method === 'POST') {
        const b = JSON.parse(c.body ?? '{}') as { user: string; password: string };
        if (b.password !== 'secret') return answer(401, { code: 'sign-in-failed', message: 'no' });
        signedIn = true;
        return answer(200, session);
      }
      if (c.method === 'DELETE') {
        signedIn = false;
        return new Response(null, { status: 204 });
      }
      if (c.method === 'PATCH') return answer(200, { ...session, ...JSON.parse(c.body ?? '{}') });
      return signedIn ? answer(200, session) : answer(401, { code: 'session-required', message: 'no' });
    }
    if (!signedIn) return answer(401, { code: 'session-expired', message: 'expired' });
    if (url === '/trfrontend/api/home') return answer(200, home);
    if (url.startsWith('/trfrontend/api/tile-value')) return answer(200, { NAME: '/TRFDEMO/METRIC', VALUE: 42 });
    return answer(404, { code: 'not-found', message: url });
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function signIn(password = 'secret') {
  render(<App />);
  const user = await screen.findByLabelText(/^User/);
  (user as HTMLInputElement).focus();
  await userEvent.fill(user, 'ALICE');
  await userEvent.fill(screen.getByLabelText(/^Password/), password);
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
}

describe('shell', () => {
  it('signs in and shows the spaces with their tiles and metric values', async () => {
    await signIn();
    expect(await screen.findByRole('tab', { name: 'Finance' })).toBeTruthy();
    expect(await screen.findByText('Manage')).toBeTruthy();
    await waitFor(() => expect(screen.getByText('42')).toBeTruthy());
    const post = calls.find((c) => c.method === 'POST');
    expect(JSON.parse(post?.body ?? '{}')).toEqual({ user: 'ALICE', password: 'secret' });
  });

  it('says when the sign-in fails', async () => {
    await signIn('wrong');
    expect(await screen.findByText(/password is wrong/)).toBeTruthy();
  });

  it('opens an app from a tile through the address', async () => {
    await signIn();
      await userEvent.click(await screen.findByText('Manage'));
    await waitFor(() => expect(window.location.hash).toBe('#/app/TRFDEMO/COMPANIES'));
  });

  it('sends the CSRF token when it signs out', async () => {
    await signIn();
    await screen.findByRole('tab', { name: 'Finance' });
    const { signOut } = await import('./api.ts');
    await signOut();
    const del = calls.find((c) => c.method === 'DELETE');
    expect(del?.headers['X-CSRF-Token']).toBe('token-1');
  });
});
