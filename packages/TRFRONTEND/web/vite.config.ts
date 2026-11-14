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

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// React and TRUI come from /trui/lib/ through the import map of the shell,
// so that they load once and remote apps share them; the build leaves them
// out.
export const shared = [
  'react',
  'react-dom',
  'react-dom/client',
  'react/jsx-runtime',
  '@tim-riep/trui',
  '@tim-riep/trui/core',
  '@tim-riep/trui/icons',
];

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist-web',
    emptyOutDir: true,
    sourcemap: true,
    target: 'es2023',
    lib: { entry: 'src/main.tsx', formats: ['es'], fileName: () => 'shell.js' },
    rollupOptions: {
      external: shared,
      output: { chunkFileNames: 'chunks/[name]-[hash].js', assetFileNames: 'chunks/[name]-[hash][extname]' },
    },
  },
});
