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

// Values of entities. The client asks for IEEE754Compatible=true, so
// Edm.Int64 and Edm.Decimal travel as strings and never become JavaScript
// numbers. null means no value and "" is a value; nothing turns one into
// the other.

import type { EntityType, Property } from './metadata.ts';

/** A value of a property: strings also for Int64, Decimal, dates and times. */
export type Value = string | number | boolean | null | Value[] | { [name: string]: Value };

/** An entity: its properties by public name. */
export type Entity = Record<string, Value>;

const quoted = new Set(['Edm.String']);
const raw = new Set([
  'Edm.Int16',
  'Edm.Int32',
  'Edm.Int64',
  'Edm.Decimal',
  'Edm.Double',
  'Edm.Single',
  'Edm.Byte',
  'Edm.SByte',
  'Edm.Boolean',
  'Edm.Date',
  'Edm.DateTimeOffset',
  'Edm.TimeOfDay',
  'Edm.Guid',
]);

/**
 * A literal of a value in a URL: strings in single quotes with doubled
 * quotes, numbers, booleans, dates and GUIDs as they are, null as null.
 */
export function literal(value: Value, type: string): string {
  if (value === null) {
    return 'null';
  }
  if (quoted.has(type) || !raw.has(type)) {
    return `'${String(value).replaceAll("'", "''")}'`;
  }
  return String(value);
}

/** The key predicate of an entity, e.g. ('C1') or (NAME='/X',POSITION=1). */
export function keyPredicate(type: EntityType, entity: Entity): string {
  const parts = type.key.map((k) => {
    const p = type.properties.find((x) => x.name === k);
    return { name: k, text: literal(entity[k] ?? null, p?.type ?? 'Edm.String') };
  });
  if (parts.length === 1 && parts[0]) {
    return `(${encodeURIComponent(parts[0].text)})`;
  }
  return `(${parts.map((p) => `${p.name}=${encodeURIComponent(p.text)}`).join(',')})`;
}

/** Whether a property travels as a string although it is a number. */
export function isExactNumber(p: Property): boolean {
  return p.type === 'Edm.Int64' || p.type === 'Edm.Decimal';
}

/** Whether a property is a number of any kind. */
export function isNumber(p: Property): boolean {
  return ['Edm.Int16', 'Edm.Int32', 'Edm.Int64', 'Edm.Decimal', 'Edm.Double', 'Edm.Single', 'Edm.Byte', 'Edm.SByte'].includes(p.type);
}

/**
 * The changed properties of an entity for PATCH: those whose value
 * differs from the original, compared as JSON so that null and "" stay
 * apart.
 */
export function changes(original: Entity, edited: Entity): Entity {
  const out: Entity = {};
  for (const [k, v] of Object.entries(edited)) {
    if (JSON.stringify(original[k] ?? null) !== JSON.stringify(v)) {
      out[k] = v;
    }
  }
  return out;
}
