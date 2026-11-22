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
import { allEntities, errorText, literal, type ODataClient } from './context.ts';
import {
  balanceOf,
  cellId,
  clearingDifference,
  fieldMessage,
  type ItemDraft,
  missingFields,
  newItem,
  offsetItems,
  postingItems,
  runningBalances,
  sortMessages,
} from './items.ts';
import { accountItems, partnerItems, searchItems } from './masterdata.ts';
import { numberParam, param, paramsOf } from './report.tsx';
import { format, textsOf } from './texts.ts';
import { parentKeys, rowKey, type StatementRow, visibleRows } from './tree.ts';

const texts = textsOf('en');

function item(change: Partial<ItemDraft>): ItemDraft {
  return { ...newItem(), ...change };
}

describe('items of journal entries', () => {
  it('keep a running balance with exact decimals', () => {
    const items = [
      item({ DEBIT_CREDIT: 'D', AMOUNT: '0.10' }),
      item({ DEBIT_CREDIT: 'D', AMOUNT: '0.20' }),
      item({ DEBIT_CREDIT: 'C', AMOUNT: null }),
      item({ DEBIT_CREDIT: 'C', AMOUNT: '0.30' }),
    ];
    expect(runningBalances(items)).toEqual(['0.10', '0.30', '0.30', '0.00']);
    expect(balanceOf(items)).toBe('0.00');
    expect(balanceOf([item({ DEBIT_CREDIT: 'C', AMOUNT: '1234567890123456.78' })])).toBe('-1234567890123456.78');
  });

  it('name what is missing before anything is sent', () => {
    const items = [item({ ACCOUNT: '100000', AMOUNT: '1.00' }), item({ ACCOUNT_KIND: 'CUSTOMER', ACCOUNT: '100000', AMOUNT: null })];
    expect(missingFields(items, texts)).toEqual([
      { ITEM_NO: 2, FIELD: 'PARTNER', TEXT: 'Partner is missing.' },
      { ITEM_NO: 2, FIELD: 'AMOUNT', TEXT: 'Amount is missing.' },
    ]);
  });

  it('become items of a posting numbered by position, without values of the other kind', () => {
    const items = [
      item({ ACCOUNT: '100000', PARTNER: '0000000001', DEBIT_CREDIT: 'D', AMOUNT: '5.00', ITEM_TEXT: '', DUE_DATE: '2026-01-01' }),
      item({ ACCOUNT_KIND: 'SUPPLIER', ACCOUNT: '160000', PARTNER: '0000000003', DEBIT_CREDIT: 'C', AMOUNT: '5.00', DUE_DATE: '2026-02-01' }),
    ];
    expect(postingItems(items)).toEqual([
      { ITEM_NO: 1, ACCOUNT_KIND: 'ACCOUNT', ACCOUNT: '100000', PARTNER: null, DEBIT_CREDIT: 'D', AMOUNT: '5.00', ITEM_TEXT: '', ASSIGNMENT: null, DUE_DATE: null },
      { ITEM_NO: 2, ACCOUNT_KIND: 'SUPPLIER', ACCOUNT: null, PARTNER: '0000000003', DEBIT_CREDIT: 'C', AMOUNT: '5.00', ITEM_TEXT: null, ASSIGNMENT: null, DUE_DATE: '2026-02-01' },
    ]);
    expect(() => postingItems([item({ ACCOUNT: '100000' })])).toThrow('item 1 has no amount');
  });

  it('sort the messages of CHECK by item and field', () => {
    const sorted = sortMessages([
      { ITEM_NO: 2, FIELD: 'ACCOUNT', TEXT: 'the account 999999 does not exist' },
      { ITEM_NO: null, FIELD: null, TEXT: 'debit and credit differ' },
      { ITEM_NO: 2, FIELD: 'ACCOUNT', TEXT: 'and is blocked' },
      { ITEM_NO: 1, FIELD: 'AMOUNT', TEXT: 'too many decimals' },
    ]);
    expect(sorted.entry).toEqual([{ ITEM_NO: null, FIELD: null, TEXT: 'debit and credit differ' }]);
    expect(fieldMessage(sorted, 2, 'ACCOUNT')).toBe('the account 999999 does not exist and is blocked');
    expect(fieldMessage(sorted, 1, 'AMOUNT')).toBe('too many decimals');
    expect(fieldMessage(sorted, 1, 'ACCOUNT')).toBeNull();
    expect(cellId('je', 3, 'DEBIT_CREDIT')).toBe('je-item-3-debit-credit');
  });
});

describe('clearing', () => {
  it('sums the selected items and the new items on the same partner', () => {
    const selected = [{ AMOUNT: '119.00' }, { AMOUNT: '-19.00' }];
    expect(clearingDifference(selected, [], 'CUSTOMER', '0000000001')).toBe('100.00');
    const items = offsetItems('100.00', 'CUSTOMER', '0000000001');
    expect(items.map((i) => [i.ACCOUNT_KIND, i.PARTNER, i.DEBIT_CREDIT, i.AMOUNT])).toEqual([
      ['ACCOUNT', null, 'D', '100.00'],
      ['CUSTOMER', '0000000001', 'C', '100.00'],
    ]);
    expect(clearingDifference(selected, items, 'CUSTOMER', '0000000001')).toBe('0.00');
    // Items on another partner or an account do not count.
    expect(clearingDifference(selected, [item({ ACCOUNT_KIND: 'CUSTOMER', PARTNER: '0000000002', DEBIT_CREDIT: 'C', AMOUNT: '100.00' })], 'CUSTOMER', '0000000001')).toBe('100.00');
    const back = offsetItems('-30.00', 'ACCOUNT', '110000');
    expect(back.map((i) => [i.ACCOUNT_KIND, i.ACCOUNT, i.DEBIT_CREDIT, i.AMOUNT])).toEqual([
      ['ACCOUNT', null, 'C', '30.00'],
      ['ACCOUNT', '110000', 'D', '30.00'],
    ]);
    expect(clearingDifference([{ AMOUNT: '-30.00' }], back, 'ACCOUNT', '110000')).toBe('0.00');
  });
});

