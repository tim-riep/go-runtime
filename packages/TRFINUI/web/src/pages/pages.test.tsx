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
// The pages with a context of the shell that answers from memory: the
// journal entry with its running balance and the messages of CHECK at the
// cells, the balance sheet with expand, collapse, export and drill-down,
// and the confirmation of number ranges.

import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { FeedbackHost } from '@tim-riep/trui';
import { TruiProvider } from '@tim-riep/trui/core';
import type { ComponentType } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { userEvent } from 'vitest/browser';
import type { Entity, JSONInit, ODataClient, Query, RemoteContext, RemoteProps, Value } from '../shared/context.ts';
import BalanceSheetPage from './BalanceSheetPage.tsx';
import JournalEntryPage from './JournalEntryPage.tsx';
import NumberRangePage from './NumberRangePage.tsx';

const sets: Record<string, Entity[]> = {
  Companies: [{ COMPANY: '1000', NAME: 'Sample', CURRENCY: 'EUR', CHART: 'TRAC', COUNTRY_CHART: null }],
  Ledgers: [{ LEDGER: '0L', LedgerTexts: [] }],
  DocumentTypes: [{ DOCUMENT_TYPE: 'SA', DocumentTypeTexts: [{ LANGUAGE: 'en', LABEL: 'Accounts' }] }],
  StatementStructures: [{ STRUCTURE: 'BSOP', CHART: 'TRAC', StatementStructureTexts: [] }],
  CompanyAccounts: [
    { COMPANY: '1000', ACCOUNT: '110000', RECONCILIATION: 'NONE', BLOCKED: false },
    { COMPANY: '1000', ACCOUNT: '400000', RECONCILIATION: 'NONE', BLOCKED: false },
  ],
  Accounts: [
    { CHART: 'TRAC', ACCOUNT: '110000', AccountTexts: [{ LANGUAGE: 'en', SHORT_TEXT: 'Bank' }] },
    { CHART: 'TRAC', ACCOUNT: '400000', AccountTexts: [{ LANGUAGE: 'en', SHORT_TEXT: 'Revenue' }] },
  ],
  Customers: [],
  Suppliers: [],
  Partners: [],
};

interface Call {
  kind: 'action' | 'json' | 'download' | 'navigate' | 'notify';
  name: string;
  body?: unknown;
  extra?: unknown;
}

/** A context whose services answer from sets and from the answers given per action or path. */
function fakeContext(answers: Record<string, Array<(body: unknown) => unknown>>) {
  const calls: Call[] = [];
  const answer = (key: string, body: unknown) => {
    const next = answers[key]?.shift();
    if (!next) throw new Error(`unexpected ${key}`);
    return next(body);
  };
  const client: ODataClient = {
    async query(set: string, _q?: Query) {
      return { value: sets[set] ?? [], count: null, nextLink: null };
    },
    async page() {
      return { value: [], count: null, nextLink: null };
    },
    async entityPath(set: string, key: Entity) {
      return `${set}(${Object.entries(key)
        .map(([k, v]) => `${k}=${typeof v === 'string' ? `'${v}'` : String(v)}`)
        .join(',')})`;
    },
    async action(name: string, params: Record<string, Value>) {
      calls.push({ kind: 'action', name, body: params });
      return { entity: answer(name, params) as Entity, etag: null };
    },
  };
  const context: RemoteContext = {
    language: 'en',
    odata: () => client,
    async fetchJSON<T>(path: string, init: JSONInit = {}) {
      calls.push({ kind: 'json', name: `${init.method ?? (init.body !== undefined ? 'POST' : 'GET')} ${path}`, body: init.body });
      return answer(`${init.method ?? (init.body !== undefined ? 'POST' : 'GET')} ${path}`, init.body) as T;
    },
    async download(path: string, init: JSONInit = {}) {
      calls.push({ kind: 'download', name: path, body: init.body });
      return 'balance-sheet-1000.xlsx';
    },
    navigate(app: string, params: Record<string, string> = {}, entity: string | null = null) {
      calls.push({ kind: 'navigate', name: app, body: params, extra: entity });
    },
    notify(message: string) {
      calls.push({ kind: 'notify', name: message });
    },
  };
  return { context, calls };
}

function show(Page: ComponentType<RemoteProps>, context: RemoteContext, params: Record<string, string> = {}) {
  return render(
    <TruiProvider>
      <Page params={params} context={context} />
      <FeedbackHost />
    </TruiProvider>,
  );
}

async function choose(label: string, option: RegExp) {
  await userEvent.click(await screen.findByRole('combobox', { name: label }));
  await userEvent.click(await screen.findByRole('option', { name: option }));
}

async function enter(label: string, value: string) {
  const box = screen.getByRole('textbox', { name: label });
  await userEvent.fill(box, value);
  await userEvent.tab();
}

/** A failure of an OData action as the client of the shell throws it. */
function odataError(message: string, details: string[]) {
  return Object.assign(new Error(message), { status: 400, code: 'VALIDATION', details: details.map((m) => ({ code: 'VALIDATION', message: m, target: null })) });
}

