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


// Sets up go-runtime for the browser tests as the README describes: a new
// SQLite database, TRUI, the built TRFRONTEND, TRFIN, TROTTO and their UI
// packages, active nodes and services, the users clara and admin, and
// rt serve on a free port. TRUI comes from TRUI_PACKAGE, a directory or ZIP
// of the package (the release asset of tim-riep/trui).

import { type ChildProcess, execFileSync, spawn } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import type { TestProject } from 'vitest/node';

const repo = resolve(import.meta.dirname, '../../../../..');
const examples = join(repo, 'examples');

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
  const pkg = join(repo, 'packages/TRFRONTEND/dist/TRFRONTEND');
  if (!existsSync(pkg)) {
    throw new Error('build the package first: npm run build');
  }
  dir = mkdtempSync(join(tmpdir(), 'trfrontend-e2e-'));
  const rt = join(dir, 'rt');
  execFileSync('go', ['build', '-o', rt, './cmd/rt'], { cwd: repo, stdio: 'inherit' });
  const env = { ...process.env, RT_DATABASE: `sqlite:${join(dir, 'rt.db')}` };
  const run = (args: string[], input?: string) => execFileSync(rt, args, { cwd: repo, env, input, stdio: ['pipe', 'pipe', 'inherit'] });
  run(['init', '--admin', 'admin', '--tenant', 'ACME'], 'secret\n');
  run(['language', 'add', 'de']);
  run(['install', trui, pkg, join(examples, 'TRFIN'), join(examples, 'TROTTO'), join(examples, 'TRFINUI'), join(examples, 'TROTTOUI'), '--yes']);
  for (const node of ['/trui', '/trfrontend']) run(['http', 'activate', node]);
  for (const service of ['/TRFIN/COMPANY_SERVICE', '/TRFRONTEND/CONFIGURATION_SERVICE']) run(['service', 'activate', service]);
  run(['user', 'create', 'clara', '--tenant', 'ACME'], 'secret\n');
  for (const role of ['/TRFRONTEND/USER', '/TRFIN/COMPANY_CLERK', '/TRFIN/COMPANY_ADMIN', '/TRFIN/ODATA_USER']) run(['role', 'assign', 'clara', role, '--tenant', 'ACME']);
  run(['role', 'assign', 'admin', '/TRFRONTEND/ADMINISTRATOR', '--tenant', 'ACME']);
  for (const [code, country] of [['DE', 'Germany'], ['AT', 'Austria']]) {
    run(['run', 'TRFIN', '/TRFIN/COMPANY_SERVICE', 'ADD_COUNTRY', JSON.stringify([code, country]), '--tenant', 'ACME', '--user', 'admin']);
  }

  const port = await freePort();
  server = spawn(rt, ['serve', '--addr', `127.0.0.1:${port}`, '--insecure-session-cookie'], { cwd: repo, env, stdio: ['ignore', 'ignore', 'pipe'] });
  const url = `http://127.0.0.1:${port}`;
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`${url}/trfrontend/`)).ok) break;
    } catch {
      // Not yet listening.
    }
    await new Promise((r) => setTimeout(r, 100));
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