describe('the balance sheet as tree', () => {
  const row = (LEVEL: number, NODE: string, ACCOUNT: string | null = null): StatementRow => ({
    LEVEL,
    NODE,
    PARENT_NODE: null,
    NODE_KIND: 'NORMAL',
    SIDE: 'ASSETS',
    ACCOUNT,
    LABEL: NODE,
    AMOUNT: '0.00',
  });
  const rows = [row(1, 'ASSETS'), row(2, 'FIXED'), row(3, 'FIXED', '030000'), row(2, 'CURRENT'), row(3, 'CASH'), row(4, 'CASH', '110000'), row(3, 'STOCK'), row(1, 'LIAB'), row(2, 'LIAB', '300000')];

  it('knows the nodes with rows below them', () => {
    expect([...parentKeys(rows)]).toEqual(['node:ASSETS', 'node:FIXED', 'node:CURRENT', 'node:CASH', 'node:LIAB']);
    expect(rowKey(rows[2] as StatementRow)).toBe('account:FIXED:030000');
  });

  it('hides the rows below collapsed nodes', () => {
    const keys = (c: string[]) => visibleRows(rows, new Set(c)).map(rowKey);
    expect(keys([])).toHaveLength(rows.length);
    expect(keys(['node:CURRENT'])).toEqual(['node:ASSETS', 'node:FIXED', 'account:FIXED:030000', 'node:CURRENT', 'node:LIAB', 'account:LIAB:300000']);
    expect(keys(['node:CASH', 'node:FIXED'])).toEqual(['node:ASSETS', 'node:FIXED', 'node:CURRENT', 'node:CASH', 'node:STOCK', 'node:LIAB', 'account:LIAB:300000']);
    expect(keys([...parentKeys(rows)])).toEqual(['node:ASSETS', 'node:LIAB']);
  });
});

describe('helpers', () => {
  it('search the items of value helps on the client', async () => {
    const items = accountItems([
      { ACCOUNT: '110000', TEXT: 'Bank', RECONCILIATION: 'NONE', BLOCKED: false },
      { ACCOUNT: '140000', TEXT: 'Receivables', RECONCILIATION: 'CUSTOMERS', BLOCKED: false },
      { ACCOUNT: '130000', TEXT: 'Blocked', RECONCILIATION: 'NONE', BLOCKED: true },
    ]);
    expect(items.map((i) => i.value)).toEqual(['110000']);
    const fetch = searchItems([...items, ...partnerItems([{ PARTNER: '0000000001', NAME: 'Alder Retail', KIND: 'CUSTOMER' }], 'CUSTOMER')]);
    expect((await fetch({ query: 'bank', offset: 0, limit: 10 })).items.map((i) => i.value)).toEqual(['110000']);
    expect((await fetch({ query: 'ALDER', offset: 0, limit: 10 })).items.map((i) => i.value)).toEqual(['0000000001']);
    expect(await fetch({ query: '', offset: 1, limit: 10 })).toMatchObject({ total: 2, items: [{ value: '0000000001' }] });
  });

  it('read and write parameters of apps without turning null into text', () => {
    expect(param({ COMPANY: '1000', EMPTY: '' }, 'COMPANY')).toBe('1000');
    expect(param({ EMPTY: '' }, 'EMPTY')).toBe('');
    expect(param({}, 'COMPANY')).toBeNull();
    expect(numberParam({ YEAR: '2026', BAD: 'x', EMPTY: '' }, 'YEAR')).toBe(2026);
    expect(numberParam({ BAD: 'x' }, 'BAD')).toBeNull();
    expect(numberParam({ EMPTY: '' }, 'EMPTY')).toBeNull();
    expect(paramsOf({ COMPANY: '1000', LEDGER: null, FISCAL_YEAR: 2026, ACCOUNT: '' })).toEqual({ COMPANY: '1000', FISCAL_YEAR: '2026', ACCOUNT: '' });
  });

  it('formats texts and errors and follows next links', async () => {
    expect(format(textsOf('de').posted, { no: '0001000000', year: 2026 })).toBe('Beleg 0001000000 von 2026 gebucht.');
    expect(textsOf('fr').post).toBe('Post');
    expect(literal("O'Neil")).toBe("'O''Neil'");
    expect(errorText(Object.assign(new Error('The entity is not valid.'), { details: [{ message: 'debit and credit differ' }] }))).toBe('debit and credit differ');
    expect(errorText(new Error('plain'))).toBe('plain');
    const pages = [
      { value: [{ A: 1 }], count: null, nextLink: 'Set?$skiptoken=1' },
      { value: [{ A: 2 }], count: null, nextLink: null },
    ];
    const client = { query: async () => pages[0], page: async () => pages[1] } as unknown as ODataClient;
    expect(await allEntities(client, 'Set')).toEqual([{ A: 1 }, { A: 2 }]);
  });
});
