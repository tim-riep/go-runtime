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

// Items of journal entries as the pages edit them, the running balance with
// exact decimals, the items of /TRFIN/POSTING and the messages of CHECK per
// item and field.

import { addDecimal, negateDecimal, signDecimal } from '@tim-riep/trui/core';
import { format, type Texts } from './texts.ts';

export type AccountKind = 'ACCOUNT' | 'CUSTOMER' | 'SUPPLIER';

export interface ItemDraft {
  /** Identifies the row while it is edited. */
  key: string;
  ACCOUNT_KIND: AccountKind;
  ACCOUNT: string | null;
  PARTNER: string | null;
  DEBIT_CREDIT: 'D' | 'C';
  AMOUNT: string | null;
  ITEM_TEXT: string | null;
  ASSIGNMENT: string | null;
  DUE_DATE: string | null;
}

/** An item of /TRFIN/POSTING. */
export interface PostingItem {
  ITEM_NO: number;
  ACCOUNT_KIND: AccountKind;
  ACCOUNT: string | null;
  PARTNER: string | null;
  DEBIT_CREDIT: 'D' | 'C';
  AMOUNT: string;
  ITEM_TEXT: string | null;
  ASSIGNMENT: string | null;
  DUE_DATE: string | null;
}

/** A message of /TRFIN/POSTING_MESSAGE: ITEM_NO null for the entry. */
export interface PostingMessage {
  ITEM_NO: number | null;
  FIELD: string | null;
  TEXT: string;
}

let counter = 0;

/** A new empty item. */
export function newItem(dc: 'D' | 'C' = 'D', kind: AccountKind = 'ACCOUNT'): ItemDraft {
  counter += 1;
  return { key: `item-${counter}`, ACCOUNT_KIND: kind, ACCOUNT: null, PARTNER: null, DEBIT_CREDIT: dc, AMOUNT: null, ITEM_TEXT: null, ASSIGNMENT: null, DUE_DATE: null };
}

/** The amount of an item with sign: debit positive, credit negative; 0 without amount. */
export function signedAmount(item: Pick<ItemDraft, 'DEBIT_CREDIT' | 'AMOUNT'>): string {
  if (item.AMOUNT === null) return '0';
  return item.DEBIT_CREDIT === 'C' ? negateDecimal(item.AMOUNT) : item.AMOUNT;
}

/** The balance after each item, from the first on. */
export function runningBalances(items: readonly ItemDraft[]): string[] {
  const out: string[] = [];
  let balance = '0';
  for (const item of items) {
    balance = addDecimal(balance, signedAmount(item));
    out.push(balance);
  }
  return out;
}

/** The balance of all items: zero when debit equals credit. */
export function balanceOf(items: readonly Pick<ItemDraft, 'DEBIT_CREDIT' | 'AMOUNT'>[]): string {
  return items.reduce((sum, i) => addDecimal(sum, signedAmount(i)), '0');
}

export function isZero(d: string): boolean {
  return signDecimal(d) === 0;
}

/**
 * Messages of the page itself before anything is sent: an amount, an
 * account for accounts and a partner for customers and suppliers. The item
 * numbers are the positions from 1.
 */
export function missingFields(items: readonly ItemDraft[], texts: Texts): PostingMessage[] {
  const out: PostingMessage[] = [];
  items.forEach((item, i) => {
    const no = i + 1;
    if (item.ACCOUNT_KIND === 'ACCOUNT' && item.ACCOUNT === null) out.push({ ITEM_NO: no, FIELD: 'ACCOUNT', TEXT: format(texts.required, { field: texts.account }) });
    if (item.ACCOUNT_KIND !== 'ACCOUNT' && item.PARTNER === null) out.push({ ITEM_NO: no, FIELD: 'PARTNER', TEXT: format(texts.required, { field: texts.partner }) });
    if (item.AMOUNT === null) out.push({ ITEM_NO: no, FIELD: 'AMOUNT', TEXT: format(texts.required, { field: texts.amount }) });
  });
  return out;
}

