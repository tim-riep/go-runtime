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

// The intervals of the number range objects of the general ledger through
// the node /trfin/number-ranges: adding, changing and deleting intervals,
// with a confirmation where the node asks for one (409), and the history.

import { Banner, Button, Checkbox, Form, FormField, FormGroup, PageHeader, Select, Table, TextField, Title } from '@tim-riep/trui';
import { useMessageDialog } from '@tim-riep/trui/core';
import { addIcon } from '@tim-riep/trui/icons';
import { useCallback, useEffect, useState } from 'react';
import { errorText, type RemoteContext, type RemoteProps } from '../shared/context.ts';
import { IntegerField } from '../shared/fields.tsx';
import { param } from '../shared/report.tsx';
import { buttonRowStyle, pageStyle, sectionStyle } from '../shared/styles.ts';
import { format, textsOf } from '../shared/texts.ts';

export interface NumberRangeObject {
  NAME: string;
  PACKAGE_ID: string;
  LABEL: string | null;
  LENGTH: number;
  SUBOBJECT_NAME: string | null;
  PER_FISCAL_YEAR: boolean;
}

export interface NumberInterval {
  RANGE_NAME: string;
  INTERVAL_ID: string;
  SUBOBJECT: string | null;
  FISCAL_YEAR: number | null;
  FROM_NO: string;
  TO_NO: string;
  CURRENT_NO: string | null;
  EXTERNAL: boolean;
  USED_PERCENT?: number | null;
  CHANGED_AT?: string | null;
  CHANGED_BY?: string | null;
}

export interface NumberRangeChange {
  CHANGED_AT: string;
  CHANGED_BY: string;
  KIND: string;
  INTERVAL_ID: string;
  SUBOBJECT: string | null;
  FISCAL_YEAR: number | null;
}

const node = '/trfin/number-ranges';

/** The key of an interval in its object. */
export function intervalKey(i: Pick<NumberInterval, 'INTERVAL_ID' | 'SUBOBJECT' | 'FISCAL_YEAR'>): string {
  return `${i.SUBOBJECT ?? ''}/${i.FISCAL_YEAR ?? ''}/${i.INTERVAL_ID}`;
}

/**
 * Sends a change of an interval; when the node answers 409, asks the user
 * and sends it again with confirm. The intervals after the change, or null
 * when the user declined.
 */
export async function changeInterval(
  context: RemoteContext,
  method: 'POST' | 'PUT' | 'DELETE',
  interval: NumberInterval,
  ask: (message: string) => Promise<boolean>,
): Promise<NumberInterval[] | null> {
  try {
    return await context.fetchJSON<NumberInterval[]>(`${node}/intervals`, { method, body: interval });
  } catch (e) {
    if (e && typeof e === 'object' && 'status' in e && (e as { status: number }).status === 409) {
      if (!(await ask(errorText(e)))) return null;
      return context.fetchJSON<NumberInterval[]>(`${node}/intervals?confirm=true`, { method, body: interval });
    }
    throw e;
  }
}

function emptyInterval(object: NumberRangeObject): NumberInterval {
  return {
    RANGE_NAME: object.NAME,
    INTERVAL_ID: '',
    SUBOBJECT: null,
    FISCAL_YEAR: object.PER_FISCAL_YEAR ? new Date().getFullYear() : null,
    FROM_NO: '',
    TO_NO: '',
    CURRENT_NO: null,
    EXTERNAL: false,
  };
}

