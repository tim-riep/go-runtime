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


// Shows and edits the value of one field by its type: fixed values as a
// selection, value helps as a lookup, booleans as a checkbox, dates with a
// date picker, numbers exactly as decimal strings, text otherwise. null and
// "" stay apart: a nullable text field can be emptied to null.

import { Checkbox, DatePicker, LookupField, NumberField, Select, TextField } from '@tim-riep/trui';
import type { ODataClient, Value } from '../odata/index.ts';
import { defined } from '../shell/defined.ts';
import type { TextKey } from '../shell/texts.ts';
import { type Field, valueHelpFilter } from './view.ts';

const integers = new Set(['Edm.Int16', 'Edm.Int32', 'Edm.Byte', 'Edm.SByte']);
const numbers = new Set([...integers, 'Edm.Int64', 'Edm.Decimal', 'Edm.Double', 'Edm.Single']);

/** The text of a value for display. */
export function displayValue(field: Field, value: Value | undefined, texts: Record<TextKey, string>): string {
  if (value === null || value === undefined) {
    return '';
  }
  if (field.fixedValues) {
    return field.fixedValues.find((f) => f.value === String(value))?.label ?? String(value);
  }
  if (typeof value === 'boolean') {
    return value ? texts.yes : texts.no;
  }
  if (typeof value === 'object') {
    return JSON.stringify(value);
  }
  return String(value);
}

export interface FieldEditorProps {
  field: Field;
  value: Value | undefined;
  onChange: (value: Value) => void;
  id: string;
  readOnly: boolean;
  client: ODataClient;
  /** Messages of the server at this field. */
  error: string | null;
}

/** The input of a field. */
export function FieldEditor({ field, value, onChange, id, readOnly, client, error }: FieldEditorProps) {
  const p = field.property;
  const nullable = p.nullable;
  const state = defined({ valueState: error ? ('error' as const) : undefined, valueStateMessage: error ?? undefined });
  const common = { id, readOnly, required: field.mandatory, ...state };
  const text = value === null || value === undefined ? null : String(value);

  if (field.fixedValues) {
    const items = field.fixedValues.map((f) => ({ value: f.value, label: f.label, description: f.value }));
    return nullable ? (
      <Select id={id} items={items} nullable value={text} onChange={(v) => onChange(v)} readOnly={readOnly} required={field.mandatory} {...state} />
    ) : (
      <Select id={id} items={items} value={text ?? ''} onChange={(v) => onChange(v)} readOnly={readOnly} required={field.mandatory} {...state} />
    );
  }
  if (field.valueHelp) {
    const help = field.valueHelp;
    const fetchItems = async ({ query, offset, limit }: { query: string; offset: number; limit: number }) => {
      const filter = query ? valueHelpFilter(help, query) : undefined;
      const page = await client.query(help.set, defined({ filter, top: limit, skip: offset, count: true, orderby: help.property }));
      return {
        items: page.value.map((e) => ({ value: String(e[help.property] ?? ''), label: String(e[help.property] ?? ''), description: describe(e, help.property) })),
        total: page.count ?? page.value.length,
      };
    };
    return nullable ? (
      <LookupField {...common} nullable fetchItems={fetchItems} dialogTitle={field.label} value={text} onChange={(v) => onChange(v)} />
    ) : (
      <LookupField {...common} fetchItems={fetchItems} dialogTitle={field.label} value={text ?? ''} onChange={(v) => onChange(v)} />
    );
  }
  if (p.type === 'Edm.Boolean') {
    return <Checkbox id={id} checked={value === true} onChange={(c) => onChange(c)} disabled={readOnly} aria-label={field.label} {...state} />;
  }
  if (p.type === 'Edm.Date') {
    return nullable ? (
      <DatePicker {...common} nullable value={text} onChange={(v) => onChange(v)} />
    ) : (
      <DatePicker {...common} value={text ?? ''} onChange={(v) => onChange(v)} />
    );
  }
  if (numbers.has(p.type)) {
    const toValue = (v: string | null): Value => (v === null ? null : integers.has(p.type) ? Number(v) : v);
    const scale = p.type === 'Edm.Decimal' ? (p.scale ?? undefined) : integers.has(p.type) || p.type === 'Edm.Int64' ? 0 : undefined;
    const digits = defined({ scale, integerDigits: p.precision !== null && p.scale !== null ? p.precision - p.scale : undefined });
    return nullable ? (
      <NumberField {...common} {...digits} nullable value={text} onChange={(v) => onChange(toValue(v))} />
    ) : (
      <NumberField {...common} {...digits} value={text ?? ''} onChange={(v) => onChange(toValue(v))} />
    );
  }
  const max = defined({ maxLength: p.maxLength ?? undefined });
  return nullable ? (
    <TextField {...common} {...max} nullable value={text} onChange={(v) => onChange(v)} />
  ) : (
    <TextField {...common} {...max} value={text ?? ''} onChange={(v) => onChange(v)} />
  );
}

/** A second line for values of a value help: the first other text property. */
function describe(e: Record<string, Value>, key: string): string {
  for (const [k, v] of Object.entries(e)) {
    if (k !== key && !k.startsWith('@') && typeof v === 'string' && v !== '') {
      return v;
    }
  }
  return '';
}
