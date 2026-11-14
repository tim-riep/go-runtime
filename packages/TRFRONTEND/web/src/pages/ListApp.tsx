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


// An app of the kind LIST: the list page of its entity set and the detail
// pages of the entities, addressed by the entity in the route:
//
//   (none)                       the list
//   Companies('C1')              an entity of any set of the service
//   new/Companies                a new entity of a set
//   new/Companies('C1')/Contacts a new dependent entity along a navigation

import { Banner, Spinner } from '@tim-riep/trui';
import { useToast } from '@tim-riep/trui/core';
import { useEffect, useMemo, useState } from 'react';
import { ODataClient, ODataError, type ServiceModel } from '../odata/index.ts';
import { currentCsrfToken, currentLanguage, type HomeApp } from '../shell/api.ts';
import type { TextKey } from '../shell/texts.ts';
import { DetailPage } from './DetailPage.tsx';
import { ListPage } from './ListPage.tsx';
import { detailView, listView, mergedDetailView } from './view.ts';

export interface ListAppProps {
  app: HomeApp;
  entity: string | null;
  params: Record<string, string>;
  texts: Record<TextKey, string>;
  /** Shows another entity of the app, or the list for null. */
  onNavigate: (entity: string | null) => void;
  onSignedOut: () => void;
}

/** The entity set of a path such as Companies('C1') or a collection path for new entities. */
export function setOfPath(model: ServiceModel, path: string): string | null {
  const paren = path.indexOf('(');
  if (paren < 0) {
    return model.entitySets[path] ? path : null;
  }
  const parent = path.slice(0, paren);
  const rest = path.slice(path.lastIndexOf(')') + 1);
  if (!rest) {
    return model.entitySets[parent] ? parent : null;
  }
  const nav = rest.replace(/^\//, '');
  return model.entitySets[parent]?.bindings[nav] ?? null;
}

export function ListApp({ app, entity, params, texts, onNavigate, onSignedOut }: ListAppProps) {
  const client = useMemo(
    () => new ODataClient(app.TARGET.URL ?? '', { csrfToken: currentCsrfToken, language: currentLanguage }),
    [app.TARGET.URL],
  );
  const [model, setModel] = useState<ServiceModel | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const toast = useToast();

  const onError = (e: unknown) => {
    if (e instanceof ODataError && e.unauthorized) {
      onSignedOut();
      return;
    }
    toast.show({ message: e instanceof Error ? e.message : String(e), tone: 'error', duration: null });
  };

  useEffect(() => {
    client.metadata().then(setModel, (e: unknown) => {
      if (e instanceof ODataError && e.unauthorized) onSignedOut();
      else setFailed(e instanceof Error ? e.message : String(e));
    });
  }, [client]);

  if (failed !== null) return <Banner tone="error">{failed}</Banner>;
  if (!model) return <Spinner size="l" />;
  const listSet = app.TARGET.ENTITY_SET ?? '';
  if (!model.entitySets[listSet]) return <Banner tone="error">{texts.appUnknown}</Banner>;
  const list = listView(model, listSet);

  if (entity === null) {
    return (
      <ListPage
        key={listSet}
        client={client}
        model={model}
        view={list}
        texts={texts}
        params={params}
        onOpen={onNavigate}
        onCreate={() => onNavigate(`new/${listSet}`)}
        onError={onError}
      />
    );
  }

  const creating = entity.startsWith('new/');
  const path = creating ? entity.slice('new/'.length) : entity;
  const set = setOfPath(model, path);
  if (!set) return <Banner tone="warning">{texts.notFound}</Banner>;
  const shipped = list.mergeSet?.name === set;
  return (
    <DetailPage
      key={entity}
      client={client}
      model={model}
      view={shipped && list.mergeSet ? mergedDetailView(detailView(model, listSet), list.mergeSet) : detailView(model, set)}
      texts={texts}
      path={creating ? null : path}
      createPath={path}
      readOnly={shipped}
      onOpen={onNavigate}
      onCreate={(collection) => onNavigate(`new/${collection}`)}
      onCreated={(p) => onNavigate(p)}
      onDeleted={() => onNavigate(null)}
      onError={onError}
    />
  );
}
