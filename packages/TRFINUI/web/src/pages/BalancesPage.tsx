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

// The balances of the accounts of a company per fiscal year and range of
// periods (/trfin/reports/balances); a row opens its line items.

import { Table, Title } from '@tim-riep/trui';
import { useCallback, useState } from 'react';
import { errorText, type RemoteProps } from '../shared/context.ts';
import { ChartKindField, CompanyField, IntegerField, LedgerField, LookupInput } from '../shared/fields.tsx';
import { accountItems, useChoices, useCompanyData } from '../shared/masterdata.ts';
import { numberParam, param, paramsOf, ReportFrame, type Totals, TotalsLine, useAmountColumn, useReport, useRunOnOpen } from '../shared/report.tsx';
import { sectionStyle } from '../shared/styles.ts';
import { textsOf } from '../shared/texts.ts';

export interface BalanceSelection {
  LEDGER: string | null;
  COMPANY: string;
  FISCAL_YEAR: number;
  FROM_PERIOD: number | null;
  TO_PERIOD: number | null;
  FROM_ACCOUNT: string | null;
  TO_ACCOUNT: string | null;
  CHART_KIND: string | null;
}

export interface BalanceRow {
  ACCOUNT: string;
  SHORT_TEXT: string | null;
  KIND: string | null;
  CARRY_FORWARD: string;
  PRIOR_BALANCE: string;
  DEBIT: string;
  CREDIT: string;
  PERIOD_BALANCE: string;
  CUMULATIVE_BALANCE: string;
}

export interface BalanceReport {
  LEDGER: string;
  COMPANY: string;
  CURRENCY: string;
  FISCAL_YEAR: number;
  FROM_PERIOD: number;
  TO_PERIOD: number;
  ROWS: BalanceRow[];
  UNASSIGNED: BalanceRow[];
  TOTALS: Totals;
}

export default function BalancesPage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const onError = useCallback((e: unknown) => context.notify(errorText(e), 'error'), [context]);
  const choices = useChoices(context, onError);
  const [company, setCompany] = useState<string | null>(param(params, 'COMPANY'));
  const [ledger, setLedger] = useState<string | null>(param(params, 'LEDGER'));
  const [year, setYear] = useState<number | null>(numberParam(params, 'FISCAL_YEAR'));
  const [from, setFrom] = useState<number | null>(numberParam(params, 'FROM_PERIOD'));
  const [to, setTo] = useState<number | null>(numberParam(params, 'TO_PERIOD'));
  const [fromAccount, setFromAccount] = useState<string | null>(param(params, 'FROM_ACCOUNT'));
  const [toAccount, setToAccount] = useState<string | null>(param(params, 'TO_ACCOUNT'));
  const [chartKind, setChartKind] = useState<string | null>(param(params, 'CHART_KIND'));
  const companyInfo = choices?.companies.find((c) => c.COMPANY === company) ?? null;
  const data = useCompanyData(context, companyInfo, onError);
  const report = useReport<BalanceSelection, BalanceReport>(context, 'balances', texts);
  const amount = useAmountColumn();

  const selection = (): BalanceSelection | null =>
    company !== null && year !== null
      ? { LEDGER: ledger, COMPANY: company, FISCAL_YEAR: year, FROM_PERIOD: from, TO_PERIOD: to, FROM_ACCOUNT: fromAccount, TO_ACCOUNT: toAccount, CHART_KIND: chartKind }
      : null;
  const run = useCallback(() => {
    const s = selection();
    if (s) void report.run(s);
  }, [company, ledger, year, from, to, fromAccount, toAccount, chartKind, report.run]);
  useRunOnOpen(company !== null && year !== null && 'COMPANY' in params, run);

  const r = report.result;
  const s = report.ran;
  const drill = (row: BalanceRow) => {
    if (!s || !r) return;
    context.navigate(
      '/TRFINUI/LINE_ITEMS',
      paramsOf({ COMPANY: s.COMPANY, LEDGER: s.LEDGER, FISCAL_YEAR: s.FISCAL_YEAR, FROM_PERIOD: r.FROM_PERIOD, TO_PERIOD: r.TO_PERIOD, CHART_KIND: s.CHART_KIND, ACCOUNT: row.ACCOUNT }),
    );
  };
  const items = accountItems(data.accounts, true);
  const columns = [
    { key: 'ACCOUNT', header: texts.account, width: 110 },
    { key: 'SHORT_TEXT', header: texts.shortText, width: 200 },
    amount<BalanceRow>('CARRY_FORWARD', texts.carryForward),
    amount<BalanceRow>('PRIOR_BALANCE', texts.priorBalance),
    amount<BalanceRow>('DEBIT', texts.debit),
    amount<BalanceRow>('CREDIT', texts.credit),
    amount<BalanceRow>('PERIOD_BALANCE', texts.periodBalance),
    amount<BalanceRow>('CUMULATIVE_BALANCE', texts.cumulativeBalance),
  ];
  return (
    <ReportFrame
      title={texts.balances}
      texts={texts}
      canRun={selection() !== null}
      running={report.running}
      error={report.error}
      onRun={run}
      onExport={s ? () => void report.exportRows(s) : null}
      selection={
        <>
          <CompanyField id="balances-company" companies={choices?.companies ?? []} value={company} onChange={setCompany} texts={texts} />
          <LedgerField id="balances-ledger" ledgers={choices?.ledgers ?? []} value={ledger} onChange={setLedger} texts={texts} />
          <ChartKindField id="balances-chart-kind" value={chartKind} onChange={setChartKind} texts={texts} />
          <IntegerField id="balances-year" label={texts.fiscalYear} required value={year} onChange={setYear} min={1900} max={9999} />
          <IntegerField id="balances-from-period" label={texts.fromPeriod} value={from} onChange={setFrom} min={1} max={16} />
          <IntegerField id="balances-to-period" label={texts.toPeriod} value={to} onChange={setTo} min={1} max={16} />
          <LookupInput id="balances-from-account" label={texts.fromAccount} items={items} value={fromAccount} onChange={setFromAccount} />
          <LookupInput id="balances-to-account" label={texts.toAccount} items={items} value={toAccount} onChange={setToAccount} />
        </>
      }
    >
      {r ? (
        <section style={sectionStyle} aria-label={texts.result}>
          <Table<BalanceRow>
            aria-label={texts.balances}
            columns={columns}
            rows={r.ROWS}
            getRowKey={(row) => row.ACCOUNT}
            onRowPress={drill}
            renderEmpty={() => texts.noRows}
          />
          <TotalsLine totals={r.TOTALS} currency={r.CURRENCY} texts={texts} />
          {r.UNASSIGNED.length ? (
            <>
              <Title level={2} size="m">
                {texts.unassigned}
              </Title>
              <Table<BalanceRow> aria-label={texts.unassigned} columns={columns} rows={r.UNASSIGNED} getRowKey={(row) => row.ACCOUNT} />
            </>
          ) : null}
        </section>
      ) : null}
    </ReportFrame>
  );
}
