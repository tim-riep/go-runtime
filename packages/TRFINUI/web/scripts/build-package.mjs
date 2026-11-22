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

// Builds the installable package TRFINUI from its sources and the build of
// the remote apps (vite build into web/dist-web), as TRFRONTEND does:
//
//   packages/TRFINUI/dist/TRFINUI/
//     manifest.json, schema/, objects/   copied from packages/TRFINUI
//     assets/web/                        asset.json of the sources and the build
//
// The version of web/package.json must equal that of the manifest. Paths
// follow the rules of go-runtime for assets; files of the build that only
// tools read (.vite/, mf-stats.json) stay out.

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const web = resolve(import.meta.dirname, '..');
const pkg = resolve(web, '..');
const build = join(web, 'dist-web');
const out = join(pkg, 'dist', 'TRFINUI');

const manifest = JSON.parse(readFileSync(join(pkg, 'manifest.json'), 'utf8'));
const webVersion = JSON.parse(readFileSync(join(web, 'package.json'), 'utf8')).version;
if (manifest.version !== webVersion) {
  console.error(`The version ${webVersion} of web/package.json differs from the version ${manifest.version} of manifest.json.`);
  process.exit(1);
}
for (const f of ['remoteEntry.js', 'mf-manifest.json']) {
  if (!existsSync(join(build, f))) {
    console.error(`The build of the remote apps has no ${f}; run vite build first.`);
    process.exit(1);
  }
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

const skipped = new Set(['.vite', 'mf-stats.json']);
function files(dir) {
  const result = [];
  for (const name of readdirSync(dir)) {
    if (skipped.has(name)) continue;
    const full = join(dir, name);
    if (statSync(full).isDirectory()) {
      result.push(...files(full));
    } else {
      result.push(full);
    }
  }
  return result;
}

const repo = join(out, 'assets', 'web');
const descriptor = JSON.parse(readFileSync(join(repo, 'asset.json'), 'utf8'));
descriptor.files = {
  'mf-manifest.json': { description: { en: 'The manifest of the remote apps for Module Federation', de: 'Das Manifest der Remote-Apps für Module Federation' } },
  'remoteEntry.js': { description: { en: 'The entry of the remote apps', de: 'Der Einstieg der Remote-Apps' } },
};
const folders = new Set();
for (const f of files(build)) {
  const p = relative(build, f).split('\\').join('/');
  checkPath(p);
  const dir = p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '';
  if (dir) folders.add(dir);
  mkdirSync(join(repo, dir), { recursive: true });
  cpSync(f, join(repo, p));
  if (p.endsWith('.map')) {
    descriptor.files[p] = { mimeType: 'application/json' };
  }
}
if (folders.size) {
  descriptor.folders = Object.fromEntries([...folders].sort().map((f) => [f, { description: { en: 'Parts of the remote apps', de: 'Teile der Remote-Apps' } }]));
}
writeFileSync(join(repo, 'asset.json'), `${JSON.stringify(descriptor, null, 2)}\n`);
console.log(`Built ${relative(process.cwd(), out)} ${manifest.version}`);
