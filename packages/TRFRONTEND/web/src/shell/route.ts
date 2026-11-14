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

// The navigation of the shell lives in the hash of the address, so that
// the browser goes back and apps can be bookmarked:
//
//   #/                                   the start page, first space
//   #/space/TRFDEMO/COMPANIES_SPACE      a space
//   #/app/TRFDEMO/COMPANIES?mode=edit    an app with parameters
//   #/app/TRFDEMO/COMPANIES/Companies('C1')   an entity of a list app
//   #/login?return=<hash>                the sign-in, then back
//
// Names of objects appear without their leading slash.

export type Route =
  | { kind: 'home'; space: string | null }
  | { kind: 'app'; app: string; params: Record<string, string>; entity: string | null }
  | { kind: 'login'; back: string | null };

function name(path: string): string {
  return `/${path}`;
}

/** Reads a hash such as #/app/TRFDEMO/COMPANIES. */
export function parseRoute(hash: string): Route {
  const raw = hash.startsWith('#') ? hash.slice(1) : hash;
  const [path = '', query = ''] = raw.split('?', 2);
  const params = Object.fromEntries(new URLSearchParams(query));
  if (path === '/login') {
    return { kind: 'login', back: params.return ?? null };
  }
  if (path.startsWith('/space/')) {
    return { kind: 'home', space: name(path.slice('/space/'.length)) };
  }
  if (path.startsWith('/app/')) {
    // An app has a namespace and a name; what follows is the entity.
    const parts = path.slice('/app/'.length).split('/');
    const app = name(parts.slice(0, 2).join('/'));
    const rest = parts.slice(2).join('/');
    return { kind: 'app', app, params, entity: rest ? decodeURIComponent(rest) : null };
  }
  return { kind: 'home', space: null };
}

/** The hash of a route. */
export function routeHash(r: Route): string {
  switch (r.kind) {
    case 'home':
      return r.space ? `#/space${r.space}` : '#/';
    case 'login':
      return r.back ? `#/login?return=${encodeURIComponent(r.back)}` : '#/login';
    case 'app': {
      let h = `#/app${r.app}`;
      if (r.entity) {
        h += `/${encodeURIComponent(r.entity).replaceAll('%2C', ',').replaceAll('%3D', '=').replaceAll('%27', "'").replaceAll('%28', '(').replaceAll('%29', ')')}`;
      }
      const q = new URLSearchParams(r.params).toString();
      return q ? `${h}?${q}` : h;
    }
  }
}
