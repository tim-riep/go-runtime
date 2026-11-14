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

/** A message of the service, about the request or a property (target). */
export interface Message {
  code: string;
  message: string;
  /** The property or path the message is about, if any. */
  target: string | null;
  /** error, warning, info or success. */
  severity: 'error' | 'warning' | 'info' | 'success';
}

/** An error answer of a service, with the messages of its details. */
export class ODataError extends Error {
  readonly status: number;
  readonly code: string;
  readonly target: string | null;
  readonly details: Message[];

  constructor(status: number, code: string, message: string, target: string | null, details: Message[]) {
    super(message);
    this.name = 'ODataError';
    this.status = status;
    this.code = code;
    this.target = target;
    this.details = details;
  }

  /** 412: another user changed the entity since it was read. */
  get conflict(): boolean {
    return this.status === 412;
  }

  /** 401: the session expired or there is none. */
  get unauthorized(): boolean {
    return this.status === 401;
  }

  /** The messages for a property, from the error and its details. */
  messagesFor(property: string): Message[] {
    const all: Message[] = [{ code: this.code, message: this.message, target: this.target, severity: 'error' }, ...this.details];
    return all.filter((m) => m.target === property || m.target?.endsWith(`/${property}`));
  }
}

type Json = Record<string, unknown>;

function message(m: Json, severity: Message['severity']): Message {
  return {
    code: typeof m.code === 'string' ? m.code : '',
    message: typeof m.message === 'string' ? m.message : '',
    target: typeof m.target === 'string' ? m.target : null,
    severity,
  };
}

/** Reads the body of an error answer; other bodies give a generic error. */
export function parseError(status: number, body: string): ODataError {
  try {
    const e = (JSON.parse(body) as Json).error as Json | undefined;
    if (e && typeof e === 'object') {
      const details = Array.isArray(e.details) ? (e.details as Json[]).map((d) => message(d, 'error')) : [];
      return new ODataError(status, String(e.code ?? ''), String(e.message ?? ''), typeof e.target === 'string' ? e.target : null, details);
    }
    const plain = JSON.parse(body) as Json;
    if (typeof plain.code === 'string') {
      return new ODataError(status, plain.code, String(plain.message ?? ''), null, []);
    }
  } catch {
    // The body is no JSON.
  }
  return new ODataError(status, String(status), body.trim() || `HTTP ${status}`, null, []);
}

/** The messages of @Core.Messages of a successful answer. */
export function coreMessages(body: Json): Message[] {
  const raw = body['@Core.Messages'] ?? body['@Org.OData.Core.V1.Messages'];
  if (!Array.isArray(raw)) {
    return [];
  }
  const known = ['error', 'warning', 'info', 'success'];
  return (raw as Json[]).map((m) => {
    if (typeof m.severity === 'string' && known.includes(m.severity)) {
      return message(m, m.severity as Message['severity']);
    }
    const n = typeof m.numericSeverity === 'number' ? m.numericSeverity : 4;
    return message(m, n >= 4 ? 'error' : n === 3 ? 'warning' : n === 2 ? 'info' : 'success');
  });
}
