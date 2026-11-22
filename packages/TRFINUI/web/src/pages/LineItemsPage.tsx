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

// The ledger entries of an account or a partner (/trfin/reports/line-items),
// where the other reports drill down to; a row opens its journal entry.

import { Table, Tag } from '@tim-riep/trui';
import { useCallback, useState } from 'react';
import { errorText, type RemoteProps } from '../shared/context.ts';
import { ChartKindField, CompanyField, IntegerField, LedgerField, LookupInput } from '../shared/fields.tsx';
import { accountItems, partnerItems, useChoices, useCompanyData } from '../shared/masterdata.ts';
import { Figures, numberParam, openEntry, param, ReportFrame, useAmountColumn, useReport, useRunOnOpen } from '../shared/report.tsx';
import { sectionStyle } from '../shared/styles.ts';
import { textsOf } from '../shared/texts.ts';
import { customerRole, RoleField, roleText, supplierRole } from './PartnerBalancesPage.tsx';

export interface LineItemSelection {
  LEDGER: string | null;
  COMPANY: string;
  FISCAL_YEAR: number;
  FROM_PERIOD: number | null;
  TO_PERIOD: number | null;
  CHART_KIND: string | null;
  ACCOUNT: string | null;
  PARTNER_ROLE: string | null;
  PARTNER: string | null;
}

export interface LineItemRow {
  COMPANY: string;
  FISCAL_YEAR: number;
  DOCUMENT_NO: string;
  LINE_NO: number;
  ITEM_NO: number | null;
  DOCUMENT_TYPE: string;
  POSTING_DATE: string;
  PERIOD: number;
  ACCOUNT: string;
  PARTNER_ROLE: string | null;
  PARTNER: string | null;
  AMOUNT: string;
  LINE_KIND: string;
  OPEN_ITEM: boolean;
  DUE_DATE: string | null;
  CLEARING_YEAR: number | null;
  CLEARING_NO: string | null;
  CLEARING_DATE: string | null;
}

export interface LineItemReport {
  CURRENCY: string;
  ROWS: LineItemRow[];
  TOTAL: string;
}

