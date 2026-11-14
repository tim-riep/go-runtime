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


// The generic list page: filter bar, table with paging, sorting and count,
// creating, deleting and bound actions by the capabilities of the entity
// set. With a merge set (Frontend.ListPage.MergeSet) it shows the entries
// of both sets in one table, sorted and paged in the browser, and marks
// those of the merge set, which cannot be changed.

import { Banner, Button, Form, FormField, PageHeader, Table, type TableColumn, type TableSort, Tag, Toolbar, type ToolbarItem } from '@tim-riep/trui';
import { useMessageDialog, useToast } from '@tim-riep/trui/core';
import { useEffect, useState } from 'react';
import { type Entity, keyPredicate, type ODataClient, type ServiceModel, type Value } from '../odata/index.ts';
import { defined } from '../shell/defined.ts';
import { format, type TextKey } from '../shell/texts.ts';
import { ActionDialog } from './ActionDialog.tsx';
import { displayValue, FieldEditor } from './Field.tsx';
import { type FilterValues, filterOf, matches } from './filter.ts';
import { actionLabel, type ListView } from './view.ts';

/** The most entries of each set a merged list reads. */
export const mergeLimit = 2000;

const shippedMark = '@shipped';

export interface ListPageProps {
  client: ODataClient;
  model: ServiceModel;
  view: ListView;
  texts: Record<TextKey, string>;
  params: Record<string, string>;
  onOpen: (path: string) => void;
  onCreate: () => void;
  onError: (e: unknown) => void;
}

function orderby(sort: TableSort | null): string | undefined {
  return sort ? `${sort.column}${sort.direction === 'descending' ? ' desc' : ''}` : undefined;
}

