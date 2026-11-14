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

// A client of one service of go-runtime over OData 4.0 in JSON. It asks
// for IEEE754Compatible=true, so that Edm.Int64 and Edm.Decimal stay
// exact strings, sends the CSRF token of the session with every change and
// the ETag with every update.

import { coreMessages, type Message, ODataError, parseError } from './errors.ts';
import { type EntitySet, parseMetadata, type ServiceModel } from './metadata.ts';
import { type Entity, keyPredicate, type Value } from './values.ts';

const jsonType = 'application/json;IEEE754Compatible=true';

export interface ClientOptions {
  /** The CSRF token of the session for changes, read when a request is sent. */
  csrfToken?: () => string | null;
  /** The language of texts, as query parameter lang. */
  language?: () => string | null;
  /** fetch, for tests. */
  fetch?: typeof fetch;
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

export interface Page {
  value: Entity[];
  /** The number of all entities that match, with count. */
  count: number | null;
  nextLink: string | null;
}

export interface Read {
  entity: Entity;
  etag: string | null;
}

export interface Changed {
  entity: Entity | null;
  etag: string | null;
  messages: Message[];
}

export class ODataClient {
  /** The URL of the service, ending with a slash. */
  readonly url: string;
  private readonly options: ClientOptions;
  private model: Promise<ServiceModel> | null = null;

  constructor(url: string, options: ClientOptions = {}) {
    this.url = url.endsWith('/') ? url : `${url}/`;
    this.options = options;
  }

  private headers(change: boolean, extra: Record<string, string> = {}): Record<string, string> {
    const h: Record<string, string> = { Accept: jsonType, 'OData-MaxVersion': '4.0', ...extra };
    if (change) {
      h['Content-Type'] = jsonType;
      const token = this.options.csrfToken?.();
      if (token) {
        h['X-CSRF-Token'] = token;
      }
    }
    return h;
  }

  private withLanguage(url: string): string {
    const lang = this.options.language?.();
    if (!lang) {
      return url;
    }
    return `${url}${url.includes('?') ? '&' : '?'}lang=${encodeURIComponent(lang)}`;
  }

  private async send(method: string, path: string, body?: unknown, extra: Record<string, string> = {}): Promise<Response> {
    const f = this.options.fetch ?? fetch;
    const change = method !== 'GET' && method !== 'HEAD';
    const init: RequestInit = { method, headers: this.headers(change, extra), credentials: 'same-origin' };
    if (body !== undefined) {
      init.body = JSON.stringify(body);
    }
    const url = path.startsWith('http') || path.startsWith('/') ? path : this.url + path;
    const resp = await f(this.withLanguage(url), init);
    if (!resp.ok && resp.status !== 304) {
      throw parseError(resp.status, await resp.text());
    }
    return resp;
  }

  /** The metadata of the service, read once. */
  metadata(): Promise<ServiceModel> {
    if (!this.model) {
      this.model = this.send('GET', '$metadata?$format=json', undefined, { Accept: 'application/json' })
        .then((r) => r.json())
        .then(parseMetadata);
      this.model.catch(() => {
        this.model = null;
      });
    }
    return this.model;
  }

  /** Reads a page of entities of an entity set. */
  async query(set: string, q: Query = {}): Promise<Page> {
    const params: string[] = [];
    if (q.filter) params.push(`$filter=${encodeURIComponent(q.filter)}`);
    if (q.orderby) params.push(`$orderby=${encodeURIComponent(q.orderby)}`);
    if (q.top !== undefined) params.push(`$top=${q.top}`);
    if (q.skip) params.push(`$skip=${q.skip}`);
    if (q.count) params.push('$count=true');
    if (q.select?.length) params.push(`$select=${q.select.map(encodeURIComponent).join(',')}`);
    if (q.expand?.length) params.push(`$expand=${q.expand.map(encodeURIComponent).join(',')}`);
    return this.page(`${set}${params.length ? `?${params.join('&')}` : ''}`);
  }

