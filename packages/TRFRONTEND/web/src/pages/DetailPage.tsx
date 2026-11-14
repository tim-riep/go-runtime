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


// The generic detail page: header, sections with field groups and tables
// of dependent entities, display and edit, PATCH with the ETag (412 → the
// conflict with Reload), creating with POST, deleting, bound actions and
// messages of the server at their fields.

import { Banner, Button, Form, FormField, FormGroup, PageHeader, Spinner, Table, Toolbar, type ToolbarItem } from '@tim-riep/trui';
import { useMessageDialog, useToast } from '@tim-riep/trui/core';
import { useCallback, useEffect, useState } from 'react';
import { changes, type Entity, keyPredicate, type ODataClient, ODataError, type ServiceModel, type Value } from '../odata/index.ts';
import { defined } from '../shell/defined.ts';
import type { TextKey } from '../shell/texts.ts';
import { ActionDialog } from './ActionDialog.tsx';
import { displayValue, FieldEditor } from './Field.tsx';
import { actionLabel, type DetailView, type Field, listView } from './view.ts';

export interface DetailPageProps {
  client: ODataClient;
  model: ServiceModel;
  view: DetailView;
  texts: Record<TextKey, string>;
  /** The path of the entity, e.g. Companies('C1'); null to create one. */
  path: string | null;
  /** Where a new entity is posted: the entity set or a navigation of its parent. */
  createPath: string;
  /** Shipped entries and sets without changes are only shown. */
  readOnly: boolean;
  onOpen: (path: string) => void;
  onCreate: (collectionPath: string) => void;
  onCreated: (path: string) => void;
  onDeleted: () => void;
  onError: (e: unknown) => void;
}

/** The fields that a client writes: neither computed nor ETag nor, on update, immutable or key. */
function writable(view: DetailView, f: Field, creating: boolean): boolean {
  if (f.computed || view.concurrency.includes(f.name)) return false;
  return creating || !(f.immutable || f.key);
}

