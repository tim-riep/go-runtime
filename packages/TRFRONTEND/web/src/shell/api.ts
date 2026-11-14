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

// The session of the kernel at /rt/session and the start page service of
// TRFRONTEND at /trfrontend/api, with the CSRF token of the session for
// every change.

/** The session as GET /rt/session describes it. */
export interface Session {
  user: string;
  name: string | null;
  tenants: string[];
  tenant: string | null;
  language: string;
  languages: Array<{ code: string; name: string }>;
  csrfToken: string;
  expiresAt: string;
}

export interface HomeTarget {
  KIND: 'LIST' | 'PAGE' | 'REMOTE' | 'URL';
  SERVICE: string | null;
  ENTITY_SET: string | null;
  URL: string | null;
  MODULE: string | null;
}

export interface HomeApp {
  NAME: string;
  TITLE: string;
  SUBTITLE: string | null;
  ICON: string | null;
  TARGET: HomeTarget;
  PARAMETERS: Array<{ NAME: string; VALUE: string | null }>;
}

export interface HomeTile {
  NAME: string;
  APP: string;
  KIND: 'SIMPLE' | 'METRIC' | 'CONTENT';
  SIZE: 'SMALL' | 'WIDE' | 'LARGE';
  TITLE: string;
  SUBTITLE: string | null;
  FOOTER: string | null;
  UNIT: string | null;
  CONTENT: string | null;
  ICON: string | null;
  TONE: 'NEUTRAL' | 'GOOD' | 'CRITICAL' | 'BAD' | null;
}

export interface HomeSection {
  ID: string;
  TITLE: string | null;
  TILES: string[];
}

export interface HomePage {
  NAME: string;
  TITLE: string;
  SECTIONS: HomeSection[];
}

export interface HomeSpace {
  NAME: string;
  TITLE: string;
  SHIPPED: boolean;
  PAGES: HomePage[];
}

export interface Home {
  SPACES: HomeSpace[];
  TILES: HomeTile[];
  APPS: HomeApp[];
}

/** An answer of the kernel or of a service that is no success. */
export class RequestError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'RequestError';
    this.status = status;
    this.code = code;
  }

  /** The session expired or there is none: the shell signs in again. */
  get signedOut(): boolean {
    return this.status === 401;
  }
}

let csrfToken: string | null = null;

/** The CSRF token of the current session, for the OData client. */
export function currentCsrfToken(): string | null {
  return csrfToken;
}

let language: string | null = null;

/** The language of the session, for requests that name it. */
export function currentLanguage(): string | null {
  return language;
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (method !== 'GET' && csrfToken) {
    headers['X-CSRF-Token'] = csrfToken;
  }
  const init: RequestInit = { method, headers, credentials: 'same-origin' };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
  }
  const resp = await fetch(url, init);
  const text = await resp.text();
  if (!resp.ok) {
    let code = String(resp.status);
    let message = text;
    try {
      const e = JSON.parse(text) as { code?: string; message?: string; error?: { code?: string; message?: string } };
      code = e.code ?? e.error?.code ?? code;
      message = e.message ?? e.error?.message ?? message;
    } catch {
      // No JSON.
    }
    throw new RequestError(resp.status, code, message);
  }
  return (text ? JSON.parse(text) : null) as T;
}

function remember(s: Session): Session {
  csrfToken = s.csrfToken;
  language = s.language;
  return s;
}

/** The current session, or null without one. */
export async function readSession(): Promise<Session | null> {
  try {
    return remember(await request<Session>('GET', '/rt/session'));
  } catch (e) {
    if (e instanceof RequestError && e.signedOut) {
      csrfToken = null;
      return null;
    }
    throw e;
  }
}

export async function signIn(user: string, password: string): Promise<Session> {
  return remember(await request<Session>('POST', '/rt/session', { user, password }));
}

export async function signOut(): Promise<void> {
  await request<null>('DELETE', '/rt/session');
  csrfToken = null;
}

export async function changeSession(change: { tenant?: string; language?: string }): Promise<Session> {
  return remember(await request<Session>('PATCH', '/rt/session', change));
}

export function readHome(): Promise<Home> {
  return request<Home>('GET', '/trfrontend/api/home');
}

export async function readTileValue(name: string): Promise<string> {
  const v = await request<{ NAME: string; VALUE: number | string }>('GET', `/trfrontend/api/tile-value?name=${encodeURIComponent(name)}`);
  return String(v.VALUE);
}
