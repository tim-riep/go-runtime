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
// Sets up go-runtime for the browser tests as docs/trfinui.md describes: a
// new SQLite database, TRUI, the built TRFRONTEND and TRFINUI, TRBP, TRFIN
// and TRBPUI, active nodes and services, the sample data of TRFIN, the
// accountant anna, and rt serve on a free port. Every browser gets an
// invoice of its own to clear. TRUI comes from TRUI_PACKAGE, a directory
// or ZIP of the package (the release asset of tim-riep/trui).

import { type ChildProcess, execFileSync, spawn } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import type { TestProject } from 'vitest/node';
import { invoices } from './data.ts';

const repo = resolve(import.meta.dirname, '../../../../..');
const packages = join(repo, 'packages');


function freePort(): Promise<number> {
  return new Promise((ok, fail) => {
    const s = createServer();
    s.listen(0, '127.0.0.1', () => {
      const a = s.address();
      s.close(() => (typeof a === 'object' && a ? ok(a.port) : fail(new Error('no port'))));
    });
  });
}

let server: ChildProcess | null = null;
let dir = '';

export async function setup(project: TestProject) {
  const trui = process.env.TRUI_PACKAGE;
  if (!trui || !existsSync(trui)) {
    throw new Error('TRUI_PACKAGE must name the package TRUI, a directory or ZIP archive');
  }
  const frontend = join(packages, 'TRFRONTEND/dist/TRFRONTEND');
  const ui = join(packages, 'TRFINUI/dist/TRFINUI');
  if (!existsSync(frontend) || !existsSync(ui)) {
    throw new Error('build the packages first: make trfrontend trfinui');
  }
  dir = mkdtempSync(join(tmpdir(), 'trfinui-e2e-'));
  const rt = join(dir, 'rt');
  execFileSync('go', ['build', '-o', rt, './cmd/rt'], { cwd: repo, stdio: 'inherit' });
  const env = { ...process.env, RT_DATABASE: `sqlite:${join(dir, 'rt.db')}` };
  const run = (args: string[], input?: string) => execFileSync(rt, args, { cwd: repo, env, input, stdio: ['pipe', 'pipe', 'inherit'] });
  run(['init', '--admin', 'admin', '--tenant', 'ACME'], 'secret\n');
  run(['language', 'add', 'de']);
  run(['install', trui, frontend, join(packages, 'TRBP'), join(packages, 'TRFIN'), join(packages, 'TRBPUI'), ui, '--yes']);
  for (const node of ['/trui', '/trfrontend', '/trfin', '/trfinui']) run(['http', 'activate', node]);
  for (const service of ['/TRBP/PARTNER_SERVICE', '/TRFIN/CONFIGURATION_SERVICE', '/TRFIN/MASTER_DATA_SERVICE', '/TRFIN/JOURNAL_ENTRY_SERVICE']) run(['service', 'activate', service]);
  for (const role of ['/TRFIN/CONFIGURATION', '/TRFIN/MASTER_DATA', '/TRFIN/ACCOUNTANT', '/TRBP/PARTNER_ADMIN']) run(['role', 'assign', 'admin', role, '--tenant', 'ACME']);
  run(['run', 'TRFIN', '/TRFIN/SAMPLE_DATA', 'SETUP', '[]', '--tenant', 'ACME', '--user', 'admin']);
  run(['user', 'create', 'anna', '--tenant', 'ACME'], 'secret\n');
  for (const role of ['/TRFRONTEND/USER', '/TRFIN/ACCOUNTANT', '/TRFIN/ODATA_USER', '/TRFINUI/USER']) run(['role', 'assign', 'anna', role, '--tenant', 'ACME']);

  const port = await freePort();
  // The log is not read: a pipe nobody reads fills up and blocks the server.
  server = spawn(rt, ['serve', '--addr', `127.0.0.1:${port}`, '--insecure-session-cookie'], { cwd: repo, env, stdio: 'ignore' });
  const url = `http://127.0.0.1:${port}`;
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`${url}/trfrontend/`)).ok) break;
    } catch {
      // Not yet listening.
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  // An invoice of the customer 0000000001 per browser, to clear with a payment.
  for (const amount of Object.values(invoices)) {
    const resp = await fetch(`${url}/rt/odata/trfin/journal_entry_service/POST`, {
      method: 'POST',
      headers: { Authorization: `Basic ${Buffer.from('anna:secret').toString('base64')}`, 'X-Tenant': 'ACME', 'Content-Type': 'application/json' },
      body: JSON.stringify({
        COMPANY: '1000',
        DOCUMENT_TYPE: 'DR',
        DOCUMENT_DATE: '2026-11-02',
        POSTING_DATE: '2026-11-02',
        HEADER_TEXT: 'Invoice for the browser tests',
        ITEMS: [
          { ACCOUNT_KIND: 'CUSTOMER', PARTNER: '0000000001', DEBIT_CREDIT: 'D', AMOUNT: amount },
          { ACCOUNT: '400000', DEBIT_CREDIT: 'C', AMOUNT: amount },
        ],
      }),
    });
    if (!resp.ok) {
      throw new Error(`invoice ${amount}: ${resp.status} ${await resp.text()}`);
    }
  }
  project.provide('baseURL', url);
}

export function teardown() {
  server?.kill();
  if (dir) rmSync(dir, { recursive: true, force: true });
}

declare module 'vitest' {
  export interface ProvidedContext {
    baseURL: string;
  }
}