/** The items of /TRFIN/POSTING, numbered by their position; missingFields must have none. */
export function postingItems(items: readonly ItemDraft[]): PostingItem[] {
  return items.map((item, i) => {
    if (item.AMOUNT === null) {
      throw new Error(`item ${i + 1} has no amount`);
    }
    return {
    ITEM_NO: i + 1,
    ACCOUNT_KIND: item.ACCOUNT_KIND,
    ACCOUNT: item.ACCOUNT_KIND === 'ACCOUNT' ? item.ACCOUNT : null,
    PARTNER: item.ACCOUNT_KIND === 'ACCOUNT' ? null : item.PARTNER,
    DEBIT_CREDIT: item.DEBIT_CREDIT,
    AMOUNT: item.AMOUNT,
    ITEM_TEXT: item.ITEM_TEXT,
    ASSIGNMENT: item.ASSIGNMENT,
    DUE_DATE: item.ACCOUNT_KIND === 'ACCOUNT' ? null : item.DUE_DATE,
    };
  });
}

/** The messages of the entry and those of every item by number and field. */
export interface SortedMessages {
  entry: PostingMessage[];
  items: Map<number, PostingMessage[]>;
}

export function sortMessages(messages: readonly PostingMessage[]): SortedMessages {
  const entry: PostingMessage[] = [];
  const items = new Map<number, PostingMessage[]>();
  for (const m of messages) {
    if (m.ITEM_NO === null) {
      entry.push(m);
    } else {
      const list = items.get(m.ITEM_NO) ?? [];
      list.push(m);
      items.set(m.ITEM_NO, list);
    }
  }
  return { entry, items };
}

/** The text of the messages of an item at a field, null for none. */
export function fieldMessage(sorted: SortedMessages, itemNo: number, field: string): string | null {
  const list = (sorted.items.get(itemNo) ?? []).filter((m) => m.FIELD === field);
  return list.length ? list.map((m) => m.TEXT).join(' ') : null;
}

/**
 * The difference of a clearing: the selected open items (debit positive)
 * plus the new items on the account or partner of the selection.
 */
export function clearingDifference(selected: readonly { AMOUNT: string }[], items: readonly ItemDraft[], kind: AccountKind, target: string | null): string {
  let sum = selected.reduce((s, r) => addDecimal(s, r.AMOUNT), '0');
  for (const item of items) {
    const same = kind === 'ACCOUNT' ? item.ACCOUNT_KIND === 'ACCOUNT' && item.ACCOUNT === target : item.ACCOUNT_KIND === kind && item.PARTNER === target;
    if (same) sum = addDecimal(sum, signedAmount(item));
  }
  return sum;
}

/**
 * Items that clear a difference: one on the account or partner of the
 * selection with the opposite sign, and one to enter the other account,
 * such as the bank, with the same amount on the other side.
 */
export function offsetItems(difference: string, kind: AccountKind, target: string | null): ItemDraft[] {
  const credit = !difference.startsWith('-');
  const amount = credit ? difference : negateDecimal(difference);
  return [
    { ...newItem(credit ? 'D' : 'C'), AMOUNT: amount },
    { ...newItem(credit ? 'C' : 'D', kind), ACCOUNT: kind === 'ACCOUNT' ? target : null, PARTNER: kind === 'ACCOUNT' ? null : target, AMOUNT: amount },
  ];
}

/** The fields of items that have an input in the table; messages at others show at the item. */
export const itemFields = ['ACCOUNT_KIND', 'ACCOUNT', 'PARTNER', 'DEBIT_CREDIT', 'AMOUNT', 'ITEM_TEXT', 'ASSIGNMENT', 'DUE_DATE'];

/** The id of the input of a field of an item, as target of messages. */
export function cellId(prefix: string, itemNo: number, field: string): string {
  return `${prefix}-item-${itemNo}-${field.toLowerCase().replaceAll('_', '-')}`;
}