export default function LineItemsPage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const onError = useCallback((e: unknown) => context.notify(errorText(e), 'error'), [context]);
  const choices = useChoices(context, onError);
  const [company, setCompany] = useState<string | null>(param(params, 'COMPANY'));
  const [ledger, setLedger] = useState<string | null>(param(params, 'LEDGER'));
  const [year, setYear] = useState<number | null>(numberParam(params, 'FISCAL_YEAR'));
  const [from, setFrom] = useState<number | null>(numberParam(params, 'FROM_PERIOD'));
  const [to, setTo] = useState<number | null>(numberParam(params, 'TO_PERIOD'));
  const [chartKind, setChartKind] = useState<string | null>(param(params, 'CHART_KIND'));
  const [account, setAccount] = useState<string | null>(param(params, 'ACCOUNT'));
  const [role, setRole] = useState<string | null>(param(params, 'PARTNER_ROLE'));
  const [partner, setPartner] = useState<string | null>(param(params, 'PARTNER'));
  const companyInfo = choices?.companies.find((c) => c.COMPANY === company) ?? null;
  const data = useCompanyData(context, companyInfo, onError);
  const report = useReport<LineItemSelection, LineItemReport>(context, 'line-items', texts);
  const amount = useAmountColumn();

  const selection = (): LineItemSelection | null =>
    company !== null && year !== null
      ? { LEDGER: ledger, COMPANY: company, FISCAL_YEAR: year, FROM_PERIOD: from, TO_PERIOD: to, CHART_KIND: chartKind, ACCOUNT: account, PARTNER_ROLE: role, PARTNER: partner }
      : null;
  const run = useCallback(() => {
    const s = selection();
    if (s) void report.run(s);
  }, [company, ledger, year, from, to, chartKind, account, role, partner, report.run]);
  useRunOnOpen(company !== null && year !== null && 'COMPANY' in params, run);

  const r = report.result;
  const s = report.ran;
  const partners =
    role === supplierRole ? partnerItems(data.partners, 'SUPPLIER') : role === customerRole ? partnerItems(data.partners, 'CUSTOMER') : [...partnerItems(data.partners, 'CUSTOMER'), ...partnerItems(data.partners, 'SUPPLIER')];
  return (
    <ReportFrame
      title={texts.lineItems}
      texts={texts}
      canRun={selection() !== null}
      running={report.running}
      error={report.error}
      onRun={run}
      onExport={s ? () => void report.exportRows(s) : null}
      selection={
        <>
          <CompanyField id="line-items-company" companies={choices?.companies ?? []} value={company} onChange={setCompany} texts={texts} />
          <LedgerField id="line-items-ledger" ledgers={choices?.ledgers ?? []} value={ledger} onChange={setLedger} texts={texts} />
          <ChartKindField id="line-items-chart-kind" value={chartKind} onChange={setChartKind} texts={texts} />
          <IntegerField id="line-items-year" label={texts.fiscalYear} required value={year} onChange={setYear} min={1900} max={9999} />
          <IntegerField id="line-items-from-period" label={texts.fromPeriod} value={from} onChange={setFrom} min={1} max={16} />
          <IntegerField id="line-items-to-period" label={texts.toPeriod} value={to} onChange={setTo} min={1} max={16} />
          <LookupInput id="line-items-account" label={texts.account} items={accountItems(data.accounts, true)} value={account} onChange={setAccount} />
          <RoleField id="line-items-role" value={role} onChange={setRole} texts={texts} />
          <LookupInput id="line-items-partner" label={texts.partner} items={partners} value={partner} onChange={setPartner} />
        </>
      }
    >
      {r ? (
        <section style={sectionStyle} aria-label={texts.result}>
          <Table<LineItemRow>
            aria-label={texts.lineItems}
            columns={[
              { key: 'DOCUMENT_NO', header: texts.document, width: 130, renderCell: (row) => `${row.DOCUMENT_NO}/${row.FISCAL_YEAR}` },
              { key: 'ITEM_NO', header: texts.item, width: 70, align: 'end' },
              { key: 'DOCUMENT_TYPE', header: texts.documentType, width: 90 },
              { key: 'POSTING_DATE', header: texts.postingDate, width: 120 },
              { key: 'PERIOD', header: texts.period, width: 80, align: 'end' },
              { key: 'ACCOUNT', header: texts.account, width: 100 },
              { key: 'PARTNER', header: texts.partner, width: 140, renderCell: (row) => (row.PARTNER === null ? '' : `${row.PARTNER} ${roleText(row.PARTNER_ROLE, texts)}`) },
              amount<LineItemRow>('AMOUNT', texts.amount),
              { key: 'LINE_KIND', header: texts.lineKind, width: 140, renderCell: (row) => (row.LINE_KIND === 'ITEM' ? '' : <Tag>{row.LINE_KIND}</Tag>) },
              { key: 'CLEARING_NO', header: texts.clearing, width: 140, renderCell: (row) => (row.CLEARING_NO === null ? '' : `${row.CLEARING_NO}/${row.CLEARING_YEAR ?? ''}`) },
            ]}
            rows={r.ROWS}
            getRowKey={(row) => `${row.FISCAL_YEAR} ${row.DOCUMENT_NO} ${row.LINE_NO} ${row.ACCOUNT}`}
            onRowPress={(row) => void openEntry(context, row.COMPANY, row.FISCAL_YEAR, row.DOCUMENT_NO)}
            renderEmpty={() => texts.noRows}
          />
          <Figures label={texts.totals} currency={r.CURRENCY} figures={[[texts.total, r.TOTAL]]} />
        </section>
      ) : null}
    </ReportFrame>
  );
}
