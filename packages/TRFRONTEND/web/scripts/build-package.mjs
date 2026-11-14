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

// Builds the installable package TRFRONTEND from its sources and the build
// of the user interface (vite build into web/dist-web):
//
//   packages/TRFRONTEND/dist/TRFRONTEND/
//     manifest.json, schema/, src/, objects/   copied from packages/TRFRONTEND
//     assets/vocabularies/                     copied
//     assets/shell/                            index.html of the sources and the build
//
// The version of web/package.json must equal that of the manifest. Paths
// follow the rules of go-runtime for assets.

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const web = resolve(import.meta.dirname, '..');
const pkg = resolve(web, '..');
const build = join(web, 'dist-web');
const out = join(pkg, 'dist', 'TRFRONTEND');

const manifest = JSON.parse(readFileSync(join(pkg, 'manifest.json'), 'utf8'));
const webVersion = JSON.parse(readFileSync(join(web, 'package.json'), 'utf8')).version;
if (manifest.version !== webVersion) {
  console.error(`The version ${webVersion} of web/package.json differs from the version ${manifest.version} of manifest.json.`);
  process.exit(1);
}
if (!existsSync(join(build, 'shell.js'))) {
  console.error('The build of the user interface is missing; run vite build first.');
  process.exit(1);
}

rmSync(out, { recursive: true, force: true });
mkdirSync(out, { recursive: true });
for (const entry of ['manifest.json', 'schema', 'src', 'objects', 'assets']) {
  if (existsSync(join(pkg, entry))) {
    cpSync(join(pkg, entry), join(out, entry), { recursive: true });
  }
}

// The rules of go-runtime for the paths of assets.
const segment = /^[A-Za-z0-9_-][A-Za-z0-9._-]*$/;
function checkPath(p) {
  const problems = [];
  if (Buffer.byteLength(p) > 255) problems.push('longer than 255 bytes');
  for (const s of p.split('/')) {
    if (!segment.test(s)) problems.push(`segment ${s} has characters other than letters, digits, ., _ and -`);
    if (Buffer.byteLength(s) > 100) problems.push(`segment ${s} is longer than 100 bytes`);
  }
  if (problems.length) {
    console.error(`${p}: ${problems.join('; ')}`);
    process.exit(1);
  }
}

function files(dir) {
  const result = [];
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) {
      result.push(...files(full));
    } else {
      result.push(full);
    }
  }
  return result;
}

const shell = join(out, 'assets', 'shell');
cpSync(build, shell, { recursive: true });
const descriptor = JSON.parse(readFileSync(join(shell, 'asset.json'), 'utf8'));
descriptor.files ??= {};
descriptor.files['shell.js'] = {
  description: { en: 'The shell, which loads React and TRUI from /trui/lib/', de: 'Die Shell, die React und TRUI aus /trui/lib/ lädt' },
};
const folders = new Set();
for (const f of files(shell)) {
  const p = relative(shell, f).split('\\').join('/');
  if (p === 'asset.json') continue;
  checkPath(p);
  const dir = p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '';
  if (dir) folders.add(dir);
  if (p.endsWith('.map')) {
    descriptor.files[p] = { ...(descriptor.files[p] ?? {}), mimeType: 'application/json' };
  }
}
if (folders.size) {
  descriptor.folders = Object.fromEntries([...folders].sort().map((f) => [f, { description: { en: 'Parts of the shell', de: 'Teile der Shell' } }]));
}
writeFileSync(join(shell, 'asset.json'), `${JSON.stringify(descriptor, null, 2)}\n`);
console.log(`Built ${relative(process.cwd(), out)} ${manifest.version}`);
