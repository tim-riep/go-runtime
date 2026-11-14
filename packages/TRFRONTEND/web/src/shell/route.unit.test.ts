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


import { describe, expect, it } from 'vitest';
import { parseRoute, routeHash } from './route.ts';

describe('routes', () => {
  it('reads the start page, spaces and the sign-in', () => {
    expect(parseRoute('')).toEqual({ kind: 'home', space: null });
    expect(parseRoute('#/')).toEqual({ kind: 'home', space: null });
    expect(parseRoute('#/space/TRFDEMO/SPACE')).toEqual({ kind: 'home', space: '/TRFDEMO/SPACE' });
    expect(parseRoute('#/login?return=%23%2Fapp%2FX%2FY')).toEqual({ kind: 'login', back: '#/app/X/Y' });
  });

  it('reads apps with parameters and entities', () => {
    expect(parseRoute('#/app/TRFDEMO/COMPANIES?mode=edit')).toEqual({ kind: 'app', app: '/TRFDEMO/COMPANIES', params: { mode: 'edit' }, entity: null });
    expect(parseRoute("#/app/TRFDEMO/COMPANIES/Companies('C%2F1')")).toEqual({
      kind: 'app',
      app: '/TRFDEMO/COMPANIES',
      params: {},
      entity: "Companies('C/1')",
    });
  });

  it('writes what it reads', () => {
    for (const h of ['#/', '#/space/A/B', '#/app/A/B?x=1', "#/app/A/B/Set('k%2F1')", '#/login?return=%23%2Fapp%2FA%2FB']) {
      expect(routeHash(parseRoute(h))).toBe(h);
    }
  });
});
