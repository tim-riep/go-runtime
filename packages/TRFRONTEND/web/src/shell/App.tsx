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


import { Banner, Button, type MenuItem, ShellHeader, ShellLayout, Spinner } from '@tim-riep/trui';
import { type Density, type ThemeSetting, TruiProvider } from '@tim-riep/trui/core';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { changeSession, type Home as HomeData, type HomeApp, readHome, readSession, RequestError, type Session, signOut } from './api.ts';
import { AppHost, pageUrl } from './AppHost.tsx';
import { Home } from './Home.tsx';
import { defined } from './defined.ts';
import { iconOf } from './icons.ts';
import { Login } from './Login.tsx';
import { parseRoute, type Route, routeHash } from './route.ts';
import { textsOf } from './texts.ts';

const themes: ThemeSetting[] = ['auto', 'light', 'dark'];
const densities: Density[] = ['compact', 'comfortable'];

/** A setting of this browser, kept in localStorage where it works. */
function stored<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const v = localStorage.getItem(key);
    return v !== null && (allowed as readonly string[]).includes(v) ? (v as T) : fallback;
  } catch {
    return fallback;
  }
}

function store(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Private windows may refuse; the setting lasts for this page then.
  }
}

function navigate(r: Route) {
  window.location.hash = routeHash(r);
}

