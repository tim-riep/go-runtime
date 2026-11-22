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
import { FeedbackHost } from '@tim-riep/trui';
import { TruiProvider } from '@tim-riep/trui/core';
import { useEffect, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { userEvent } from 'vitest/browser';
import type { HomeApp } from './api.ts';
import { AppHost } from './AppHost.tsx';
import type { RemoteContext, RemoteProps } from './context.ts';
import { textsOf } from './texts.ts';

const contexts: RemoteContext[] = [];

/** A remote that shows its parameters and uses every part of its context. */
function Balances({ params, context }: RemoteProps) {
  const [rows, setRows] = useState<string>('');
  contexts.push(context);
  useEffect(() => {
    context.fetchJSON<{ ROWS: Array<{ ACCOUNT: string }> }>('/trfin/reports/balances', { body: { COMPANY: params.COMPANY } }).then(
      (r) => setRows(r.ROWS.map((x) => x.ACCOUNT).join(' ')),
      (e: unknown) => setRows(String(e)),
    );
  }, [context, params.COMPANY]);
  return (
    <div>
      <p>company {params.COMPANY} in {context.language}</p>
      <p>accounts {rows}</p>
      <button type="button" onClick={() => context.navigate('/TRFINUI/LINE_ITEMS', { ACCOUNT: '100000' })}>
        drill down
      </button>
      <button type="button" onClick={() => context.notify('exported', 'success')}>
        export
      </button>
    </div>
  );
}

vi.mock('./remote.ts', () => ({
  loadRemoteApp: vi.fn(async () => Balances),
}));

const app: HomeApp = {
  NAME: '/TRFINUI/BALANCES',
  TITLE: 'Balances',
  SUBTITLE: null,
  ICON: null,
  TARGET: { KIND: 'REMOTE', SERVICE: null, ENTITY_SET: null, URL: '/trfinui/remote/mf-manifest.json', MODULE: './Balances' },
  PARAMETERS: [{ NAME: 'COMPANY', VALUE: '1000' }],
};

let requests: Array<{ url: string; headers: Record<string, string>; body: string | undefined }> = [];

beforeEach(() => {
  requests = [];
  contexts.length = 0;
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    requests.push({ url, headers: (init?.headers ?? {}) as Record<string, string>, body: init?.body as string | undefined });
    return new Response(JSON.stringify({ ROWS: [{ ACCOUNT: '100000' }, { ACCOUNT: '400000' }] }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('remote apps', () => {
  it('get their parameters and the context of the shell', async () => {
    const opened: Array<[string, Record<string, string>]> = [];
    const texts = textsOf('de');
    const view = (onOpen: (a: string, p: Record<string, string>) => void) => (
      <TruiProvider>
        <AppHost app={app} params={{}} entity={null} texts={texts} language="de" onNavigate={() => {}} onOpen={onOpen} onSignedOut={() => {}} />
        <FeedbackHost />
      </TruiProvider>
    );
    const { rerender } = render(view((a, p) => opened.push([a, p])));
    expect(await screen.findByText('company 1000 in de')).toBeTruthy();
    expect(await screen.findByText('accounts 100000 400000')).toBeTruthy();
    expect(requests[0]?.url).toBe('/trfin/reports/balances');
    expect(requests[0]?.body).toBe('{"COMPANY":"1000"}');
    expect(requests[0]?.headers['Accept-Language']).toBe('de');

    // Another callback of the shell keeps the context, so effects of the
    // remote do not run again.
    const second: Array<[string, Record<string, string>]> = [];
    rerender(view((a, p) => second.push([a, p])));
    await userEvent.click(screen.getByRole('button', { name: 'drill down' }));
    expect(opened).toEqual([]);
    expect(second).toEqual([['/TRFINUI/LINE_ITEMS', { ACCOUNT: '100000' }]]);
    expect(new Set(contexts).size).toBe(1);
    expect(requests).toHaveLength(1);

    await userEvent.click(screen.getByRole('button', { name: 'export' }));
    await waitFor(() => expect(screen.getByText('exported')).toBeTruthy());
  });
});
