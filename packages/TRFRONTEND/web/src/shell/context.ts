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

// The context the shell gives the component of a remote app besides its
// parameters: clients of OData services and JSON requests to nodes with
// the CSRF token and the language of the session, navigation to other
// apps and toasts. A remote that only reads params keeps working.

import { ODataClient } from '../odata/index.ts';
import { currentCsrfToken, fetchFile, fetchJSON, type JSONInit } from './api.ts';

/** The tone of a toast of notify. */
export type NotifyTone = 'information' | 'success' | 'warning' | 'error';

export interface RemoteContext {
  /** The language of the session, e.g. en. */
  readonly language: string;
  /**
   * The client of an OData service by its name, such as
   * /TRFIN/JOURNAL_ENTRY_SERVICE, or by its URL; one per service.
   */
  odata(service: string): ODataClient;
  /** A JSON request to a node, such as /trfin/reports/balances; RequestError for failures. */
  fetchJSON<T = unknown>(path: string, init?: JSONInit): Promise<T>;
  /**
   * Requests a file from a node and saves it in the browser, such as
   * /trfin/reports/balances?format=xlsx with { body: selection }: POST with
   * a body, else GET, the CSRF token for changes and the language of the
   * session. The name comes from Content-Disposition; the promise gives it.
   * Failures throw a RequestError as fetchJSON does.
   */
  download(path: string, init?: JSONInit): Promise<string>;
  /**
   * Opens another app of the user with parameters; a list app opens at an
   * entity by its path in the service, such as
   * JournalEntries(COMPANY='1000',FISCAL_YEAR=2026,DOCUMENT_NO='0000000001')
   * from odata(service).entityPath(set, key).
   */
  navigate(app: string, params?: Record<string, string>, entity?: string | null): void;
  /** Shows a toast, by default of the tone information. */
  notify(message: string, tone?: NotifyTone): void;
}

/** The properties of the component of a remote app. */
export interface RemoteProps {
  params: Record<string, string>;
  context: RemoteContext;
}

export interface RemoteContextOptions {
  language: string;
  navigate: (app: string, params: Record<string, string>, entity: string | null) => void;
  notify: (message: string, tone: NotifyTone) => void;
  /** Called when an answer is 401: the session is gone. */
  onSignedOut: () => void;
  /** The CSRF token; by default that of the session. */
  csrfToken?: () => string | null;
  /** fetch, for tests; by default the global one at the time of the request. */
  fetch?: typeof fetch;
  /** Saves a downloaded file, for tests; by default through a link with download. */
  save?: (blob: Blob, filename: string) => void;
}

/** Saves a file in the browser through a temporary link with the attribute download. */
export function saveFile(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.style.display = 'none';
  document.body.append(a);
  a.click();
  a.remove();
  // The browser reads the URL after the click; revoke it later.
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

const serviceName = /^\/([A-Z0-9_]+)\/([A-Z0-9_]+)$/;

/**
 * The URL of a service below /rt/odata: /TRFIN/JOURNAL_ENTRY_SERVICE is
 * /rt/odata/trfin/journal_entry_service/; other values are URLs already.
 */
export function serviceUrl(service: string): string {
  const m = serviceName.exec(service);
  if (m) {
    return `/rt/odata/${(m[1] ?? '').toLowerCase()}/${(m[2] ?? '').toLowerCase()}/`;
  }
  return service.endsWith('/') ? service : `${service}/`;
}

/** The context of a remote app. */
export function createRemoteContext(options: RemoteContextOptions): RemoteContext {
  const base = options.fetch ?? ((input: RequestInfo | URL, init?: RequestInit) => fetch(input, init));
  const watched = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const resp = await base(input, init);
    if (resp.status === 401) {
      options.onSignedOut();
    }
    return resp;
  }) as typeof fetch;
  const csrfToken = options.csrfToken ?? currentCsrfToken;
  const clients = new Map<string, ODataClient>();
  return {
    language: options.language,
    odata(service: string): ODataClient {
      const url = serviceUrl(service);
      let client = clients.get(url);
      if (!client) {
        client = new ODataClient(url, { csrfToken, language: () => options.language, fetch: watched });
        clients.set(url, client);
      }
      return client;
    },
    fetchJSON<T = unknown>(path: string, init: JSONInit = {}): Promise<T> {
      return fetchJSON<T>(path, { ...init, headers: { 'Accept-Language': options.language, ...init.headers } }, { csrfToken, fetch: watched });
    },
    async download(path: string, init: JSONInit = {}): Promise<string> {
      const file = await fetchFile(path, { ...init, headers: { 'Accept-Language': options.language, ...init.headers } }, { csrfToken, fetch: watched });
      (options.save ?? saveFile)(file.blob, file.filename);
      return file.filename;
    },
    navigate(app: string, params: Record<string, string> = {}, entity: string | null = null) {
      options.navigate(app, params, entity);
    },
    notify(message: string, tone: NotifyTone = 'information') {
      options.notify(message, tone);
    },
  };
}
