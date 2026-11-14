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
import { TruiProvider } from '@tim-riep/trui/core';
import { FeedbackHost } from '@tim-riep/trui';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { userEvent } from 'vitest/browser';
import companyService from '../../test/fixtures/company-service.json';
import type { HomeApp } from '../shell/api.ts';
import { textsOf } from '../shell/texts.ts';
import { ListApp } from './ListApp.tsx';

const app: HomeApp = {
  NAME: '/TRFDEMO/COMPANIES',
  TITLE: 'Companies',
  SUBTITLE: null,
  ICON: null,
  TARGET: { KIND: 'LIST', SERVICE: '/TRFIN/COMPANY_SERVICE', ENTITY_SET: 'Companies', URL: '/rt/odata/trfin/company_service/', MODULE: null },
  PARAMETERS: [],
};

const companies = [
  { COMPANY_ID: 'C1', NAME: 'Acme', CITY: 'Berlin', COUNTRY: 'DE', STATUS: 'ACTIVE', TROTTO_OTTO_ID: null, STREET: null, POSTAL_CODE: '', CHANGED_AT: '2026-11-01T10:00:00Z', '@odata.etag': 'W/"1"', Contacts: [{ CONTACT_ID: 'k1', COMPANY_ID: 'C1', NAME: 'Bea', EMAIL: 'bea@example.org' }] },
  { COMPANY_ID: 'C2', NAME: 'Globex', CITY: null, COUNTRY: 'AT', STATUS: 'BLOCKED', TROTTO_OTTO_ID: null, STREET: null, POSTAL_CODE: null, CHANGED_AT: '2026-11-01T10:00:00Z', '@odata.etag': 'W/"1"', Contacts: [] },
];

interface Call {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string | undefined;
}
let calls: Call[] = [];
let patchAnswer: () => Response;

const json = (body: unknown, status = 200, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });

beforeEach(() => {
  calls = [];
  patchAnswer = () => json({ ...companies[0], NAME: 'Acme AG' }, 200, { ETag: 'W/"2"' });
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const c: Call = { method: init?.method ?? 'GET', url, headers: (init?.headers ?? {}) as Record<string, string>, body: init?.body as string | undefined };
    calls.push(c);
    const u = new URL(url, 'http://x');
    const rest = decodeURIComponent(u.pathname.slice('/rt/odata/trfin/company_service/'.length));
    if (rest === '$metadata') return json(companyService);
    if (rest === 'Companies' && c.method === 'GET') {
      const filter = u.searchParams.get('$filter');
      const rows = filter?.includes("COUNTRY eq 'AT'") ? companies.filter((x) => x.COUNTRY === 'AT') : companies;
      return json({ value: rows.map(({ Contacts: _, ...r }) => r), '@odata.count': rows.length });
    }
    if (rest === "Companies('C1')" && c.method === 'GET') return json(companies[0], 200, { ETag: 'W/"1"' });
    if (rest === "Companies('C1')" && c.method === 'PATCH') return patchAnswer();
    if (rest === "Companies('C2')" && c.method === 'DELETE') return new Response(null, { status: 204 });
    return json({ error: { code: 'NOT_FOUND', message: rest } }, 404);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function show(entity: string | null, onNavigate = vi.fn()) {
  render(
    <TruiProvider>
      <ListApp app={app} entity={entity} params={{}} texts={textsOf('en')} onNavigate={onNavigate} onSignedOut={vi.fn()} />
      <FeedbackHost />
    </TruiProvider>,
  );
  return onNavigate;
}

describe('list page', () => {
  it('shows the columns of the annotations with fixed value texts and the count', async () => {
    show(null);
    const table = await screen.findByRole('grid', { name: 'Companies' });
    await within(table).findByText('Globex');
    expect(within(table).getByRole('columnheader', { name: /OTTO partner/ })).toBeTruthy();
    expect(within(table).getByText('Blocked for postings')).toBeTruthy();
    expect(screen.getByText('2')).toBeTruthy();
    const list = calls.find((c) => c.url.includes('Companies?'));
    expect(list?.url).toContain('$orderby=NAME');
    expect(list?.url).toContain('$count=true');
  });

  it('filters by the filter bar and opens an entity', async () => {
    const nav = show(null);
    await screen.findByText('Globex');
    await userEvent.click(screen.getByRole('button', { name: 'Go' }));
    const filtered = calls.filter((c) => c.url.includes('Companies?'));
    expect(filtered.length).toBeGreaterThan(0);
    await userEvent.click(await screen.findByText('Acme'));
    expect(nav).toHaveBeenCalledWith(`Companies(${encodeURIComponent("'C1'")})`);
  });

  it('deletes selected entries with their ETag after confirmation', async () => {
    show(null);
    const table = await screen.findByRole('grid', { name: 'Companies' });
    const row = (await within(table).findByText('Globex')).closest('tr, [role="row"]') as HTMLElement;
    await userEvent.click(within(row).getByRole('checkbox'));
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('alertdialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE')).toBe(true));
    const del = calls.find((c) => c.method === 'DELETE');
    expect(del?.headers['If-Match']).toBe('W/"1"');
  });
});

describe('detail page', () => {
  it('shows sections, groups and the table of contacts', async () => {
    show("Companies('C1')");
    expect(await screen.findByRole('heading', { name: 'Acme', level: 1 })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Marketplace' })).toBeTruthy();
    expect(within(screen.getByRole('region', { name: 'Contacts' })).getByText('bea@example.org')).toBeTruthy();
    expect(screen.getByText('Active')).toBeTruthy();
  });

  it('saves only changed fields with the ETag and keeps null apart from the empty string', async () => {
    show("Companies('C1')");
    await screen.findByRole('heading', { name: 'Acme', level: 1 });
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const name = screen.getByLabelText(/^Name/);
    await userEvent.fill(name, 'Acme AG');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(JSON.parse(patch?.body ?? '{}')).toEqual({ NAME: 'Acme AG' });
    expect(patch?.headers['If-Match']).toBe('W/"1"');
  });

  it('reports a conflict with a reload', async () => {
    patchAnswer = () => json({ error: { code: 'ETAG_MISMATCH', message: 'changed' } }, 412);
    show("Companies('C1')");
    await screen.findByRole('heading', { name: 'Acme', level: 1 });
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    await userEvent.fill(screen.getByLabelText(/^Name/), 'X');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(/Someone else changed this entry/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Reload' })).toBeTruthy();
  });

  it('shows messages of the server at their field', async () => {
    patchAnswer = () => json({ error: { code: 'VALIDATION', message: 'the name is taken', target: 'NAME' } }, 400);
    show("Companies('C1')");
    await screen.findByRole('heading', { name: 'Acme', level: 1 });
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    await userEvent.fill(screen.getByLabelText(/^Name/), 'Taken');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect((await screen.findAllByText('the name is taken')).length).toBeGreaterThan(0);
  });
});