export function ListPage({ client, model, view, texts, params, onOpen, onCreate, onError }: ListPageProps) {
  const set = view.set;
  const [values, setValues] = useState<FilterValues>(() =>
    Object.fromEntries(view.filterFields.map((f) => [f.name, params[f.name] ?? null])),
  );
  const [filter, setFilter] = useState(() => filterOf(view.filterFields, values));
  const [sort, setSort] = useState<TableSort | null>(() => {
    const s = view.sort[0];
    return s ? { column: s.field, direction: s.descending ? 'descending' : 'ascending' } : null;
  });
  const [generation, setGeneration] = useState(0);
  const [total, setTotal] = useState<number | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [rowsByKey] = useState(() => new Map<string, Entity>());
  const [merged, setMerged] = useState<{ rows: Entity[]; truncated: boolean } | null>(null);
  const [action, setAction] = useState<string | null>(null);
  const toast = useToast();
  const dialog = useMessageDialog();

  const keyOf = (row: Entity) => `${row[shippedMark] === true ? 'shipped:' : ''}${keyPredicate(set.type, row)}`;
  const pathOf = (row: Entity) => `${row[shippedMark] === true && view.mergeSet ? view.mergeSet.name : set.name}${keyPredicate(set.type, row)}`;

  // A merged list reads both sets whenever the filter changes.
  const mergeKey = `${filter ?? ''}|${generation}`;
  useEffect(() => {
    if (!view.mergeSet) return;
    const mergeSet = view.mergeSet;
    setMerged(null);
    Promise.all([
      client.query(set.name, defined({ filter, top: mergeLimit, count: true })),
      client.query(mergeSet.name, defined({ top: mergeLimit, count: true })),
    ]).then(
      ([local, shipped]) => {
        const rows = [
          ...local.value,
          ...shipped.value.map((e) => ({ ...e, [shippedMark]: true }) as Entity).filter((e) => matches(view.filterFields, values, e)),
        ];
        setMerged({ rows, truncated: (local.count ?? 0) > mergeLimit || (shipped.count ?? 0) > mergeLimit });
        setTotal(rows.length);
      },
      (e: unknown) => onError(e),
    );
  }, [mergeKey]);

  const columns: TableColumn<Entity>[] = view.columns.map((c) => ({
    key: c.field.name,
    header: c.field.heading,
    renderCell: (row: Entity) => displayValue(c.field, row[c.field.name], texts),
    sortValue: (row: Entity) => row[c.field.name],
    align: ['Edm.Decimal', 'Edm.Int64', 'Edm.Int32', 'Edm.Int16', 'Edm.Double'].includes(c.field.property.type) ? ('end' as const) : ('start' as const),
    ...defined({ width: c.width ? c.width * 16 : undefined }),
    sortable: true,
  }));
  if (view.mergeSet) {
    columns.push({
      key: shippedMark,
      header: texts.shipped,
      renderCell: (row: Entity) => (row[shippedMark] === true ? <Tag>{texts.shipped}</Tag> : <Tag>{texts.local}</Tag>),
      sortValue: (row: Entity) => (row[shippedMark] === true ? 1 : 0),
      sortable: true,
    });
  }

  const selectedRows = [...selected].map((k) => rowsByKey.get(k)).filter((r): r is Entity => r !== undefined);
  const anyShipped = selectedRows.some((r) => r[shippedMark] === true);
  const items: ToolbarItem[] = [];
  if (view.insertable) items.push({ key: 'create', label: texts.create, variant: 'primary', overflow: 'never' });
  if (view.deletable) items.push({ key: 'delete', label: texts.delete, disabled: selectedRows.length === 0 || anyShipped, overflow: 'never' });
  for (const a of view.actions) {
    items.push({ key: `action:${a}`, label: actionLabel(model, a), disabled: selectedRows.length === 0 || anyShipped });
  }

  const reload = () => {
    setSelected(new Set());
    setGeneration((g) => g + 1);
  };

  async function remove() {
    const ok = await dialog.confirm({
      title: texts.delete,
      message: format(texts.confirmDelete, { count: selectedRows.length }),
      danger: true,
      confirmLabel: texts.delete,
      cancelLabel: texts.cancel,
    });
    if (!ok) return;
    try {
      for (const row of selectedRows) {
        await client.remove(set.name, row, (row['@odata.etag'] as string | undefined) ?? null);
      }
      toast.show({ message: texts.deleted, tone: 'success' });
    } catch (e) {
      onError(e);
    }
    reload();
  }

  async function run(name: string, args: Record<string, Value>) {
    setAction(null);
    try {
      for (const row of selectedRows) {
        const r = await client.action(name, args, { set: set.name, key: row, etag: (row['@odata.etag'] as string | undefined) ?? null });
        for (const m of r.messages) toast.show({ message: m.message, tone: m.severity === 'error' ? 'error' : m.severity === 'warning' ? 'warning' : 'information' });
      }
      toast.show({ message: texts.actionDone, tone: 'success' });
    } catch (e) {
      onError(e);
    }
    reload();
  }

  const onAction = (key: string) => {
    if (key === 'create') {
      onCreate();
    } else if (key === 'delete') {
      void remove();
    } else if (key.startsWith('action:')) {
      const name = key.slice('action:'.length);
      const a = model.actions.find((x) => x.name === name && x.boundTo === set.type.name);
      if (a && a.parameters.length > 0) setAction(name);
      else void run(name, {});
    }
  };

  const apply = () => {
    setFilter(filterOf(view.filterFields, values));
    setSelected(new Set());
  };
  const boundAction = action ? model.actions.find((x) => x.name === action && x.boundTo === set.type.name) : undefined;

  const table = view.mergeSet ? (
    <Table<Entity>
      key={mergeKey}
      aria-label={view.typeNamePlural}
      columns={columns}
      rows={merged?.rows ?? []}
      loading={merged === null}
      getRowKey={(row) => {
        const k = keyOf(row);
        rowsByKey.set(k, row);
        return k;
      }}
      sort={sort}
      onSortChange={setSort}
      selectionMode={view.deletable || view.actions.length ? 'multiple' : 'none'}
      selectedKeys={selected}
      onSelectionChange={setSelected}
      onRowPress={(row) => onOpen(pathOf(row))}
      renderEmpty={() => texts.noEntries}
    />
  ) : (
    <Table<Entity>
      key={`${filter ?? ''}|${orderby(sort) ?? ''}|${generation}`}
      aria-label={view.typeNamePlural}
      columns={columns}
      pageSize={50}
      loadPage={async ({ offset, limit, sort: s }) => {
        try {
          const page = await client.query(set.name, defined({ filter, orderby: orderby(s), top: limit, skip: offset, count: true }));
          setTotal(page.count);
          return { rows: page.value, total: page.count ?? offset + page.value.length };
        } catch (e) {
          onError(e);
          throw e;
        }
      }}
      getRowKey={(row) => {
        const k = keyOf(row);
        rowsByKey.set(k, row);
        return k;
      }}
      sort={sort}
      onSortChange={setSort}
      manualSort
      selectionMode={view.deletable || view.actions.length ? 'multiple' : 'none'}
      selectedKeys={selected}
      onSelectionChange={setSelected}
      onRowPress={(row) => onOpen(pathOf(row))}
      renderEmpty={() => texts.noEntries}
    />
  );

  return (
    <div className="trfrontend-list">
      <PageHeader title={view.typeNamePlural} headingLevel={1} {...defined({ subtitle: total === null ? undefined : String(total) })} />
      {view.filterFields.length > 0 ? (
        <Form
          columns={4}
          aria-label={texts.filters}
          onSubmit={(e) => {
            e.preventDefault();
            apply();
          }}
        >
          {view.filterFields.map((f) => (
            <FormField key={f.name} label={f.label} inputId={`filter-${f.name}`}>
              <FieldEditor
                field={{ ...f, mandatory: false, property: { ...f.property, nullable: true } }}
                id={`filter-${f.name}`}
                value={values[f.name] ?? null}
                onChange={(v) => setValues((old) => ({ ...old, [f.name]: v === null ? null : String(v) }))}
                readOnly={false}
                client={client}
                error={null}
              />
            </FormField>
          ))}
          <div className="trfrontend-filter-buttons">
            <Button variant="primary" type="submit">
              {texts.go}
            </Button>
            <Button
              variant="text"
              onClick={() => {
                const empty = Object.fromEntries(view.filterFields.map((f) => [f.name, null]));
                setValues(empty);
                setFilter(undefined);
              }}
            >
              {texts.clearFilter}
            </Button>
          </div>
        </Form>
      ) : null}
      {merged?.truncated ? <Banner tone="warning">{format(texts.truncated, { count: mergeLimit })}</Banner> : null}
      {items.length ? <Toolbar aria-label={texts.actions} items={items} onAction={onAction} /> : null}
      {table}
      {boundAction ? (
        <ActionDialog model={model} action={boundAction} client={client} texts={texts} onRun={(p) => void run(boundAction.name, p)} onCancel={() => setAction(null)} />
      ) : null}
    </div>
  );
}
