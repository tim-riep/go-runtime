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


import { Banner, Spinner } from '@tim-riep/trui';
import { useToast } from '@tim-riep/trui/core';
import { type ComponentType, useEffect, useMemo, useRef, useState } from 'react';
import { ListApp } from '../pages/ListApp.tsx';
import type { HomeApp } from './api.ts';
import { createRemoteContext, type RemoteProps } from './context.ts';
import { loadRemoteApp } from './remote.ts';
import type { TextKey } from './texts.ts';

export interface AppHostProps {
  app: HomeApp | null;
  params: Record<string, string>;
  entity: string | null;
  texts: Record<TextKey, string>;
  /** The language of the session, for the context of remote apps. */
  language: string;
  onNavigate: (entity: string | null) => void;
  /** Opens another app with parameters, a list app at an entity, for the context of remote apps. */
  onOpen: (app: string, params: Record<string, string>, entity?: string | null) => void;
  onSignedOut: () => void;
}

/** The parameters of an app with those of the address, which win. */
export function parametersOf(app: HomeApp, params: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const p of app.PARAMETERS) {
    if (p.VALUE !== null) {
      out[p.NAME] = p.VALUE;
    }
  }
  return { ...out, ...params };
}

/** The address of a page app with its parameters. */
export function pageUrl(app: HomeApp, params: Record<string, string>): string {
  const url = app.TARGET.URL ?? '';
  const q = new URLSearchParams(parametersOf(app, params)).toString();
  return q ? `${url}${url.includes('?') ? '&' : '?'}${q}` : url;
}

interface RemoteHostProps {
  app: HomeApp;
  params: Record<string, string>;
  texts: Record<TextKey, string>;
  language: string;
  onOpen: (app: string, params: Record<string, string>, entity?: string | null) => void;
  onSignedOut: () => void;
}

function Remote({ app, params, texts, language, onOpen, onSignedOut }: RemoteHostProps) {
  const [component, setComponent] = useState<{ C: ComponentType<RemoteProps> } | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const toast = useToast();
  // The context stays the same for a language, so that remotes may depend
  // on it in effects; the callbacks behind it are always the current ones.
  const latest = useRef({ onOpen, onSignedOut, toast });
  latest.current = { onOpen, onSignedOut, toast };
  const context = useMemo(
    () =>
      createRemoteContext({
        language,
        navigate: (a, p, e) => latest.current.onOpen(a, p, e),
        notify: (message, tone) => {
          latest.current.toast.show({ message, tone });
        },
        onSignedOut: () => latest.current.onSignedOut(),
      }),
    [language],
  );
  useEffect(() => {
    let current = true;
    setComponent(null);
    setFailed(null);
    loadRemoteApp(app.NAME, app.TARGET.URL ?? '', app.TARGET.MODULE ?? '.').then(
      (C) => current && setComponent({ C }),
      (e: unknown) => current && setFailed(e instanceof Error ? e.message : String(e)),
    );
    return () => {
      current = false;
    };
  }, [app.NAME, app.TARGET.URL, app.TARGET.MODULE]);
  if (failed !== null) {
    return (
      <Banner tone="error" title={texts.remoteFailed}>
        {failed}
      </Banner>
    );
  }
  if (!component) {
    return <Spinner size="l" />;
  }
  const C = component.C;
  return <C params={parametersOf(app, params)} context={context} />;
}

/** The content area of an opened app by the kind of its target. */
export function AppHost({ app, params, entity, texts, language, onNavigate, onOpen, onSignedOut }: AppHostProps) {
  if (!app) {
    return <Banner tone="warning">{texts.appUnknown}</Banner>;
  }
  switch (app.TARGET.KIND) {
    case 'LIST':
      return <ListApp app={app} entity={entity} params={params} texts={texts} onNavigate={onNavigate} onSignedOut={onSignedOut} />;
    case 'PAGE':
      return <iframe className="trfrontend-page" title={app.TITLE} src={pageUrl(app, params)} />;
    case 'REMOTE':
      return <Remote app={app} params={params} texts={texts} language={language} onOpen={onOpen} onSignedOut={onSignedOut} />;
    case 'URL':
      return <Banner tone="information">{texts.opened}</Banner>;
  }
}