  /** Reads the page a nextLink addresses. */
  async page(path: string): Promise<Page> {
    const body = (await (await this.send('GET', path)).json()) as Record<string, unknown>;
    const count = body['@odata.count'];
    return {
      value: (body.value as Entity[]) ?? [],
      count: typeof count === 'number' ? count : typeof count === 'string' ? Number(count) : null,
      nextLink: typeof body['@odata.nextLink'] === 'string' ? (body['@odata.nextLink'] as string) : null,
    };
  }

  /** The number of entities of an entity set that match a filter. */
  async count(set: string, filter?: string): Promise<number> {
    const text = await (await this.send('GET', `${set}/$count${filter ? `?$filter=${encodeURIComponent(filter)}` : ''}`)).text();
    return Number(text);
  }

  private async setOf(name: string): Promise<EntitySet> {
    const m = await this.metadata();
    const set = m.entitySets[name];
    if (!set) {
      throw new ODataError(404, 'NOT_FOUND', `the service has no entity set ${name}`, null, []);
    }
    return set;
  }

  /** The path of an entity by its key. */
  async entityPath(set: string, key: Entity): Promise<string> {
    return `${set}${keyPredicate((await this.setOf(set)).type, key)}`;
  }

  /** Reads an entity with its ETag. */
  async read(set: string, key: Entity, expand: string[] = []): Promise<Read> {
    return this.readPath(await this.entityPath(set, key), expand);
  }

  /** Reads the entity at a path such as Companies('C1') with its ETag. */
  async readPath(path: string, expand: string[] = []): Promise<Read> {
    const resp = await this.send('GET', expand.length ? `${path}?$expand=${expand.map(encodeURIComponent).join(',')}` : path);
    const entity = (await resp.json()) as Entity;
    return { entity: strip(entity), etag: resp.headers.get('ETag') ?? (entity['@odata.etag'] as string | undefined) ?? null };
  }

  /** Creates an entity, with dependent entities in its navigations. */
  async create(set: string, entity: Entity): Promise<Changed> {
    const resp = await this.send('POST', set, entity, { Prefer: 'return=representation' });
    return changed(resp);
  }

  /** Changes properties of an entity read with etag; 412 if another changed it since. */
  async update(set: string, key: Entity, patch: Entity, etag: string | null): Promise<Changed> {
    const extra: Record<string, string> = { Prefer: 'return=representation' };
    if (etag) extra['If-Match'] = etag;
    const resp = await this.send('PATCH', await this.entityPath(set, key), patch, extra);
    return changed(resp);
  }

  /** Deletes an entity read with etag. */
  async remove(set: string, key: Entity, etag: string | null): Promise<void> {
    await this.send('DELETE', await this.entityPath(set, key), undefined, etag ? { 'If-Match': etag } : {});
  }

  /**
   * Runs an action: bound to an entity with set and key, else an action
   * import. It returns the result and the messages.
   */
  async action(name: string, params: Record<string, Value>, bound?: { set: string; key: Entity; etag: string | null }): Promise<Changed> {
    let path = name;
    const extra: Record<string, string> = {};
    if (bound) {
      path = `${await this.entityPath(bound.set, bound.key)}/${name}`;
      if (bound.etag) extra['If-Match'] = bound.etag;
    }
    return changed(await this.send('POST', path, params, extra));
  }
}

/** An entity without the annotations of control such as @odata.context. */
export function strip(entity: Entity): Entity {
  const out: Entity = {};
  for (const [k, v] of Object.entries(entity)) {
    if (!k.startsWith('@')) {
      out[k] = v;
    }
  }
  return out;
}

async function changed(resp: Response): Promise<Changed> {
  const etag = resp.headers.get('ETag');
  if (resp.status === 204) {
    return { entity: null, etag, messages: [] };
  }
  const text = await resp.text();
  if (!text) {
    return { entity: null, etag, messages: [] };
  }
  const body = JSON.parse(text) as Record<string, unknown>;
  return { entity: strip(body as Entity), etag: etag ?? (body['@odata.etag'] as string | undefined) ?? null, messages: coreMessages(body) };
}
