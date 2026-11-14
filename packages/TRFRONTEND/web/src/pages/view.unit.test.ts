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
import { parseMetadata } from '../odata/index.ts';
import { filterOf, matches } from './filter.ts';
import { actionLabel, detailView, listView, mergedDetailView } from './view.ts';

const m = parseMetadata(JSON.parse(readFileSync(new URL('../../test/fixtures/company-service.json', import.meta.url), 'utf8')));

describe('list view', () => {
  const v = listView(m, 'Companies');

  it('takes columns, filter fields, sort and actions from Frontend.ListPage', () => {
    expect(v.typeNamePlural).toBe('Companies');
    expect(v.columns.map((c) => c.field.name)).toEqual(['COMPANY_ID', 'NAME', 'TROTTO_OTTO_ID', 'CITY', 'COUNTRY', 'STATUS']);
    expect(v.columns[0]).toMatchObject({ width: 8, importance: 'HIGH' });
    expect(v.columns[3]?.width).toBeNull();
    expect(v.filterFields.map((f) => f.name)).toEqual(['NAME', 'COUNTRY', 'STATUS', 'TROTTO_OTTO_ID']);
    expect(v.sort).toEqual([{ field: 'NAME', descending: false }]);
    expect(v.actions).toEqual(['TRFIN.BLOCK', 'TRFIN.TROTTO_SYNC']);
    expect(v.insertable && v.deletable).toBe(true);
  });

  it('reads labels, fixed values, value helps and rules of fields', () => {
    const status = v.columns.find((c) => c.field.name === 'STATUS')?.field;
    expect(status?.computed).toBe(true);
    expect(status?.fixedValues?.[1]).toEqual({ value: 'BLOCKED', label: 'Blocked for postings' });
    const country = v.filterFields.find((f) => f.name === 'COUNTRY');
    expect(country?.valueHelp).toEqual({ set: 'Countries', property: 'COUNTRY_CODE' });
    expect(country?.label).toBe('Country');
    const id = v.columns[0]?.field;
    expect(id).toMatchObject({ key: true, immutable: true, label: 'Company' });
    expect(v.columns[1]?.field.mandatory).toBe(true);
  });

  it('shows the key and all fields without annotations', () => {
    const c = listView(m, 'Countries');
    expect(c.columns.map((x) => x.field.name)).toEqual(['COUNTRY_CODE', 'NAME', 'CURRENCY']);
    expect(c.insertable).toBe(false);
  });
});

describe('detail view', () => {
  it('takes header, sections, groups and tables from the annotations', () => {
    const d = detailView(m, 'Companies');
    expect(d).toMatchObject({ typeName: 'Company', titleField: 'NAME', subtitleField: 'COMPANY_ID', descriptionField: 'CITY' });
    expect(d.sections.map((s) => s.id)).toEqual(['GENERAL', '/TROTTOUI/MARKETPLACE', 'CONTACTS']);
    expect(d.sections[0]?.groups[1]?.fields.map((f) => f.name)).toEqual(['STREET', 'POSTAL_CODE', 'CITY', 'COUNTRY']);
    expect(d.sections[2]?.table).toBe('Contacts');
    expect(d.concurrency).toEqual(['CHANGED_AT']);
    expect(d.actions).toEqual(['TRFIN.BLOCK']);
  });

  it('leaves out tables that the type of a merged set lacks', () => {
    const d = detailView(m, 'Companies');
    const countries = m.entitySets.Countries;
    if (!countries) throw new Error('no Countries');
    expect(mergedDetailView(d, countries).sections.map((s) => s.id)).toEqual(['GENERAL', '/TROTTOUI/MARKETPLACE']);
    const companies = m.entitySets.Companies;
    if (!companies) throw new Error('no Companies');
    expect(mergedDetailView(d, companies).sections.map((s) => s.id)).toEqual(['GENERAL', '/TROTTOUI/MARKETPLACE', 'CONTACTS']);
  });

  it('labels actions', () => {
    expect(actionLabel(m, 'TRFIN.TROTTO_SYNC')).toBe('Gives the company a partner ID at OTTO');
    expect(actionLabel(m, 'TRFIN.BLOCK')).toBe('BLOCK');
  });
});

describe('filter', () => {
  const v = listView(m, 'Companies');

  it('matches text in part and fixed values and value helps exactly', () => {
    expect(filterOf(v.filterFields, { NAME: "O'B", COUNTRY: 'DE', STATUS: null, TROTTO_OTTO_ID: '' })).toBe("contains(NAME,'O''B') and COUNTRY eq 'DE'");
    expect(filterOf(v.filterFields, {})).toBeUndefined();
  });

  it('filters rows in the browser the same way', () => {
    expect(matches(v.filterFields, { NAME: 'acme', STATUS: 'ACTIVE' }, { NAME: 'ACME Ltd', STATUS: 'ACTIVE' })).toBe(true);
    expect(matches(v.filterFields, { NAME: 'acme', STATUS: 'ACTIVE' }, { NAME: 'ACME Ltd', STATUS: 'CLOSED' })).toBe(false);
  });
});
