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

// The balance sheet of a company at a key date
// (/trfin/reports/balance-sheet) as indented table: nodes expand and
// collapse, accounts open their line items of the year of the key date.

import { Banner, Button, FormField, Select, Table, type TableColumn, Tag, Title } from '@tim-riep/trui';
import { chevronDownIcon, chevronRightIcon, collapseIcon, expandIcon } from '@tim-riep/trui/icons';
import { useCallback, useMemo, useState } from 'react';
import { errorText, type RemoteProps } from '../shared/context.ts';
import { ChartKindField, CompanyField, DateField, LedgerField } from '../shared/fields.tsx';
import { useChoices } from '../shared/masterdata.ts';
import { Figures, param, paramsOf, ReportFrame, todayHere, useAmountColumn, useReport, useRunOnOpen } from '../shared/report.tsx';
import { buttonRowStyle, sectionStyle } from '../shared/styles.ts';
import { format, textsOf } from '../shared/texts.ts';
import { hasChildren, parentKeys, rowKey, type StatementRow, visibleRows } from '../shared/tree.ts';

export interface StatementSelection {
  LEDGER: string | null;
  COMPANY: string;
  KEY_DATE: string;
  CHART_KIND: string | null;
  STRUCTURE: string | null;
}

export interface StatementReport {
  LEDGER: string;
  COMPANY: string;
  CURRENCY: string;
  KEY_DATE: string;
  CHART: string;
  STRUCTURE: string;
  ROWS: StatementRow[];
  TOTAL_ASSETS: string;
  TOTAL_LIABILITIES: string;
  RESULT: string;
  DIFFERENCE: string;
  BALANCED: boolean;
  UNASSIGNED: Array<{ CHART: string; ACCOUNT: string; SHORT_TEXT: string | null; BALANCE: string }>;
  PROBLEMS: Array<{ TEXT: string }>;
}

