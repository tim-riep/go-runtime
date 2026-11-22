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

import { describe, expect, it } from 'vitest';
import { ODataError } from '../odata/index.ts';
import { filenameOf, RequestError } from './api.ts';
import { createRemoteContext, type NotifyTone, serviceUrl } from './context.ts';

interface Call {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string | undefined;
}

function setup(answers: Array<() => Response>) {
  const calls: Call[] = [];
  const saved: Array<{ blob: Blob; filename: string }> = [];
  const opened: Array<[string, Record<string, string>, string | null]> = [];
  const notes: Array<[string, NotifyTone]> = [];
  let signedOut = 0;
  const fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    calls.push({ method: init?.method ?? 'GET', url: String(url), headers: (init?.headers ?? {}) as Record<string, string>, body: init?.body as string | undefined });
    const a = answers.shift();
    if (!a) throw new Error(`unexpected request ${String(url)}`);
    return a();
  }) as typeof globalThis.fetch;
  const context = createRemoteContext({
    language: 'de',
    navigate: (app, params, entity) => opened.push([app, params, entity]),
    notify: (message, tone) => notes.push([message, tone]),
    onSignedOut: () => {
      signedOut++;
    },
    csrfToken: () => 'token-1',
    fetch,
    save: (blob, filename) => saved.push({ blob, filename }),
  });
  return { context, calls, opened, notes, saved, signedOut: () => signedOut };
}

const json = (body: unknown, status = 200) => () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

