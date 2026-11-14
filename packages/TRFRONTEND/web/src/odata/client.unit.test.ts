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

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { ODataClient } from './client.ts';
import { ODataError } from './errors.ts';
import { annotation, parseMetadata } from './metadata.ts';
import { changes, keyPredicate, literal } from './values.ts';

const companyService = JSON.parse(readFileSync(new URL('../../test/fixtures/company-service.json', import.meta.url), 'utf8'));

interface Call {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string | undefined;
}

/** A fetch that answers from a list of responses and records the requests. */
function fake(responses: Array<(c: Call) => Response>) {
  const calls: Call[] = [];
  const f = async (url: string | URL | Request, init?: RequestInit) => {
    const c: Call = {
      method: init?.method ?? 'GET',
      url: String(url),
      headers: (init?.headers ?? {}) as Record<string, string>,
      body: init?.body as string | undefined,
    };
    calls.push(c);
    const r = responses.shift();
    if (!r) {
      throw new Error(`unexpected request ${c.method} ${c.url}`);
    }
    return r(c);
  };
  return { calls, fetch: f as typeof fetch };
}

const json = (body: unknown, status = 200, headers: Record<string, string> = {}) => () =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });
const metadata = json(companyService);

describe('metadata', () => {
  const m = parseMetadata(companyService);

  it('reads entity sets, types, keys and navigations', () => {
    const companies = m.entitySets.Companies;
    expect(companies?.type.name).toBe('TRFIN.TRFIN_COMPANIES');
    expect(companies?.type.key).toEqual(['COMPANY_ID']);
    const capital = companies?.type.properties.find((p) => p.name === 'CAPITAL');
    expect(capital).toMatchObject({ type: 'Edm.Decimal', precision: 15, scale: 2, nullable: true });
    expect(companies?.type.navigations.map((n) => n.name)).toContain('Contacts');
    expect(companies?.bindings.Country).toBe('Countries');
  });

  it('reads the annotations of the kernel and of TRFRONTEND', () => {
    const t = m.entityTypes['TRFIN.TRFIN_COMPANIES'];
    expect(t).toBeDefined();
    const name = t?.properties.find((p) => p.name === 'NAME');
    expect(annotation(m, name?.annotations ?? {}, 'Core.Description')).toBe('Name');
    const list = annotation(m, t?.annotations ?? {}, 'TRFRONTEND.UI.ListPage') as { Columns: Array<{ Property: { $PropertyPath: string } }> };
    expect(list.Columns.map((c) => c.Property.$PropertyPath)).toEqual(['COMPANY_ID', 'NAME', 'TROTTO_OTTO_ID', 'CITY', 'COUNTRY', 'STATUS']);
    expect(annotation(m, t?.annotations ?? {}, 'Frontend.Header')).toBeDefined();
  });

  it('reads bound actions and their parameters', () => {
    const block = m.actions.find((a) => a.name === 'TRFIN.BLOCK');
    expect(block?.boundTo).toBe('TRFIN.TRFIN_COMPANIES');
    expect(block?.parameters.map((p) => p.name)).toEqual(['TEXT']);
  });
});

describe('values', () => {
  it('writes literals with quotes, exact numbers and null', () => {
    expect(literal("O'Brien", 'Edm.String')).toBe("'O''Brien'");
    expect(literal('', 'Edm.String')).toBe("''");
    expect(literal(null, 'Edm.String')).toBe('null');
    expect(literal('12345678901234567890.12', 'Edm.Decimal')).toBe('12345678901234567890.12');
    expect(literal('9007199254740993', 'Edm.Int64')).toBe('9007199254740993');
    expect(literal(true, 'Edm.Boolean')).toBe('true');
  });

  it('builds key predicates', () => {
    const m = parseMetadata(companyService);
    const t = m.entityTypes['TRFIN.TRFIN_COMPANIES'];
    if (!t) throw new Error('no type');
    expect(keyPredicate(t, { COMPANY_ID: 'A/1' })).toBe(`(${encodeURIComponent("'A/1'")})`);
    const multi = { ...t, key: ['NAME', 'POSITION'], properties: [...t.properties, { ...t.properties[0], name: 'POSITION', type: 'Edm.Int32' }] };
    expect(keyPredicate(multi as typeof t, { NAME: 'x', POSITION: 2 })).toBe("(NAME='x',POSITION=2)");
  });

  it('keeps null and the empty string apart in changes', () => {
    expect(changes({ A: null, B: '', C: 'x' }, { A: '', B: null, C: 'x' })).toEqual({ A: '', B: null });
    expect(changes({ D: '1.50' }, { D: '1.5' })).toEqual({ D: '1.5' });
  });
});