export default function BalanceSheetPage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const onError = useCallback((e: unknown) => context.notify(errorText(e), 'error'), [context]);
  const choices = useChoices(context, onError);
  const [company, setCompany] = useState<string | null>(param(params, 'COMPANY'));
  const [ledger, setLedger] = useState<string | null>(param(params, 'LEDGER'));
  const [keyDate, setKeyDate] = useState<string | null>(param(params, 'KEY_DATE') ?? todayHere());
  const [chartKind, setChartKind] = useState<string | null>(param(params, 'CHART_KIND'));
  const [structure, setStructure] = useState<string | null>(param(params, 'STRUCTURE'));
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const report = useReport<StatementSelection, StatementReport>(context, 'balance-sheet', texts);
  const amount = useAmountColumn();

  const selection = (): StatementSelection | null =>
    company !== null && keyDate !== null ? { LEDGER: ledger, COMPANY: company, KEY_DATE: keyDate, CHART_KIND: chartKind, STRUCTURE: structure } : null;
  const run = useCallback(() => {
    const s = selection();
    if (s) {
      setCollapsed(new Set());
      void report.run(s);
    }
  }, [company, ledger, keyDate, chartKind, structure, report.run]);
  useRunOnOpen(company !== null && keyDate !== null && 'COMPANY' in params, run);

  const r = report.result;
  const s = report.ran;
  const rows = useMemo(() => r?.ROWS ?? [], [r]);
  const parents = useMemo(() => parentKeys(rows), [rows]);
  const shown = useMemo(() => visibleRows(rows, collapsed), [rows, collapsed]);
  const toggle = (key: string) => {
    const next = new Set(collapsed);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setCollapsed(next);
  };
  const press = (row: StatementRow) => {
    if (row.ACCOUNT === null) {
      if (parents.has(rowKey(row))) toggle(rowKey(row));
      return;
    }
    if (!s || !r) return;
    context.navigate(
      '/TRFINUI/LINE_ITEMS',
      paramsOf({ COMPANY: s.COMPANY, LEDGER: s.LEDGER, FISCAL_YEAR: Number(r.KEY_DATE.slice(0, 4)), CHART_KIND: s.CHART_KIND, ACCOUNT: row.ACCOUNT }),
    );
  };
  const companyInfo = choices?.companies.find((c) => c.COMPANY === company) ?? null;
  const chart = companyInfo ? (chartKind === 'COUNTRY' ? companyInfo.COUNTRY_CHART : companyInfo.CHART) : null;
  const structures = (choices?.structures ?? []).filter((x) => chart === null || x.CHART === chart).map((x) => ({ value: x.STRUCTURE, label: x.STRUCTURE, description: x.LABEL }));

  const columns: TableColumn<StatementRow>[] = [
    {
      key: 'LABEL',
      header: texts.label,
      width: 420,
      sortable: false,
      renderCell: (row) => {
        const i = rows.indexOf(row);
        const key = rowKey(row);
        const label = row.ACCOUNT === null ? (row.LABEL ?? row.NODE) : `${row.ACCOUNT} ${row.LABEL ?? ''}`.trim();
        const open = !collapsed.has(key);
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', paddingInlineStart: `${(row.LEVEL - 1) * 1.25}rem`, fontWeight: row.ACCOUNT === null ? 600 : 400 }}>
            {hasChildren(rows, i) ? (
              <Button
                variant="text"
                icon={open ? chevronDownIcon : chevronRightIcon}
                aria-label={format(open ? texts.collapse : texts.expand, { node: label })}
                aria-expanded={open}
                onClick={(e) => {
                  e.stopPropagation();
                  toggle(key);
                }}
              />
            ) : (
              <span style={{ display: 'inline-block', width: '2rem' }} />
            )}
            {label}
          </span>
        );
      },
    },
    { ...amount<StatementRow>('AMOUNT', texts.amount), sortable: false },
  ];

  return (
    <ReportFrame
      title={texts.balanceSheet}
      texts={texts}
      canRun={selection() !== null}
      running={report.running}
      error={report.error}
      onRun={run}
      onExport={s ? () => void report.exportRows(s) : null}
      selection={
        <>
          <CompanyField id="balance-sheet-company" companies={choices?.companies ?? []} value={company} onChange={setCompany} texts={texts} />
          <LedgerField id="balance-sheet-ledger" ledgers={choices?.ledgers ?? []} value={ledger} onChange={setLedger} texts={texts} />
          <DateField id="balance-sheet-key-date" label={texts.keyDate} required value={keyDate} onChange={setKeyDate} />
          <ChartKindField id="balance-sheet-chart-kind" value={chartKind} onChange={setChartKind} texts={texts} />
          <FormField label={texts.structure} inputId="balance-sheet-structure">
            <Select id="balance-sheet-structure" items={structures} nullable placeholder={texts.firstStructure} value={structure} onChange={(v) => setStructure(v)} />
          </FormField>
        </>
      }
    >
      {r ? (
        <section style={sectionStyle} aria-label={texts.result}>
          <div style={buttonRowStyle}>
            <Tag tone={r.BALANCED ? 'success' : 'error'}>{r.BALANCED ? texts.balanced : texts.notBalanced}</Tag>
            <Figures
              label={texts.totals}
              currency={r.CURRENCY}
              figures={[
                [texts.totalAssets, r.TOTAL_ASSETS],
                [texts.totalLiabilities, r.TOTAL_LIABILITIES],
                [texts.resultOfYear, r.RESULT],
                [texts.difference, r.DIFFERENCE],
              ]}
            />
          </div>
          {r.PROBLEMS.length ? (
            <Banner tone="warning" title={texts.problems}>
              <ul>
                {r.PROBLEMS.map((p) => (
                  <li key={p.TEXT}>{p.TEXT}</li>
                ))}
              </ul>
            </Banner>
          ) : null}
          <div style={buttonRowStyle}>
            <Button variant="text" icon={expandIcon} onClick={() => setCollapsed(new Set())}>
              {texts.expandAll}
            </Button>
            <Button variant="text" icon={collapseIcon} onClick={() => setCollapsed(new Set(parents))}>
              {texts.collapseAll}
            </Button>
          </div>
          <Table<StatementRow> aria-label={texts.balanceSheet} columns={columns} rows={shown} manualSort getRowKey={rowKey} onRowPress={press} renderEmpty={() => texts.noRows} />
          {r.UNASSIGNED.length ? (
            <>
              <Title level={2} size="m">
                {texts.unassigned}
              </Title>
              <Table
                aria-label={texts.unassigned}
                columns={[
                  { key: 'ACCOUNT', header: texts.account, width: 120 },
                  { key: 'SHORT_TEXT', header: texts.shortText, width: 240 },
                  amount<StatementReport['UNASSIGNED'][number]>('BALANCE', texts.balance),
                ]}
                rows={r.UNASSIGNED}
                getRowKey={(x) => `${x.CHART} ${x.ACCOUNT}`}
              />
            </>
          ) : null}
        </section>
      ) : null}
    </ReportFrame>
  );
}