describe('the context of remote apps', () => {
  it('names services by their URL below /rt/odata', () => {
    expect(serviceUrl('/TRFIN/JOURNAL_ENTRY_SERVICE')).toBe('/rt/odata/trfin/journal_entry_service/');
    expect(serviceUrl('/rt/odata/trbp/partner_service')).toBe('/rt/odata/trbp/partner_service/');
    expect(serviceUrl('/rt/odata/trbp/partner_service/')).toBe('/rt/odata/trbp/partner_service/');
  });

  it('gives one OData client per service with CSRF token and language', async () => {
    const { context, calls } = setup([json({ value: [{ COMPANY: '1000' }] }), json({ FISCAL_YEAR: 2026 })]);
    const client = context.odata('/TRFIN/CONFIGURATION_SERVICE');
    expect(context.odata('/rt/odata/trfin/configuration_service/')).toBe(client);
    expect(client.url).toBe('/rt/odata/trfin/configuration_service/');
    const page = await client.query('Companies');
    expect(page.value).toEqual([{ COMPANY: '1000' }]);
    expect(calls[0]?.url).toBe('/rt/odata/trfin/configuration_service/Companies?lang=de');
    await client.action('POST', { COMPANY: '1000' });
    expect(calls[1]?.method).toBe('POST');
    expect(calls[1]?.headers['X-CSRF-Token']).toBe('token-1');
  });

  it('sends JSON to nodes with the CSRF token for changes only', async () => {
    const { context, calls } = setup([json({ ROWS: [] }), json({ ok: true }), () => new Response(null, { status: 204 })]);
    expect(await context.fetchJSON('/trfin/reports/balances', { body: { COMPANY: '1000', FISCAL_YEAR: 2026 } })).toEqual({ ROWS: [] });
    expect(calls[0]).toMatchObject({ method: 'POST', url: '/trfin/reports/balances', body: '{"COMPANY":"1000","FISCAL_YEAR":2026}' });
    expect(calls[0]?.headers).toMatchObject({ Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': 'token-1', 'Accept-Language': 'de' });
    expect(await context.fetchJSON('/trfin/status')).toEqual({ ok: true });
    expect(calls[1]?.method).toBe('GET');
    expect(calls[1]?.headers['X-CSRF-Token']).toBeUndefined();
    expect(calls[1]?.headers['Content-Type']).toBeUndefined();
    expect(await context.fetchJSON('/trfin/thing', { method: 'delete' })).toBeNull();
    expect(calls[2]?.method).toBe('DELETE');
    expect(calls[2]?.headers['X-CSRF-Token']).toBe('token-1');
  });

  it('turns failures into errors with the message of the node', async () => {
    const { context, signedOut } = setup([
      json({ error: 'the company 9999 does not exist' }, 400),
      json({ error: { code: 'FORBIDDEN', message: 'not allowed' } }, 403),
      () => new Response('plain', { status: 500 }),
      json({ code: 'session-expired', message: 'signed out' }, 401),
    ]);
    const failure = async (p: Promise<unknown>) => p.then(() => null, (e: unknown) => e as RequestError);
    const e1 = await failure(context.fetchJSON('/trfin/reports/balances', { body: { COMPANY: '9999' } }));
    expect(e1).toBeInstanceOf(RequestError);
    expect(e1).toMatchObject({ status: 400, message: 'the company 9999 does not exist' });
    expect(await failure(context.fetchJSON('/x', { body: {} }))).toMatchObject({ status: 403, code: 'FORBIDDEN', message: 'not allowed' });
    expect(await failure(context.fetchJSON('/x'))).toMatchObject({ status: 500, message: 'plain' });
    expect(signedOut()).toBe(0);
    const e4 = await failure(context.fetchJSON('/x'));
    expect(e4?.signedOut).toBe(true);
    expect(signedOut()).toBe(1);
  });

  it('downloads files with the CSRF token and the name of the answer', async () => {
    const xlsx = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
    const { context, calls, saved, signedOut } = setup([
      () => new Response('PK-data', { status: 200, headers: { 'Content-Type': xlsx, 'Content-Disposition': 'attachment; filename="balances-1000.xlsx"' } }),
      () => new Response('plain', { status: 200 }),
      json({ error: 'the company 9999 does not exist' }, 400),
      json({ code: 'session-expired', message: 'signed out' }, 401),
    ]);
    expect(await context.download('/trfin/reports/balances?format=xlsx', { body: { COMPANY: '1000', FISCAL_YEAR: 2026 } })).toBe('balances-1000.xlsx');
    expect(calls[0]).toMatchObject({ method: 'POST', url: '/trfin/reports/balances?format=xlsx', body: '{"COMPANY":"1000","FISCAL_YEAR":2026}' });
    expect(calls[0]?.headers).toMatchObject({ 'Content-Type': 'application/json', 'X-CSRF-Token': 'token-1', 'Accept-Language': 'de' });
    expect(saved[0]?.filename).toBe('balances-1000.xlsx');
    expect(saved[0]?.blob.type).toBe(xlsx);
    expect(await saved[0]?.blob.text()).toBe('PK-data');
    // GET without a name in the answer: the last segment of the path.
    expect(await context.download('/trfin/files/report.txt?x=1')).toBe('report.txt');
    expect(calls[1]?.method).toBe('GET');
    expect(calls[1]?.headers['X-CSRF-Token']).toBeUndefined();
    const failure = async (p: Promise<unknown>) => p.then(() => null, (e: unknown) => e as RequestError);
    expect(await failure(context.download('/trfin/reports/balances?format=xlsx', { body: { COMPANY: '9999' } }))).toMatchObject({
      status: 400,
      message: 'the company 9999 does not exist',
    });
    expect((await failure(context.download('/x', { body: {} })))?.signedOut).toBe(true);
    expect(signedOut()).toBe(1);
    expect(saved).toHaveLength(2);
  });

  it('reads file names of Content-Disposition', () => {
    expect(filenameOf(null)).toBeNull();
    expect(filenameOf('attachment')).toBeNull();
    expect(filenameOf('attachment; filename="open-items-1000.xlsx"')).toBe('open-items-1000.xlsx');
    expect(filenameOf('attachment; filename=plain.xlsx')).toBe('plain.xlsx');
    expect(filenameOf('attachment; filename="a\\"b.txt"')).toBe('a"b.txt');
    expect(filenameOf('attachment; filename="fallback.txt"; filename*=UTF-8\'\'Bilanz%20%C3%BCbersicht.xlsx')).toBe('Bilanz übersicht.xlsx');
  });

  it('reports a lost session of OData requests too', async () => {
    const { context, signedOut } = setup([json({ error: { code: 'UNAUTHORIZED', message: 'sign in' } }, 401)]);
    const e = await context
      .odata('/TRBP/PARTNER_SERVICE')
      .query('Partners')
      .then(
        () => null,
        (x: unknown) => x,
      );
    expect(e).toBeInstanceOf(ODataError);
    expect(signedOut()).toBe(1);
  });

  it('navigates, notifies and names the language', () => {
    const { context, opened, notes } = setup([]);
    expect(context.language).toBe('de');
    context.navigate('/TRFINUI/JOURNAL_ENTRIES', { COMPANY: '1000' });
    context.navigate('/TRFINUI/BALANCES');
    context.navigate('/TRFINUI/JOURNAL_ENTRIES', {}, "JournalEntries(COMPANY='1000',FISCAL_YEAR=2026,DOCUMENT_NO='0000000001')");
    context.notify('Posted');
    context.notify('Failed', 'error');
    expect(opened).toEqual([
      ['/TRFINUI/JOURNAL_ENTRIES', { COMPANY: '1000' }, null],
      ['/TRFINUI/BALANCES', {}, null],
      ['/TRFINUI/JOURNAL_ENTRIES', {}, "JournalEntries(COMPANY='1000',FISCAL_YEAR=2026,DOCUMENT_NO='0000000001')"],
    ]);
    expect(notes).toEqual([
      ['Posted', 'information'],
      ['Failed', 'error'],
    ]);
  });
});