/** The shell of TRFRONTEND: sign-in, header, start page and the opened app. */
export function App() {
  const [session, setSession] = useState<Session | null | undefined>(undefined);
  const [notice, setNotice] = useState<string | null>(null);
  const [route, setRoute] = useState<Route>(() => parseRoute(window.location.hash));
  const [home, setHome] = useState<HomeData | null>(null);
  const [homeFailed, setHomeFailed] = useState(false);
  const [theme, setTheme] = useState<ThemeSetting>(() => stored('trfrontend.theme', themes, 'auto'));
  const [density, setDensity] = useState<Density>(() => stored('trfrontend.density', densities, 'compact'));
  const texts = textsOf(session?.language);

  useEffect(() => {
    const changed = () => setRoute(parseRoute(window.location.hash));
    window.addEventListener('hashchange', changed);
    return () => window.removeEventListener('hashchange', changed);
  }, []);

  useEffect(() => {
    readSession().then(setSession, () => setSession(null));
  }, []);

  /** Every request that finds the session gone leads to the sign-in. */
  const failed = useCallback(
    (e: unknown) => {
      if (e instanceof RequestError && e.signedOut) {
        setSession(null);
        setHome(null);
        setNotice(textsOf(session?.language).sessionExpired);
        return true;
      }
      return false;
    },
    [session?.language],
  );

  const loadHome = useCallback(() => {
    setHomeFailed(false);
    readHome().then(setHome, (e: unknown) => {
      if (!failed(e)) {
        setHomeFailed(true);
      }
    });
  }, [failed]);

  const user = session?.user;
  const language = session?.language;
  const tenant = session?.tenant;
  // The start page depends on user, language and tenant of the session.
  useEffect(() => {
    if (user) {
      loadHome();
    }
  }, [user, language, tenant]);

  const apps = useMemo(() => new Map((home?.APPS ?? []).map((a) => [a.NAME, a])), [home]);

  const open = useCallback(
    (name: string, params: Record<string, string> = {}) => {
      const app = apps.get(name);
      if (app?.TARGET.KIND === 'URL') {
        window.open(pageUrl(app, params), '_blank', 'noopener');
        return;
      }
      navigate({ kind: 'app', app: name, params, entity: null });
    },
    [apps],
  );

  if (session === undefined) {
    return (
      <TruiProvider theme={theme} density={density}>
        <Spinner size="l" label={texts.title} />
      </TruiProvider>
    );
  }

  if (session === null) {
    return (
      <TruiProvider theme={theme} density={density}>
        <Login
          texts={texts}
          notice={notice}
          onSignedIn={(s) => {
            setNotice(null);
            setSession(s);
            if (route.kind === 'login') {
              window.location.hash = route.back ?? '#/';
            }
          }}
        />
      </TruiProvider>
    );
  }

  const change = (c: { tenant?: string; language?: string }) => {
    changeSession(c).then(setSession, (e: unknown) => failed(e));
  };

  const check = iconOf('check');
  const userMenu: MenuItem[] = [
    {
      key: 'language',
      label: texts.language,
      items: session.languages.map((l) => defined({ key: `language:${l.code}`, label: l.name, icon: l.code === session.language ? check : undefined })),
    },
  ];
  if (session.tenants.length > 1) {
    userMenu.push({
      key: 'tenant',
      label: texts.tenant,
      items: session.tenants.map((t) => defined({ key: `tenant:${t}`, label: t, icon: t === session.tenant ? check : undefined })),
    });
  }
  userMenu.push(
    {
      key: 'theme',
      label: texts.theme,
      items: [
        defined({ key: 'theme:auto', label: texts.themeAuto, icon: theme === 'auto' ? check : undefined }),
        defined({ key: 'theme:light', label: texts.themeLight, icon: theme === 'light' ? check : undefined }),
        defined({ key: 'theme:dark', label: texts.themeDark, icon: theme === 'dark' ? check : undefined }),
      ],
    },
    {
      key: 'density',
      label: texts.density,
      items: [
        defined({ key: 'density:compact', label: texts.densityCompact, icon: density === 'compact' ? check : undefined }),
        defined({ key: 'density:comfortable', label: texts.densityComfortable, icon: density === 'comfortable' ? check : undefined }),
      ],
    },
    { key: 'sign-out', label: texts.signOut, separatorBefore: true },
  );

  const onUserMenuAction = (key: string) => {
    const [kind, value = ''] = key.split(/:(.*)/s, 2);
    if (kind === 'language') {
      change({ language: value });
    } else if (kind === 'tenant') {
      change({ tenant: value });
    } else if (kind === 'theme' && (themes as string[]).includes(value)) {
      setTheme(value as ThemeSetting);
      store('trfrontend.theme', value);
    } else if (kind === 'density' && (densities as string[]).includes(value)) {
      setDensity(value as Density);
      store('trfrontend.density', value);
    } else if (kind === 'sign-out') {
      signOut().finally(() => {
        setSession(null);
        setHome(null);
        setNotice(null);
        navigate({ kind: 'home', space: null });
      });
    }
  };

  const search = (query: string): HomeApp[] => {
    const q = query.trim().toLowerCase();
    return (home?.APPS ?? []).filter((a) => a.TITLE.toLowerCase().includes(q) || (a.SUBTITLE ?? '').toLowerCase().includes(q));
  };

  const app = route.kind === 'app' ? (apps.get(route.app) ?? null) : null;
  const space = route.kind === 'home' ? route.space : null;

  let content = <Spinner size="l" />;
  if (homeFailed) {
    content = (
      <Banner
        tone="error"
        actions={
          <Button variant="secondary" onClick={loadHome}>
            {texts.retry}
          </Button>
        }
      >
        {texts.loadFailed}
      </Banner>
    );
  } else if (home) {
    content = (
      <Home home={home} texts={texts} space={space} onSpaceChange={(s) => navigate({ kind: 'home', space: s })} onOpen={(a) => open(a)} />
    );
  }

  return (
    <TruiProvider theme={theme} density={density} locale={session.language}>
      <ShellLayout
        view={route.kind === 'app' ? 'app' : 'home'}
        keepAppMounted={false}
        header={
          <ShellHeader
            title={app?.TITLE ?? texts.title}
            {...defined({ onHome: route.kind === 'app' ? () => navigate({ kind: 'home', space: null }) : undefined })}
            search={{
              placeholder: texts.search,
              loadSuggestions: async (query) =>
                search(query).map((a) => defined({ value: a.NAME, label: a.TITLE, description: a.SUBTITLE ?? undefined, icon: iconOf(a.ICON) })),
              onSearch: (query) => {
                const first = search(query)[0];
                if (first) open(first.NAME);
              },
              onSuggestionSelect: (item) => open(item.value),
            }}
            areas={(home?.SPACES ?? []).map((s) => ({ key: s.NAME, label: s.TITLE }))}
            onAreaChange={(s) => navigate({ kind: 'home', space: s })}
            user={{ name: session.name ?? session.user }}
            userMenu={userMenu}
            onUserMenuAction={onUserMenuAction}
          />
        }
        home={content}
        {...defined({
          app:
            route.kind === 'app' && home ? (
              <AppHost
                key={route.app}
                app={app}
                params={route.params}
                entity={route.entity}
                texts={texts}
                onNavigate={(e) => navigate({ kind: 'app', app: route.app, params: route.params, entity: e })}
                onSignedOut={() => failed(new RequestError(401, 'session-expired', ''))}
              />
            ) : undefined,
        })}
      />
    </TruiProvider>
  );
}