export function DetailPage(props: DetailPageProps) {
  const { client, model, view, texts, path, createPath, readOnly, onOpen, onCreate, onCreated, onDeleted, onError } = props;
  const creating = path === null;
  const tables = view.sections.map((s) => s.table).filter((t): t is string => t !== null);
  const [entity, setEntity] = useState<Entity | null>(creating ? {} : null);
  const [etag, setEtag] = useState<string | null>(null);
  const [edited, setEdited] = useState<Entity | null>(creating ? {} : null);
  const [failure, setFailure] = useState<ODataError | null>(null);
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [action, setAction] = useState<string | null>(null);
  const toast = useToast();
  const dialog = useMessageDialog();

  const load = useCallback(() => {
    if (path === null) return;
    setFailure(null);
    client.readPath(path, tables).then(
      (r) => {
        setEntity(r.entity);
        setEtag(r.etag);
        setEdited(null);
      },
      (e: unknown) => {
        if (e instanceof ODataError && e.status === 404) setMissing(true);
        else onError(e);
      },
    );
    // tables follow from the view, which does not change for a path.
  }, [client, path]);

  useEffect(load, [load]);

  if (missing) return <Banner tone="warning">{texts.notFound}</Banner>;
  if (!entity) return <Spinner size="l" />;

  const current = edited ?? entity;
  const editing = edited !== null;
  const titleValue = view.titleField ? current[view.titleField] : null;
  const title = creating ? texts.newEntry : titleValue !== null && titleValue !== undefined ? String(titleValue) : view.typeName;
  const subtitle = view.subtitleField && !creating ? current[view.subtitleField] : null;

  async function save() {
    if (!edited || !entity) return;
    setBusy(true);
    setFailure(null);
    const fields = view.sections.flatMap((s) => s.groups.flatMap((g) => g.fields));
    const allowed = new Set(fields.filter((f) => writable(view, f, creating)).map((f) => f.name));
    try {
      if (creating) {
        const body: Entity = {};
        for (const [k, v] of Object.entries(edited)) if (allowed.has(k)) body[k] = v;
        const r = await client.create(createPath, body);
        toast.show({ message: texts.created, tone: 'success' });
        if (r.entity) onCreated(`${view.set.name}${keyPredicate(view.set.type, r.entity)}`);
      } else {
        const patch: Entity = {};
        for (const [k, v] of Object.entries(changes(entity, edited))) if (allowed.has(k)) patch[k] = v;
        const r = await client.update(view.set.name, entity, patch, etag);
        for (const m of r.messages) toast.show({ message: m.message, tone: m.severity === 'error' ? 'error' : m.severity === 'warning' ? 'warning' : 'information' });
        toast.show({ message: texts.saved, tone: 'success' });
        load();
      }
    } catch (e) {
      if (e instanceof ODataError && !e.unauthorized) setFailure(e);
      else onError(e);
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!entity) return;
    const ok = await dialog.confirm({ title: texts.delete, message: texts.confirmDeleteOne, danger: true, confirmLabel: texts.delete, cancelLabel: texts.cancel });
    if (!ok) return;
    try {
      await client.remove(view.set.name, entity, etag);
      toast.show({ message: texts.deleted, tone: 'success' });
      onDeleted();
    } catch (e) {
      if (e instanceof ODataError && !e.unauthorized) setFailure(e);
      else onError(e);
    }
  }

  async function run(name: string, args: Record<string, Value>) {
    setAction(null);
    if (!entity) return;
    try {
      const r = await client.action(name, args, { set: view.set.name, key: entity, etag });
      for (const m of r.messages) toast.show({ message: m.message, tone: m.severity === 'error' ? 'error' : m.severity === 'warning' ? 'warning' : 'information' });
      toast.show({ message: texts.actionDone, tone: 'success' });
      load();
    } catch (e) {
      if (e instanceof ODataError && !e.unauthorized) setFailure(e);
      else onError(e);
    }
  }

  const items: ToolbarItem[] = [];
  if (editing) {
    items.push({ key: 'save', label: texts.save, variant: 'primary', disabled: busy, overflow: 'never' }, { key: 'cancel', label: texts.cancel, overflow: 'never' });
  } else if (!readOnly) {
    if (view.updatable) items.push({ key: 'edit', label: texts.edit, variant: 'primary', overflow: 'never' });
    if (view.deletable) items.push({ key: 'delete', label: texts.delete });
    for (const a of view.actions) items.push({ key: `action:${a}`, label: actionLabel(model, a) });
  }
  const onAction = (key: string) => {
    if (key === 'edit') setEdited({ ...entity });
    else if (key === 'cancel') {
      if (creating) onDeleted();
      else {
        setEdited(null);
        setFailure(null);
      }
    } else if (key === 'save') void save();
    else if (key === 'delete') void remove();
    else if (key.startsWith('action:')) {
      const name = key.slice('action:'.length);
      const a = model.actions.find((x) => x.name === name && x.boundTo === view.set.type.name);
      if (a && a.parameters.length > 0) setAction(name);
      else void run(name, {});
    }
  };

  const errorOf = (f: Field): string | null => {
    const ms = failure?.messagesFor(f.name) ?? [];
    return ms.length ? ms.map((m) => m.message).join(' ') : null;
  };
  const boundAction = action ? model.actions.find((x) => x.name === action && x.boundTo === view.set.type.name) : undefined;

  return (
    <div className="trfrontend-detail">
      <PageHeader
        title={title}
        headingLevel={1}
        {...defined({ subtitle: subtitle === null || subtitle === undefined ? undefined : String(subtitle) })}
        actions={items.length ? <Toolbar aria-label={texts.actions} items={items} onAction={onAction} /> : undefined}
      />
      {readOnly && !editing ? <Banner tone="information">{texts.readOnlyShipped}</Banner> : null}
      {failure?.conflict ? (
        <Banner
          tone="error"
          announce="assertive"
          actions={
            <Button variant="secondary" onClick={load}>
              {texts.reload}
            </Button>
          }
        >
          {texts.conflict}
        </Banner>
      ) : failure ? (
        <Banner tone="error" announce="assertive">
          {failure.message || texts.failed}
        </Banner>
      ) : null}
      {view.sections.map((s) => {
        if (s.table) {
          if (creating) return null;
          return <DependentTable key={s.id} {...props} title={s.title} navigation={s.table} rows={(entity[s.table] as Entity[] | undefined) ?? []} onOpen={onOpen} onCreate={onCreate} />;
        }
        const groups = s.groups.filter((g) => g.fields.some((f) => !creating || writable(view, f, true) || f.key));
        if (!groups.length) return null;
        return (
          <section key={s.id} aria-label={s.title} className="trfrontend-section">
            <h2>{s.title}</h2>
            <Form
              columns={2}
              onSubmit={(e) => {
                e.preventDefault();
                void save();
              }}
            >
              {groups.map((g) => (
                <FormGroup key={g.id} {...defined({ title: g.title ?? undefined })} headingLevel={3}>
                  {g.fields
                    .filter((f) => !creating || writable(view, f, true))
                    .map((f) => {
                      const id = `${s.id}-${g.id}-${f.name}`;
                      return (
                        <FormField key={f.name} label={f.label} required={editing && f.mandatory && writable(view, f, creating)} inputId={id}>
                          {editing && writable(view, f, creating) ? (
                            <FieldEditor
                              field={f}
                              id={id}
                              value={current[f.name]}
                              onChange={(v) => setEdited((old) => ({ ...(old ?? {}), [f.name]: v }))}
                              readOnly={false}
                              client={client}
                              error={errorOf(f)}
                            />
                          ) : (
                            <output id={id} className="trfrontend-value">
                              {displayValue(f, current[f.name], texts)}
                            </output>
                          )}
                        </FormField>
                      );
                    })}
                </FormGroup>
              ))}
            </Form>
          </section>
        );
      })}
      {boundAction ? (
        <ActionDialog model={model} action={boundAction} client={client} texts={texts} onRun={(p) => void run(boundAction.name, p)} onCancel={() => setAction(null)} />
      ) : null}
    </div>
  );
}

/** A table of the dependent entities of a navigation, opened by their own entity set. */
function DependentTable(props: DetailPageProps & { title: string; navigation: string; rows: Entity[] }) {
  const { model, view, texts, path, readOnly, title, navigation, rows, onOpen, onCreate } = props;
  const target = view.set.bindings[navigation];
  const nav = view.set.type.navigations.find((n) => n.name === navigation);
  const lv = target && model.entitySets[target] ? listView(model, target) : null;
  if (!lv || !nav) return null;
  const set = lv.set;
  return (
    <section aria-label={title} className="trfrontend-section">
      <h2>{title}</h2>
      {!readOnly && lv.insertable && path !== null ? (
        <Toolbar aria-label={title} items={[{ key: 'create', label: texts.create }]} onAction={() => onCreate(`${path}/${navigation}`)} />
      ) : null}
      <Table<Entity>
        aria-label={title}
        columns={lv.columns.map((c) => ({
          key: c.field.name,
          header: c.field.heading,
          renderCell: (row: Entity) => displayValue(c.field, row[c.field.name], texts),
          sortValue: (row: Entity) => row[c.field.name],
          sortable: true,
        }))}
        rows={rows}
        getRowKey={(row) => keyPredicate(set.type, row)}
        onRowPress={(row) => onOpen(`${set.name}${keyPredicate(set.type, row)}`)}
        renderEmpty={() => texts.noEntries}
      />
    </section>
  );
}
