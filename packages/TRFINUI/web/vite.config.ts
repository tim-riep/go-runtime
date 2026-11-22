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
import { federation } from '@module-federation/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The remote apps of TRFINUI as Module Federation remote below /trfinui/.
// React and TRUI come from the shell of TRFRONTEND, which offers them as
// singletons (packages/TRFRONTEND/web/src/shell/remote.ts); the build has
// no copies of its own (import: false).
const fromShell = (requiredVersion: string) => ({ singleton: true, requiredVersion, import: false as const, suppressMissingImportWarning: true });

export const pages = {
  './JournalEntryPage': './src/pages/JournalEntryPage.tsx',
  './ClearingPage': './src/pages/ClearingPage.tsx',
  './BalancesPage': './src/pages/BalancesPage.tsx',
  './PartnerBalancesPage': './src/pages/PartnerBalancesPage.tsx',
  './OpenItemsPage': './src/pages/OpenItemsPage.tsx',
  './BalanceSheetPage': './src/pages/BalanceSheetPage.tsx',
  './LineItemsPage': './src/pages/LineItemsPage.tsx',
  './NumberRangePage': './src/pages/NumberRangePage.tsx',
};

export default defineConfig({
  base: '/trfinui/',
  plugins: [
    react(),
    federation({
      name: 'trfinui',
      filename: 'remoteEntry.js',
      manifest: true,
      dts: false,
      exposes: pages,
      shared: {
        react: fromShell('^19.0.0'),
        'react-dom': fromShell('^19.0.0'),
        'react/jsx-runtime': fromShell('^19.0.0'),
        '@tim-riep/trui': fromShell('^0.2.0'),
        '@tim-riep/trui/core': fromShell('^0.2.0'),
        '@tim-riep/trui/icons': fromShell('^0.2.0'),
      },
    }),
  ],
  // A remote has no page of its own: the entries are the exposed modules.
  build: { outDir: 'dist-web', emptyOutDir: true, target: 'es2023', rollupOptions: { input: {} } },
});
