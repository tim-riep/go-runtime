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

// The editable table of items of a journal entry: kind, account or
// partner with value helps, debit or credit, the amount in the currency of
// the company, texts and due dates, with the running balance after every
// item and the messages of the service at the cells.

import { AmountField, Button, DatePicker, Select, Table, type TableColumn, TextField } from '@tim-riep/trui';
import { useFormat } from '@tim-riep/trui/core';
import { addIcon, deleteIcon } from '@tim-riep/trui/icons';
import { LookupInput } from './fields.tsx';
import { type AccountKind, cellId, fieldMessage, type ItemDraft, newItem, runningBalances, type SortedMessages } from './items.ts';
import { accountItems, type CompanyData, partnerItems } from './masterdata.ts';
import { format, type Texts } from './texts.ts';

export interface ItemsTableProps {
  /** Prefix of the ids of the inputs, unique on the page. */
  id: string;
  items: ItemDraft[];
  onChange: (items: ItemDraft[]) => void;
  data: CompanyData;
  currency: string;
  messages: SortedMessages;
  texts: Texts;
  label: string;
}

export function ItemsTable({ id, items, onChange, data, currency, messages, texts, label }: ItemsTableProps) {
  const fmt = useFormat();
  const balances = runningBalances(items);
  const set = (index: number, change: Partial<ItemDraft>) => {
    onChange(items.map((it, i) => (i === index ? { ...it, ...change } : it)));
  };
  const accounts = accountItems(data.accounts);
  const customers = partnerItems(data.partners, 'CUSTOMER');
  const suppliers = partnerItems(data.partners, 'SUPPLIER');
  const kinds = [
    { value: 'ACCOUNT', label: texts.accounts },
    { value: 'CUSTOMER', label: texts.customer },
    { value: 'SUPPLIER', label: texts.supplier },
  ];
  const sides = [
    { value: 'D', label: texts.debit },
    { value: 'C', label: texts.credit },
  ];
  const state = (no: number, field: string) => {
    const m = fieldMessage(messages, no, field);
    return m === null ? {} : { valueState: 'error' as const, valueStateMessage: m };
  };
  const cell = (field: string, no: number) => format(texts.cellLabel, { field, no });
  const index = (row: ItemDraft) => items.findIndex((i) => i.key === row.key);

  const columns: TableColumn<ItemDraft>[] = [
    { key: 'ITEM_NO', header: texts.itemNo, width: 56, align: 'end', sortable: false, renderCell: (row) => String(index(row) + 1) },
    {
      key: 'ACCOUNT_KIND',
      header: texts.accountKind,
      width: 140,
      sortable: false,
      renderCell: (row) => {
        const i = index(row);
        return (
          <Select
            id={cellId(id, i + 1, 'ACCOUNT_KIND')}
            aria-label={cell(texts.accountKind, i + 1)}
            items={kinds}
            value={row.ACCOUNT_KIND}
            onChange={(v) => set(i, { ACCOUNT_KIND: v as AccountKind, ACCOUNT: v === 'ACCOUNT' ? row.ACCOUNT : null, PARTNER: v === 'ACCOUNT' ? null : row.PARTNER })}
            {...state(i + 1, 'ACCOUNT_KIND')}
          />
        );
      },
    },
    {
      key: 'ACCOUNT',
      header: texts.accountOrPartner,
      width: 260,
      sortable: false,
      renderCell: (row) => {
        const i = index(row);
        if (row.ACCOUNT_KIND === 'ACCOUNT') {
          return (
            <LookupInput
              id={cellId(id, i + 1, 'ACCOUNT')}
              label={cell(texts.account, i + 1)}
              hideLabel
              items={accounts}
              value={row.ACCOUNT}
              onChange={(v) => set(i, { ACCOUNT: v })}
              error={fieldMessage(messages, i + 1, 'ACCOUNT')}
            />
          );
        }
        return (
          <LookupInput
            id={cellId(id, i + 1, 'PARTNER')}
            label={cell(texts.partner, i + 1)}
            hideLabel
            items={row.ACCOUNT_KIND === 'CUSTOMER' ? customers : suppliers}
            value={row.PARTNER}
            onChange={(v) => set(i, { PARTNER: v })}
            error={fieldMessage(messages, i + 1, 'PARTNER') ?? fieldMessage(messages, i + 1, 'ACCOUNT')}
          />
        );
      },
    },
    {
      key: 'DEBIT_CREDIT',
      header: texts.debitCredit,
      width: 120,
      sortable: false,
      renderCell: (row) => {
        const i = index(row);
        return (
          <Select
            id={cellId(id, i + 1, 'DEBIT_CREDIT')}
            aria-label={cell(texts.debitCredit, i + 1)}
            items={sides}
            value={row.DEBIT_CREDIT}
            onChange={(v) => set(i, { DEBIT_CREDIT: v === 'C' ? 'C' : 'D' })}
            {...state(i + 1, 'DEBIT_CREDIT')}
          />
        );
      },
    },
    {
      key: 'AMOUNT',
      header: texts.amount,
      width: 170,
      align: 'end',
      sortable: false,
      renderCell: (row) => {
        const i = index(row);
        return (
          <AmountField
            id={cellId(id, i + 1, 'AMOUNT')}
            aria-label={cell(texts.amount, i + 1)}
            currency={currency}
            nullable
            value={row.AMOUNT}
            onChange={(v) => set(i, { AMOUNT: v })}
            {...state(i + 1, 'AMOUNT')}
          />
        );
      },
    },
    {
      key: 'BALANCE',
      header: texts.runningBalance,
      width: 150,
      align: 'end',
      sortable: false,
      renderCell: (row) => <output aria-label={cell(texts.runningBalance, index(row) + 1)}>{fmt.decimal(balances[index(row)] ?? '0', { scale: 2 })}</output>,
    },
    {
      key: 'ITEM_TEXT',
      header: texts.itemText,
      width: 200,
      sortable: false,
      renderCell: (row) => {
        const i = index(row);
        return (
          <TextField id={cellId(id, i + 1, 'ITEM_TEXT')} aria-label={cell(texts.itemText, i + 1)} nullable maxLength={50} value={row.ITEM_TEXT} onChange={(v) => set(i, { ITEM_TEXT: v })} {...state(i + 1, 'ITEM_TEXT')} />
        );
      },
    },
    {
      key: 'DUE_DATE',
      header: texts.dueDate,
      width: 160,
      sortable: false,
      renderCell: (row) => {
        const i = index(row);
        return row.ACCOUNT_KIND === 'ACCOUNT' ? null : (
          <DatePicker id={cellId(id, i + 1, 'DUE_DATE')} aria-label={cell(texts.dueDate, i + 1)} nullable value={row.DUE_DATE} onChange={(v) => set(i, { DUE_DATE: v })} {...state(i + 1, 'DUE_DATE')} />
        );
      },
    },
    {
      key: 'REMOVE',
      header: '',
      width: 56,
      sortable: false,
      resizable: false,
      renderCell: (row) => {
        const i = index(row);
        return <Button variant="text" icon={deleteIcon} aria-label={format(texts.removeItem, { no: i + 1 })} onClick={() => onChange(items.filter((x) => x.key !== row.key))} />;
      },
    },
  ];

  return (
    <>
      <Table<ItemDraft> aria-label={label} columns={columns} rows={items} manualSort getRowKey={(row) => row.key} renderEmpty={() => texts.noRows} />
      <div>
        <Button icon={addIcon} onClick={() => onChange([...items, newItem(items.length % 2 === 0 ? 'D' : 'C')])}>
          {texts.addItem}
        </Button>
      </div>
    </>
  );
}