export default function NumberRangePage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const dialog = useMessageDialog();
  const [objects, setObjects] = useState<NumberRangeObject[] | null>(null);
  const [object, setObject] = useState<string | null>(param(params, 'OBJECT'));
  const [intervals, setIntervals] = useState<NumberInterval[]>([]);
  const [history, setHistory] = useState<NumberRangeChange[]>([]);
  const [editing, setEditing] = useState<{ interval: NumberInterval; adding: boolean } | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    context.fetchJSON<NumberRangeObject[]>(node).then(
      (list) => {
        setObjects(list);
        setObject((o) => o ?? list[0]?.NAME ?? null);
      },
      (e: unknown) => setFailure(errorText(e)),
    );
  }, [context]);

  const reload = useCallback(
    async (name: string) => {
      const q = `?object=${encodeURIComponent(name)}`;
      const [iv, h] = await Promise.all([context.fetchJSON<NumberInterval[]>(`${node}/intervals${q}`), context.fetchJSON<NumberRangeChange[]>(`${node}/history${q}`)]);
      setIntervals(iv);
      setHistory(h);
    },
    [context],
  );
  useEffect(() => {
    if (object === null) return;
    setEditing(null);
    reload(object).catch((e: unknown) => setFailure(errorText(e)));
  }, [object, reload]);

  const info = objects?.find((o) => o.NAME === object) ?? null;
  const ask = (message: string) =>
    dialog.confirm({ title: texts.confirmTitle, message, tone: 'warning', confirmLabel: texts.confirm, cancelLabel: texts.cancel });

  const send = async (method: 'POST' | 'PUT' | 'DELETE', interval: NumberInterval, done: string) => {
    setFailure(null);
    setBusy(true);
    try {
      const after = await changeInterval(context, method, interval, ask);
      if (after) {
        setIntervals(after);
        setEditing(null);
        context.notify(done, 'success');
        if (object !== null) await reload(object);
      }
    } catch (e) {
      setFailure(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (iv: NumberInterval) => {
    const ok = await dialog.confirm({ title: format(texts.deleteInterval, { id: iv.INTERVAL_ID }), message: intervalKey(iv), danger: true, confirmLabel: texts.delete, cancelLabel: texts.cancel });
    if (ok) await send('DELETE', iv, texts.deleted);
  };

  if (objects !== null && objects.length === 0) {
    return (
      <div style={pageStyle}>
        <PageHeader title={texts.numberRanges} headingLevel={1} />
        <Banner tone="information">{texts.noObjects}</Banner>
      </div>
    );
  }

  const e = editing?.interval;
  const setField = (change: Partial<NumberInterval>) => editing && setEditing({ ...editing, interval: { ...editing.interval, ...change } });
  return (
    <div style={pageStyle}>
      <PageHeader
        title={texts.numberRanges}
        headingLevel={1}
        actions={
          info ? (
            <Button icon={addIcon} onClick={() => setEditing({ interval: emptyInterval(info), adding: true })}>
              {texts.addInterval}
            </Button>
          ) : undefined
        }
      />
      <Form aria-label={texts.object} columns={2} onSubmit={(ev) => ev.preventDefault()}>
        <FormGroup span={2}>
          <FormField label={texts.object} inputId="number-ranges-object">
            <Select
              id="number-ranges-object"
              items={(objects ?? []).map((o) => ({ value: o.NAME, label: o.NAME, description: o.LABEL ?? '' }))}
              value={object ?? ''}
              onChange={(v) => setObject(v)}
            />
          </FormField>
        </FormGroup>
      </Form>
      {failure !== null ? (
        <Banner tone="error" title={texts.failed}>
          {failure}
        </Banner>
      ) : null}
      <Table<NumberInterval>
        aria-label={texts.interval}
        columns={[
          { key: 'INTERVAL_ID', header: texts.interval, width: 90 },
          ...(info?.SUBOBJECT_NAME ? [{ key: 'SUBOBJECT', header: info.SUBOBJECT_NAME === 'COMPANY' ? texts.company : texts.subobject, width: 110 }] : []),
          ...(info?.PER_FISCAL_YEAR ? [{ key: 'FISCAL_YEAR', header: texts.fiscalYear, width: 100, align: 'end' as const }] : []),
          { key: 'FROM_NO', header: texts.fromNo, width: 140 },
          { key: 'TO_NO', header: texts.toNo, width: 140 },
          { key: 'CURRENT_NO', header: texts.currentNo, width: 140 },
          { key: 'EXTERNAL', header: texts.external, width: 90, renderCell: (r) => (r.EXTERNAL ? texts.yes : texts.no) },
          { key: 'USED_PERCENT', header: texts.used, width: 90, align: 'end', renderCell: (r) => (r.USED_PERCENT === null || r.USED_PERCENT === undefined ? '' : `${r.USED_PERCENT} %`) },
          { key: 'CHANGED_BY', header: texts.changedBy, width: 120 },
        ]}
        rows={intervals}
        getRowKey={intervalKey}
        rowActions={() => [
          { key: 'edit', label: texts.edit },
          { key: 'delete', label: texts.delete },
        ]}
        onRowAction={(key, row) => {
          if (key === 'edit') setEditing({ interval: { ...row }, adding: false });
          else void remove(row);
        }}
        renderEmpty={() => texts.noRows}
      />
      {e && info ? (
        <Form
          aria-label={editing.adding ? texts.addInterval : format(texts.editInterval, { id: e.INTERVAL_ID })}
          columns={3}
          onSubmit={(ev) => {
            ev.preventDefault();
            void send(editing.adding ? 'POST' : 'PUT', e, texts.saved);
          }}
          actions={
            <div style={buttonRowStyle}>
              <Button type="submit" variant="primary" loading={busy}>
                {texts.save}
              </Button>
              <Button onClick={() => setEditing(null)}>{texts.cancel}</Button>
            </div>
          }
        >
          <FormGroup title={editing.adding ? texts.addInterval : format(texts.editInterval, { id: e.INTERVAL_ID })} headingLevel={2} span={3}>
            <FormField label={texts.interval} required inputId="number-ranges-interval">
              <TextField id="number-ranges-interval" required maxLength={2} readOnly={!editing.adding} value={e.INTERVAL_ID} onChange={(v) => setField({ INTERVAL_ID: v })} />
            </FormField>
            {info.SUBOBJECT_NAME ? (
              <FormField label={info.SUBOBJECT_NAME === 'COMPANY' ? texts.company : texts.subobject} required inputId="number-ranges-subobject">
                <TextField id="number-ranges-subobject" nullable required readOnly={!editing.adding} value={e.SUBOBJECT} onChange={(v) => setField({ SUBOBJECT: v })} />
              </FormField>
            ) : null}
            {info.PER_FISCAL_YEAR ? (
              editing.adding ? (
                <IntegerField id="number-ranges-year" label={texts.fiscalYear} required value={e.FISCAL_YEAR} onChange={(v) => setField({ FISCAL_YEAR: v })} min={1900} max={9999} />
              ) : (
                <FormField label={texts.fiscalYear} inputId="number-ranges-year">
                  <TextField id="number-ranges-year" readOnly value={String(e.FISCAL_YEAR ?? '')} />
                </FormField>
              )
            ) : null}
            <FormField label={texts.fromNo} required inputId="number-ranges-from">
              <TextField id="number-ranges-from" required maxLength={info.LENGTH} value={e.FROM_NO} onChange={(v) => setField({ FROM_NO: v })} />
            </FormField>
            <FormField label={texts.toNo} required inputId="number-ranges-to">
              <TextField id="number-ranges-to" required maxLength={info.LENGTH} value={e.TO_NO} onChange={(v) => setField({ TO_NO: v })} />
            </FormField>
            <FormField label={texts.currentNo} inputId="number-ranges-current">
              <TextField id="number-ranges-current" nullable maxLength={info.LENGTH} value={e.CURRENT_NO} onChange={(v) => setField({ CURRENT_NO: v })} />
            </FormField>
            <FormField label={texts.external} inputId="number-ranges-external">
              <Checkbox id="number-ranges-external" checked={e.EXTERNAL} onChange={(c) => setField({ EXTERNAL: c })} />
            </FormField>
          </FormGroup>
        </Form>
      ) : null}
      {history.length ? (
        <section style={sectionStyle} aria-label={texts.history}>
          <Title level={2} size="m">
            {texts.history}
          </Title>
          <Table<NumberRangeChange>
            aria-label={texts.history}
            columns={[
              { key: 'CHANGED_AT', header: texts.changedAt, width: 220 },
              { key: 'CHANGED_BY', header: texts.changedBy, width: 120 },
              { key: 'KIND', header: texts.change, width: 100 },
              { key: 'INTERVAL_ID', header: texts.interval, width: 90 },
              { key: 'SUBOBJECT', header: texts.subobject, width: 110 },
              { key: 'FISCAL_YEAR', header: texts.fiscalYear, width: 100 },
            ]}
            rows={history}
            getRowKey={(h) => `${h.CHANGED_AT} ${h.INTERVAL_ID} ${h.SUBOBJECT ?? ''} ${h.FISCAL_YEAR ?? ''}`}
          />
        </section>
      ) : null}
    </div>
  );
}