describe('client', () => {
  it('asks for exact numbers and keeps decimals and Int64 as strings', async () => {
    const f = fake([json({ value: [{ COMPANY_ID: 'C1', CAPITAL: '12345678901234567.89', NOTE: '', CITY: null }], '@odata.count': '1' })]);
    const c = new ODataClient('/rt/odata/trfin/company_service', { fetch: f.fetch, language: () => 'de' });
    const page = await c.query('Companies', { count: true, filter: "NAME eq 'A'", top: 10 });
    expect(f.calls[0]?.url).toBe(`/rt/odata/trfin/company_service/Companies?$filter=${encodeURIComponent("NAME eq 'A'")}&$top=10&$count=true&lang=de`);
    expect(f.calls[0]?.headers.Accept).toBe('application/json;IEEE754Compatible=true');
    expect(page.count).toBe(1);
    expect(page.value[0]).toEqual({ COMPANY_ID: 'C1', CAPITAL: '12345678901234567.89', NOTE: '', CITY: null });
  });

  it('sends the ETag and the CSRF token with a change and reads the new ETag', async () => {
    const f = fake([metadata, json({ COMPANY_ID: 'C1', NAME: 'New', NOTE: null }, 200, { ETag: 'W/"2"' })]);
    const c = new ODataClient('/rt/odata/trfin/company_service/', { fetch: f.fetch, csrfToken: () => 'token-1' });
    const r = await c.update('Companies', { COMPANY_ID: 'C1' }, { NAME: 'New', NOTE: null }, 'W/"1"');
    const call = f.calls[1];
    expect(call?.method).toBe('PATCH');
    expect(call?.url).toBe(`/rt/odata/trfin/company_service/Companies(${encodeURIComponent("'C1'")})`);
    expect(call?.headers['If-Match']).toBe('W/"1"');
    expect(call?.headers['X-CSRF-Token']).toBe('token-1');
    expect(call?.headers['Content-Type']).toBe('application/json;IEEE754Compatible=true');
    expect(JSON.parse(call?.body ?? '{}')).toEqual({ NAME: 'New', NOTE: null });
    expect(r.etag).toBe('W/"2"');
    expect(r.entity?.NOTE).toBeNull();
  });

  it('reports a conflict on 412 and messages at properties', async () => {
    const f = fake([
      metadata,
      json({ error: { code: 'ETAG_MISMATCH', message: 'changed by another user' } }, 412),
      json({ error: { code: 'VALIDATION', message: 'invalid', target: 'COUNTRY', details: [{ code: 'MANDATORY', message: 'NAME is missing', target: 'NAME' }] } }, 400),
    ]);
    const c = new ODataClient('/s/', { fetch: f.fetch });
    const conflict = await c.update('Companies', { COMPANY_ID: 'C1' }, { NAME: 'x' }, 'W/"1"').catch((e: unknown) => e);
    expect(conflict).toBeInstanceOf(ODataError);
    expect((conflict as ODataError).conflict).toBe(true);
    const invalid = (await c.create('Companies', { COMPANY_ID: 'C2' }).catch((e: unknown) => e)) as ODataError;
    expect(invalid.status).toBe(400);
    expect(invalid.messagesFor('COUNTRY').map((m) => m.message)).toEqual(['invalid']);
    expect(invalid.messagesFor('NAME').map((m) => m.message)).toEqual(['NAME is missing']);
  });

  it('reads Core.Messages of successful changes', async () => {
    const f = fake([json({ COMPANY_ID: 'C3', '@Core.Messages': [{ code: 'WARNING', message: 'check the city', severity: 'warning', target: 'CITY' }] }, 201)]);
    const c = new ODataClient('/s/', { fetch: f.fetch, csrfToken: () => 't' });
    const r = await c.create('Companies', { COMPANY_ID: 'C3' });
    expect(r.messages).toEqual([{ code: 'WARNING', message: 'check the city', severity: 'warning', target: 'CITY' }]);
    expect(f.calls[0]?.headers.Prefer).toBe('return=representation');
  });

  it('reads errors that are no OData errors', async () => {
    const f = fake([() => new Response(JSON.stringify({ code: 'session-expired', message: 'The session expired.' }), { status: 401 })]);
    const c = new ODataClient('/s/', { fetch: f.fetch });
    const e = (await c.count('Companies').catch((x: unknown) => x)) as ODataError;
    expect(e.unauthorized).toBe(true);
    expect(e.code).toBe('session-expired');
  });
});