afterEach(() => {
  cleanup();
});

describe('the journal entry page', () => {
  it('keeps the running balance, shows the messages of CHECK at the cells and posts', async () => {
    const { context, calls } = fakeContext({
      CHECK: [
        () => ({ FISCAL_YEAR: 2026, PERIOD: 6, MESSAGES: [{ ITEM_NO: 2, FIELD: 'ACCOUNT', TEXT: 'the account 999999 does not exist in the company 1000' }] }),
        () => ({ FISCAL_YEAR: 2026, PERIOD: 6, MESSAGES: [{ ITEM_NO: 2, FIELD: 'ACCOUNT', TEXT: 'the account 999999 does not exist in the company 1000' }] }),
      ],
      POST: [
        () => {
          throw odataError('The entity is not valid.', ['the account 999999 does not exist in the company 1000']);
        },
        () => ({ COMPANY: '1000', FISCAL_YEAR: 2026, DOCUMENT_NO: '0000000042' }),
      ],
    });
    show(JournalEntryPage, context, { COMPANY: '1000', DOCUMENT_TYPE: 'SA' });
    await screen.findByRole('grid', { name: 'Items' });
    await enter('Posting date', '06/15/2026');

    // Nothing is sent while fields are missing.
    await userEvent.click(screen.getByRole('button', { name: 'Check' }));
    await screen.findAllByText('Account is missing.');
    expect(calls.filter((c) => c.kind === 'action')).toEqual([]);

    await enter('Account of item 1', '110000');
    await enter('Amount of item 1', '100.10');
    await enter('Account of item 2', '999999');
    await enter('Amount of item 2', '0.10');
    await waitFor(() => expect(screen.getByRole('status', { name: 'Running balance of item 2' }).textContent).toBe('100.00'));
    await screen.findByText(/Debit and credit differ by/);
    await enter('Amount of item 2', '100.10');
    await waitFor(() => expect(screen.getByRole('status', { name: 'Running balance of item 2' }).textContent).toBe('0.00'));

    await userEvent.click(screen.getByRole('button', { name: 'Check' }));
    const cell = screen.getByRole('textbox', { name: 'Account of item 2' });
    await waitFor(() => expect(cell.getAttribute('aria-invalid')).toBe('true'));
    expect(screen.getByRole('textbox', { name: 'Account of item 1' }).getAttribute('aria-invalid')).not.toBe('true');
    expect(calls[0]?.body).toMatchObject({
      COMPANY: '1000',
      DOCUMENT_TYPE: 'SA',
      POSTING_DATE: '2026-06-15',
      REFERENCE: null,
      ITEMS: [
        { ITEM_NO: 1, ACCOUNT_KIND: 'ACCOUNT', ACCOUNT: '110000', PARTNER: null, DEBIT_CREDIT: 'D', AMOUNT: '100.10' },
        { ITEM_NO: 2, ACCOUNT: '999999', DEBIT_CREDIT: 'C', AMOUNT: '100.10' },
      ],
    });

    // A failed POST is checked again to place its messages.
    await userEvent.click(screen.getByRole('button', { name: 'Post' }));
    await waitFor(() => expect(calls.filter((c) => c.kind === 'action').map((c) => c.name)).toEqual(['CHECK', 'POST', 'CHECK']));
    await waitFor(() => expect(cell.getAttribute('aria-invalid')).toBe('true'));

    await enter('Account of item 2', '400000');
    await userEvent.click(screen.getByRole('button', { name: 'Post' }));
    await screen.findByRole('button', { name: 'Open journal entry' });
    expect(calls.find((c) => c.kind === 'notify')?.name).toBe('Journal entry 0000000042 of 2026 posted.');
    await userEvent.click(screen.getByRole('button', { name: 'Open journal entry' }));
    await waitFor(() =>
      expect(calls.find((c) => c.kind === 'navigate')).toEqual({
        kind: 'navigate',
        name: '/TRFINUI/JOURNAL_ENTRIES',
        body: {},
        extra: "JournalEntries(COMPANY='1000',FISCAL_YEAR=2026,DOCUMENT_NO='0000000042')",
      }),
    );
  });
});

