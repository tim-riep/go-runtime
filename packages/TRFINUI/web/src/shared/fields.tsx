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

// Fields of selections and entries: company, ledger, chart kind, years and
// periods, dates, accounts and partners. Values that may be absent are
// null; "" is never used for "no value".

import { DatePicker, FormField, LookupField, NumberField, Select } from '@tim-riep/trui';
import type { ListItem } from '@tim-riep/trui/core';
import type { Company } from './masterdata.ts';
import { searchItems } from './masterdata.ts';
import type { Texts } from './texts.ts';

export function CompanyField({ id, companies, value, onChange, texts }: { id: string; companies: Company[]; value: string | null; onChange: (c: string) => void; texts: Texts }) {
  const items = companies.map((c) => ({ value: c.COMPANY, label: c.COMPANY, description: c.NAME }));
  return (
    <FormField label={texts.company} required inputId={id}>
      <Select id={id} items={items} value={value ?? ''} onChange={(v) => onChange(v)} required />
    </FormField>
  );
}

export function LedgerField({ id, ledgers, value, onChange, texts }: { id: string; ledgers: ListItem[]; value: string | null; onChange: (l: string | null) => void; texts: Texts }) {
  return (
    <FormField label={texts.ledger} inputId={id}>
      <Select id={id} items={ledgers} nullable placeholder={texts.primaryLedger} value={value} onChange={(v) => onChange(v)} />
    </FormField>
  );
}

export function ChartKindField({ id, value, onChange, texts }: { id: string; value: string | null; onChange: (k: string | null) => void; texts: Texts }) {
  const items = [
    { value: 'OPERATIONAL', label: texts.operationalChart },
    { value: 'COUNTRY', label: texts.countryChart },
  ];
  return (
    <FormField label={texts.chartKind} inputId={id}>
      <Select id={id} items={items} nullable placeholder={texts.operationalChart} value={value} onChange={(v) => onChange(v)} />
    </FormField>
  );
}

/** An integer such as a fiscal year or a period; null when empty. */
export function IntegerField({ id, label, value, onChange, required, min, max }: { id: string; label: string; value: number | null; onChange: (n: number | null) => void; required?: boolean; min?: number; max?: number }) {
  const limits = { ...(min !== undefined ? { min: String(min) } : {}), ...(max !== undefined ? { max: String(max) } : {}) };
  return (
    <FormField label={label} required={required ?? false} inputId={id}>
      <NumberField id={id} scale={0} grouping={false} nullable {...limits} value={value === null ? null : String(value)} onChange={(v) => onChange(v === null ? null : Number(v))} />
    </FormField>
  );
}

export function DateField({ id, label, value, onChange, required }: { id: string; label: string; value: string | null; onChange: (d: string | null) => void; required?: boolean }) {
  return (
    <FormField label={label} required={required ?? false} inputId={id}>
      <DatePicker id={id} nullable value={value} onChange={(v) => onChange(v)} />
    </FormField>
  );
}

/** A value of a value help with the items given, such as accounts or partners. */
export function LookupInput({
  id,
  label,
  items,
  value,
  onChange,
  error,
  hideLabel,
}: {
  id: string;
  label: string;
  items: readonly ListItem[];
  value: string | null;
  onChange: (v: string | null) => void;
  error?: string | null;
  hideLabel?: boolean;
}) {
  const description = items.find((i) => i.value === value)?.description ?? '';
  const state = error ? { valueState: 'error' as const, valueStateMessage: error } : {};
  const input = (
    <LookupField
      id={id}
      nullable
      fetchItems={searchItems(items)}
      dialogTitle={label}
      description={description}
      value={value}
      onChange={(v) => onChange(v)}
      {...(hideLabel ? { 'aria-label': label } : {})}
      {...state}
    />
  );
  return hideLabel ? input : (
    <FormField label={label} inputId={id}>
      {input}
    </FormField>
  );
}
