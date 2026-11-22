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
//   Companies('C1')/TRDEMOOTTO_COMPANY_CHANNELS(…)
//                                an entity of a contained navigation, which
//                                has no entity set of its own and lives below
//                                its parent; new/… along it creates one

import { Banner, Spinner } from '@tim-riep/trui';
import { useToast } from '@tim-riep/trui/core';
import { useEffect, useMemo, useState } from 'react';
import { type EntitySet, ODataClient, ODataError, resolvePath, type ServiceModel } from '../odata/index.ts';
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
export function setOfPath(model: ServiceModel, path: string): EntitySet | null {
  return resolvePath(model, path)?.set ?? null;
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
  const resolved = resolvePath(model, path);
  if (!resolved) return <Banner tone="warning">{texts.notFound}</Banner>;
  const { set } = resolved;
  const contained = set.containedIn !== undefined;
  const shipped = !contained && list.mergeSet?.name === set.name;
  // A contained entity only exists below its parent: back there when it is
  // deleted or not created.
  const back = contained ? resolved.parent : null;
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
      onDeleted={() => onNavigate(back)}
      onError={onError}
    />
  );
}
