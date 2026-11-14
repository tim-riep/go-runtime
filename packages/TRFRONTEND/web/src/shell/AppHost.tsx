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
import { type ComponentType, useEffect, useState } from 'react';
import { ListApp } from '../pages/ListApp.tsx';
import type { HomeApp } from './api.ts';
import { loadRemoteApp } from './remote.ts';
import type { TextKey } from './texts.ts';

export interface AppHostProps {
  app: HomeApp | null;
  params: Record<string, string>;
  entity: string | null;
  texts: Record<TextKey, string>;
  onNavigate: (entity: string | null) => void;
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

function Remote({ app, params, texts }: { app: HomeApp; params: Record<string, string>; texts: Record<TextKey, string> }) {
  const [component, setComponent] = useState<{ C: ComponentType<{ params: Record<string, string> }> } | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
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
  return <C params={parametersOf(app, params)} />;
}

/** The content area of an opened app by the kind of its target. */
export function AppHost({ app, params, entity, texts, onNavigate, onSignedOut }: AppHostProps) {
  if (!app) {
    return <Banner tone="warning">{texts.appUnknown}</Banner>;
  }
  switch (app.TARGET.KIND) {
    case 'LIST':
      return <ListApp app={app} entity={entity} params={params} texts={texts} onNavigate={onNavigate} onSignedOut={onSignedOut} />;
    case 'PAGE':
      return <iframe className="trfrontend-page" title={app.TITLE} src={pageUrl(app, params)} />;
    case 'REMOTE':
      return <Remote app={app} params={params} texts={texts} />;
    case 'URL':
      return <Banner tone="information">{texts.opened}</Banner>;
  }
}
