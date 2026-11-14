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


// The filter bar of a list page as $filter: text fields match a part of
// the value, fields with fixed values, numbers, dates and booleans match
// exactly. Empty fields do not filter; the empty string is no filter value.

import { literal } from '../odata/index.ts';
import type { Field } from './view.ts';

export type FilterValues = Record<string, string | null>;

/** The $filter of the values of the filter bar, or undefined without any. */
export function filterOf(fields: Field[], values: FilterValues): string | undefined {
  const parts: string[] = [];
  for (const f of fields) {
    const v = values[f.name];
    if (v === null || v === undefined || v === '') {
      continue;
    }
    const type = f.property.type;
    if (type === 'Edm.String' && !f.fixedValues && !f.valueHelp) {
      parts.push(`contains(${f.name},${literal(v, type)})`);
    } else {
      parts.push(`${f.name} eq ${literal(type === 'Edm.Boolean' ? v === 'true' : v, type)}`);
    }
  }
  return parts.length ? parts.join(' and ') : undefined;
}

/** Whether an entity matches the filter values, for rows filtered in the browser. */
export function matches(fields: Field[], values: FilterValues, entity: Record<string, unknown>): boolean {
  for (const f of fields) {
    const v = values[f.name];
    if (v === null || v === undefined || v === '') {
      continue;
    }
    const actual = entity[f.name];
    if (f.property.type === 'Edm.String' && !f.fixedValues && !f.valueHelp) {
      if (typeof actual !== 'string' || !actual.toLowerCase().includes(v.toLowerCase())) return false;
    } else if (String(actual) !== v) {
      return false;
    }
  }
  return true;
}
