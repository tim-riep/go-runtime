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


import { defineConfig } from 'vitest/config';

// Browser tests of the built package against rt serve: test/e2e/setup.ts
// builds rt, sets up a database as the README describes and serves it.
export default defineConfig({
  test: {
    name: 'e2e',
    environment: 'node',
    include: ['test/e2e/**/*.e2e.ts'],
    globalSetup: ['test/e2e/setup.ts'],
    testTimeout: 60_000,
    hookTimeout: 180_000,
    fileParallelism: false,
  },
});
