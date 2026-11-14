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

// Checks that every source file of the user interface starts with the
// Apache 2.0 license header.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const web = resolve(import.meta.dirname, '..');
const skip = new Set(['node_modules', 'dist-web', 'test-results', 'fixtures']);
const missing = [];

function walk(dir) {
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (skip.has(name)) continue;
    if (statSync(full).isDirectory()) {
      walk(full);
    } else if (/\.(ts|tsx|mjs|js)$/.test(name) && !readFileSync(full, 'utf8').startsWith('// Copyright 2026 The go-runtime Authors')) {
      missing.push(relative(web, full));
    }
  }
}

walk(web);
if (missing.length) {
  console.error(`Files without license header:\n  ${missing.join('\n  ')}`);
  process.exit(1);
}
