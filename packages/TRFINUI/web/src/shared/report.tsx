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

// The frame of the report pages: a selection, Run and Export, the error of
// the last run and the result. Reports run with POST on /trfin/reports and
// export the same selection with ?format=xlsx.

import { Banner, Button, Form, FormGroup, PageHeader, type TableColumn } from '@tim-riep/trui';
import { today, useFormat } from '@tim-riep/trui/core';
import { exportIcon } from '@tim-riep/trui/icons';
import { type FormEvent, type ReactNode, useCallback, useEffect, useRef, useState } from 'react';
import { errorText, journalEntryService, type RemoteContext } from './context.ts';
import { fieldsStyle, figureNameStyle, figureStyle, figuresStyle, figureValueStyle, pageStyle } from './styles.ts';
import { format, type Texts } from './texts.ts';

/** Runs a report and exports it; the result is that of the last successful run. */
export function useReport<S, R>(context: RemoteContext, report: string, texts: Texts) {
  const [result, setResult] = useState<R | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [ran, setRan] = useState<S | null>(null);
  const run = useCallback(
    async (selection: S) => {
      setRunning(true);
      setError(null);
      try {
        setResult(await context.fetchJSON<R>(`/trfin/reports/${report}`, { body: selection }));
        setRan(selection);
      } catch (e) {
        setResult(null);
        setError(errorText(e));
      } finally {
        setRunning(false);
      }
    },
    [context, report],
  );
  const exportRows = useCallback(
    async (selection: S) => {
      setError(null);
      try {
        const file = await context.download(`/trfin/reports/${report}?format=xlsx`, { body: selection });
        context.notify(format(texts.exported, { file }), 'success');
      } catch (e) {
        setError(errorText(e));
      }
    },
    [context, report, texts],
  );
  return { result, error, running, ran, run, exportRows };
}

export interface ReportFrameProps {
  title: string;
  texts: Texts;
  /** The fields of the selection. */
  selection: ReactNode;
  canRun: boolean;
  running: boolean;
  error: string | null;
  onRun: () => void;
  /** Export of the selection of the shown result; none without result. */
  onExport: (() => void) | null;
  children?: ReactNode;
}

export function ReportFrame({ title, texts, selection, canRun, running, error, onRun, onExport, children }: ReportFrameProps) {
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (canRun) onRun();
  };
  return (
    <div style={pageStyle}>
      <PageHeader title={title} headingLevel={1} actions={onExport ? <Button icon={exportIcon} onClick={onExport}>{texts.export}</Button> : undefined} />
      <Form
        aria-label={texts.selection}
        onSubmit={submit}
        columns={3}
        actions={
          <Button type="submit" variant="primary" disabled={!canRun} loading={running}>
            {texts.run}
          </Button>
        }
      >
        <FormGroup title={texts.selection} headingLevel={2} span={3}>
          <div style={fieldsStyle}>{selection}</div>
        </FormGroup>
      </Form>
      {error !== null ? (
        <Banner tone="error" title={texts.failed}>
          {error}
        </Banner>
      ) : null}
      {children}
    </div>
  );
}

/** Runs once when the page opens with a complete selection from the parameters. */
export function useRunOnOpen(ready: boolean, run: () => void) {
  const done = useRef(false);
  useEffect(() => {
    if (ready && !done.current) {
      done.current = true;
      run();
    }
  }, [ready, run]);
}

/** A column of amounts, right-aligned and formatted for the locale. */
export function useAmountColumn() {
  const fmt = useFormat();
  return useCallback(
    <T,>(key: string, header: string, value?: (row: T) => string | null): TableColumn<T> => ({
      key,
      header,
      align: 'end',
      width: 150,
      renderCell: (row: T) => {
        const v = value ? value(row) : ((row as Record<string, unknown>)[key] as string | null);
        return v === null || v === undefined ? '' : fmt.decimal(v, { scale: 2 });
      },
    }),
    [fmt],
  );
}

/** The totals of the balance reports. */
export interface Totals {
  CARRY_FORWARD: string;
  PRIOR_BALANCE: string;
  DEBIT: string;
  CREDIT: string;
  PERIOD_BALANCE: string;
  CUMULATIVE_BALANCE: string;
}

/** Figures with a label each, such as totals below a table. */
export function Figures({ label, figures, currency }: { label: string; figures: Array<[string, string]>; currency: string }) {
  const fmt = useFormat();
  return (
    <dl style={figuresStyle} aria-label={label}>
      {figures.map(([name, value]) => (
        <div key={name} style={figureStyle}>
          <dt style={figureNameStyle}>{name}</dt>
          <dd style={figureValueStyle}>{fmt.decimal(value, { scale: 2, currency })}</dd>
        </div>
      ))}
    </dl>
  );
}

export function TotalsLine({ totals, currency, texts }: { totals: Totals; currency: string; texts: Texts }) {
  return (
    <Figures
      label={texts.totals}
      currency={currency}
      figures={[
        [texts.carryForward, totals.CARRY_FORWARD],
        [texts.priorBalance, totals.PRIOR_BALANCE],
        [texts.debit, totals.DEBIT],
        [texts.credit, totals.CREDIT],
        [texts.periodBalance, totals.PERIOD_BALANCE],
        [texts.cumulativeBalance, totals.CUMULATIVE_BALANCE],
      ]}
    />
  );
}

/** The parameters of an app as text, null when missing. */
export function param(params: Record<string, string>, name: string): string | null {
  return name in params ? (params[name] ?? null) : null;
}

/** The parameters of an app as number, null when missing or no number. */
export function numberParam(params: Record<string, string>, name: string): number | null {
  const v = param(params, name);
  if (v === null || v.trim() === '') return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}

/** Parameters for another app: the values that are not null, as text. */
export function paramsOf(values: Record<string, string | number | null | undefined>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(values)) {
    if (v !== null && v !== undefined) out[k] = String(v);
  }
  return out;
}

/** Today in the time zone of the browser. */
export function todayHere(): string {
  return today(Intl.DateTimeFormat().resolvedOptions().timeZone);
}

/** Opens the journal entry in the list app of journal entries, at its object page. */
export async function openEntry(context: RemoteContext, company: string, year: number, no: string): Promise<void> {
  try {
    const path = await context.odata(journalEntryService).entityPath('JournalEntries', { COMPANY: company, FISCAL_YEAR: year, DOCUMENT_NO: no });
    context.navigate('/TRFINUI/JOURNAL_ENTRIES', {}, path);
  } catch (e) {
    context.notify(errorText(e), 'error');
  }
}
