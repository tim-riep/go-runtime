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

// The context that the shell of TRFRONTEND 1.1.0 gives remote apps, as far
// as the apps of TRFINUI use it. The shell passes its own objects; these
// types describe them without importing the shell.

export type Value = string | number | boolean | null | Value[] | { [name: string]: Value };
export type Entity = Record<string, Value>;

export interface Page {
  value: Entity[];
  count: number | null;
  nextLink: string | null;
}

export interface Query {
  filter?: string;
  orderby?: string;
  top?: number;
  skip?: number;
  count?: boolean;
  select?: string[];
  expand?: string[];
}

export interface Changed {
  entity: Entity | null;
  etag: string | null;
}

/** The OData client of the shell for one service. */
export interface ODataClient {
  query(set: string, q?: Query): Promise<Page>;
  page(path: string): Promise<Page>;
  entityPath(set: string, key: Entity): Promise<string>;
  action(name: string, params: Record<string, Value>): Promise<Changed>;
}

/** A request to a node: POST with a body, else GET, or the method. */
export interface JSONInit {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

export type NotifyTone = 'information' | 'success' | 'warning' | 'error';

export interface RemoteContext {
  readonly language: string;
  odata(service: string): ODataClient;
  fetchJSON<T = unknown>(path: string, init?: JSONInit): Promise<T>;
  download(path: string, init?: JSONInit): Promise<string>;
  navigate(app: string, params?: Record<string, string>, entity?: string | null): void;
  notify(message: string, tone?: NotifyTone): void;
}

/** The properties of the component of a remote app. */
export interface RemoteProps {
  params: Record<string, string>;
  context: RemoteContext;
}

export const configurationService = '/TRFIN/CONFIGURATION_SERVICE';
export const masterDataService = '/TRFIN/MASTER_DATA_SERVICE';
export const journalEntryService = '/TRFIN/JOURNAL_ENTRY_SERVICE';

/** The message of a failure: the details of OData errors, the text of others. */
export function errorText(e: unknown): string {
  if (e && typeof e === 'object' && 'details' in e && Array.isArray((e as { details: unknown }).details)) {
    const details = (e as { details: Array<{ message?: string }> }).details.map((d) => d.message ?? '').filter(Boolean);
    if (details.length) {
      return details.join(' ');
    }
  }
  return e instanceof Error ? e.message : String(e);
}

/** All entities of a query, following the next links of the service. */
export async function allEntities(client: ODataClient, set: string, q: Query = {}): Promise<Entity[]> {
  let page = await client.query(set, q);
  const out = [...page.value];
  while (page.nextLink) {
    page = await client.page(page.nextLink);
    out.push(...page.value);
  }
  return out;
}

/** A string literal of OData. */
export function literal(s: string): string {
  return `'${s.replaceAll("'", "''")}'`;
}
