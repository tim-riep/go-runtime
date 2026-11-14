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


// Loads apps of the kind REMOTE with Module Federation. The remote shares
// React and TRUI with the shell: the shell offers the modules it loaded
// through the import map as singletons, so that there is one React and one
// TruiProvider. A remote renders without a provider and without styles.

import { createInstance } from '@module-federation/runtime';
import * as React from 'react';
import * as ReactDOM from 'react-dom';
import * as ReactDOMClient from 'react-dom/client';
import * as JSXRuntime from 'react/jsx-runtime';
import * as Trui from '@tim-riep/trui';
import * as TruiCore from '@tim-riep/trui/core';
import * as TruiIcons from '@tim-riep/trui/icons';
import type { ComponentType } from 'react';

const reactVersion = React.version;
const truiVersion = '0.2.0';

function share(lib: unknown, version: string, requiredVersion: string) {
  return {
    version,
    scope: 'default',
    lib: () => lib,
    shareConfig: { singleton: true, requiredVersion },
  };
}

let instance: ReturnType<typeof createInstance> | null = null;

function federation() {
  instance ??= createInstance({
    name: 'trfrontend_shell',
    remotes: [],
    shared: {
      react: share(React, reactVersion, '^19.0.0'),
      'react-dom': share(ReactDOM, reactVersion, '^19.0.0'),
      'react-dom/client': share(ReactDOMClient, reactVersion, '^19.0.0'),
      'react/jsx-runtime': share(JSXRuntime, reactVersion, '^19.0.0'),
      '@tim-riep/trui': share(Trui, truiVersion, '^0.1.0'),
      '@tim-riep/trui/core': share(TruiCore, truiVersion, '^0.1.0'),
      '@tim-riep/trui/icons': share(TruiIcons, truiVersion, '^0.1.0'),
    },
  });
  return instance;
}

/** The name under which a remote is registered, from the name of its app. */
export function remoteName(app: string): string {
  return app.replace(/^\//, '').replace(/[^A-Za-z0-9_]/g, '_').toLowerCase();
}

/**
 * Loads the exposed module of a remote whose manifest (mf-manifest.json) or
 * entry is at url. The module exports the component of the app as default.
 */
export async function loadRemoteApp(app: string, url: string, module: string): Promise<ComponentType<{ params: Record<string, string> }>> {
  const mf = federation();
  const name = remoteName(app);
  mf.registerRemotes([{ name, entry: url }]);
  const exposed = module.replace(/^\.\//, '');
  const m = (await mf.loadRemote(`${name}/${exposed}`)) as { default?: ComponentType<{ params: Record<string, string> }> } | null;
  if (!m?.default) {
    throw new Error(`the module ${module} of ${url} has no default export`);
  }
  return m.default;
}