describe('the balance sheet page', () => {
  const report = {
    LEDGER: '0L',
    COMPANY: '1000',
    CURRENCY: 'EUR',
    KEY_DATE: '2026-12-31',
    CHART: 'TRAC',
    STRUCTURE: 'BSOP',
    TOTAL_ASSETS: '100.00',
    TOTAL_LIABILITIES: '100.00',
    RESULT: '0.00',
    DIFFERENCE: '0.00',
    BALANCED: true,
    UNASSIGNED: [],
    PROBLEMS: [],
    ROWS: [
      { LEVEL: 1, NODE: 'ASSETS', PARENT_NODE: null, NODE_KIND: 'NORMAL', SIDE: 'ASSETS', ACCOUNT: null, LABEL: 'Assets', AMOUNT: '100.00' },
      { LEVEL: 2, NODE: 'CASH', PARENT_NODE: 'ASSETS', NODE_KIND: 'NORMAL', SIDE: 'ASSETS', ACCOUNT: null, LABEL: 'Cash', AMOUNT: '100.00' },
      { LEVEL: 3, NODE: 'CASH', PARENT_NODE: 'ASSETS', NODE_KIND: 'NORMAL', SIDE: 'ASSETS', ACCOUNT: '110000', LABEL: 'Bank', AMOUNT: '100.00' },
      { LEVEL: 1, NODE: 'LIABILITIES', PARENT_NODE: null, NODE_KIND: 'NORMAL', SIDE: 'LIABILITIES', ACCOUNT: null, LABEL: 'Equity and liabilities', AMOUNT: '100.00' },
      { LEVEL: 2, NODE: 'LIABILITIES', PARENT_NODE: null, NODE_KIND: 'NORMAL', SIDE: 'LIABILITIES', ACCOUNT: '300000', LABEL: 'Share capital', AMOUNT: '100.00' },
    ],
  };

  it('runs from its parameters, collapses, exports and drills down', async () => {
    const { context, calls } = fakeContext({ 'POST /trfin/reports/balance-sheet': [() => report] });
    show(BalanceSheetPage, context, { COMPANY: '1000', KEY_DATE: '2026-12-31' });
    const grid = await screen.findByRole('grid', { name: 'Balance sheet' });
    expect(calls[0]).toMatchObject({ name: 'POST /trfin/reports/balance-sheet', body: { COMPANY: '1000', KEY_DATE: '2026-12-31', LEDGER: null, CHART_KIND: null, STRUCTURE: null } });
    await within(grid).findByText('110000 Bank');

    await userEvent.click(screen.getByRole('button', { name: 'Collapse Assets' }));
    await waitFor(() => expect(within(grid).queryByText('Cash')).toBeNull());
    expect(within(grid).getByText('300000 Share capital')).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'Expand Assets' }));
    await within(grid).findByText('110000 Bank');
    await userEvent.click(screen.getByRole('button', { name: 'Collapse all' }));
    await waitFor(() => expect(within(grid).queryByText('300000 Share capital')).toBeNull());
    await userEvent.click(screen.getByRole('button', { name: 'Expand all' }));

    await userEvent.click(screen.getByRole('button', { name: 'Export' }));
    await waitFor(() => expect(calls.find((c) => c.kind === 'download')).toMatchObject({ name: '/trfin/reports/balance-sheet?format=xlsx', body: { COMPANY: '1000' } }));

    await userEvent.click(within(grid).getByText('110000 Bank'));
    await waitFor(() =>
      expect(calls.find((c) => c.kind === 'navigate')).toMatchObject({ name: '/TRFINUI/LINE_ITEMS', body: { COMPANY: '1000', FISCAL_YEAR: '2026', ACCOUNT: '110000' }, extra: null }),
    );
  });
});

describe('the number range page', () => {
  const object = { NAME: '/TRFIN/JOURNAL_ENTRY', PACKAGE_ID: 'TRFIN', LABEL: 'Journal entries', LENGTH: 10, SUBOBJECT_NAME: 'COMPANY', PER_FISCAL_YEAR: true };
  const interval = { RANGE_NAME: '/TRFIN/JOURNAL_ENTRY', INTERVAL_ID: '01', SUBOBJECT: '1000', FISCAL_YEAR: 2026, FROM_NO: '0000000001', TO_NO: '0000999999', CURRENT_NO: '0000000007', EXTERNAL: false };

  it('asks before the node changes what needs a confirmation', async () => {
    const conflict = () => {
      throw Object.assign(new Error('the current number moves back'), { status: 409 });
    };
    const { context, calls } = fakeContext({
      'GET /trfin/number-ranges': [() => [object]],
      'GET /trfin/number-ranges/intervals?object=%2FTRFIN%2FJOURNAL_ENTRY': [() => [interval], () => [{ ...interval, CURRENT_NO: '0000000003' }]],
      'GET /trfin/number-ranges/history?object=%2FTRFIN%2FJOURNAL_ENTRY': [() => [], () => []],
      'PUT /trfin/number-ranges/intervals': [conflict],
      'PUT /trfin/number-ranges/intervals?confirm=true': [() => [{ ...interval, CURRENT_NO: '0000000003' }]],
    });
    show(NumberRangePage, context);
    const grid = await screen.findByRole('grid', { name: 'Interval' });
    await within(grid).findByText('0000000007');
    await userEvent.click(within(grid).getByRole('button', { name: 'Actions' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Change' }));
    await enter('Current number', '0000000003');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText('the current number moves back')).toBeTruthy();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Continue' }));
    await within(grid).findByText('0000000003');
    const puts = calls.filter((c) => c.name.startsWith('PUT'));
    expect(puts.map((c) => c.name)).toEqual(['PUT /trfin/number-ranges/intervals', 'PUT /trfin/number-ranges/intervals?confirm=true']);
    expect(puts[1]?.body).toMatchObject({ INTERVAL_ID: '01', SUBOBJECT: '1000', FISCAL_YEAR: 2026, CURRENT_NO: '0000000003' });
  });
});
