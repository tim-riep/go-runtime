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

// The open items of a company at a key date with their due dates
// (/trfin/reports/open-items); a row opens its journal entry.

import { Table } from '@tim-riep/trui';
import { useCallback, useState } from 'react';
import { errorText, type RemoteProps } from '../shared/context.ts';
import { CompanyField, DateField, LedgerField, LookupInput } from '../shared/fields.tsx';
import { accountItems, partnerItems, useChoices, useCompanyData } from '../shared/masterdata.ts';
import { Figures, openEntry, param, ReportFrame, todayHere, useAmountColumn, useReport, useRunOnOpen } from '../shared/report.tsx';
import { sectionStyle } from '../shared/styles.ts';
import { textsOf } from '../shared/texts.ts';
import { customerRole, RoleField, roleText, supplierRole } from './PartnerBalancesPage.tsx';

export interface OpenItemSelection {
  LEDGER: string | null;
  COMPANY: string;
  KEY_DATE: string;
  ACCOUNT: string | null;
  PARTNER_ROLE: string | null;
  PARTNER: string | null;
}

export interface OpenItemRow {
  FISCAL_YEAR: number;
  DOCUMENT_NO: string;
  ITEM_NO: number | null;
  DOCUMENT_TYPE: string;
  POSTING_DATE: string;
  DUE_DATE: string;
  DAYS_OVERDUE: number;
  ACCOUNT: string;
  PARTNER_ROLE: string | null;
  PARTNER: string | null;
  AMOUNT: string;
  CLEARING_DATE: string | null;
}

export interface OpenItemReport {
  COMPANY: string;
  CURRENCY: string;
  KEY_DATE: string;
  ROWS: OpenItemRow[];
  TOTAL: string;
  NOT_DUE: string;
  OVERDUE: string;
  OVERDUE_1_30: string;
  OVERDUE_31_60: string;
  OVERDUE_61_90: string;
  OVERDUE_OVER_90: string;
}

/** The key of an open item, for selections. */
export function openItemKey(row: { FISCAL_YEAR: number; DOCUMENT_NO: string; ITEM_NO: number | null }): string {
  return `${row.FISCAL_YEAR}/${row.DOCUMENT_NO}/${row.ITEM_NO ?? ''}`;
}

export default function OpenItemsPage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const onError = useCallback((e: unknown) => context.notify(errorText(e), 'error'), [context]);
  const choices = useChoices(context, onError);
  const [company, setCompany] = useState<string | null>(param(params, 'COMPANY'));
  const [ledger, setLedger] = useState<string | null>(param(params, 'LEDGER'));
  const [keyDate, setKeyDate] = useState<string | null>(param(params, 'KEY_DATE') ?? todayHere());
  const [account, setAccount] = useState<string | null>(param(params, 'ACCOUNT'));
  const [role, setRole] = useState<string | null>(param(params, 'PARTNER_ROLE'));
  const [partner, setPartner] = useState<string | null>(param(params, 'PARTNER'));
  const companyInfo = choices?.companies.find((c) => c.COMPANY === company) ?? null;
  const data = useCompanyData(context, companyInfo, onError);
  const report = useReport<OpenItemSelection, OpenItemReport>(context, 'open-items', texts);
  const amount = useAmountColumn();

  const selection = (): OpenItemSelection | null =>
    company !== null && keyDate !== null ? { LEDGER: ledger, COMPANY: company, KEY_DATE: keyDate, ACCOUNT: account, PARTNER_ROLE: role, PARTNER: partner } : null;
  const run = useCallback(() => {
    const s = selection();
    if (s) void report.run(s);
  }, [company, ledger, keyDate, account, role, partner, report.run]);
  useRunOnOpen(company !== null && keyDate !== null && 'COMPANY' in params, run);

  const r = report.result;
  const s = report.ran;
  const partners =
    role === supplierRole ? partnerItems(data.partners, 'SUPPLIER') : role === customerRole ? partnerItems(data.partners, 'CUSTOMER') : [...partnerItems(data.partners, 'CUSTOMER'), ...partnerItems(data.partners, 'SUPPLIER')];
  return (
    <ReportFrame
      title={texts.openItems}
      texts={texts}
      canRun={selection() !== null}
      running={report.running}
      error={report.error}
      onRun={run}
      onExport={s ? () => void report.exportRows(s) : null}
      selection={
        <>
          <CompanyField id="open-items-company" companies={choices?.companies ?? []} value={company} onChange={setCompany} texts={texts} />
          <LedgerField id="open-items-ledger" ledgers={choices?.ledgers ?? []} value={ledger} onChange={setLedger} texts={texts} />
          <DateField id="open-items-key-date" label={texts.keyDate} required value={keyDate} onChange={setKeyDate} />
          <LookupInput id="open-items-account" label={texts.account} items={accountItems(data.accounts, true)} value={account} onChange={setAccount} />
          <RoleField id="open-items-role" value={role} onChange={setRole} texts={texts} />
          <LookupInput id="open-items-partner" label={texts.partner} items={partners} value={partner} onChange={setPartner} />
        </>
      }
    >
      {r ? (
        <section style={sectionStyle} aria-label={texts.result}>
          <Figures
            label={texts.totals}
            currency={r.CURRENCY}
            figures={[
              [texts.total, r.TOTAL],
              [texts.notDue, r.NOT_DUE],
              [texts.overdue, r.OVERDUE],
              [texts.overdue1to30, r.OVERDUE_1_30],
              [texts.overdue31to60, r.OVERDUE_31_60],
              [texts.overdue61to90, r.OVERDUE_61_90],
              [texts.overdueOver90, r.OVERDUE_OVER_90],
            ]}
          />
          <Table<OpenItemRow>
            aria-label={texts.openItems}
            columns={openItemColumns(texts, amount)}
            rows={r.ROWS}
            getRowKey={openItemKey}
            onRowPress={(row) => void openEntry(context, r.COMPANY, row.FISCAL_YEAR, row.DOCUMENT_NO)}
            renderEmpty={() => texts.noRows}
          />
        </section>
      ) : null}
    </ReportFrame>
  );
}

/** The columns of open items, also of the clearing. */
export function openItemColumns(texts: ReturnType<typeof textsOf>, amount: ReturnType<typeof useAmountColumn>) {
  return [
    { key: 'DOCUMENT_NO', header: texts.document, width: 130, renderCell: (row: OpenItemRow) => `${row.DOCUMENT_NO}/${row.FISCAL_YEAR}` },
    { key: 'ITEM_NO', header: texts.item, width: 70, align: 'end' as const },
    { key: 'DOCUMENT_TYPE', header: texts.documentType, width: 90 },
    { key: 'POSTING_DATE', header: texts.postingDate, width: 120 },
    { key: 'DUE_DATE', header: texts.dueDate, width: 120 },
    { key: 'DAYS_OVERDUE', header: texts.daysOverdue, width: 110, align: 'end' as const },
    { key: 'ACCOUNT', header: texts.account, width: 100 },
    { key: 'PARTNER', header: texts.partner, width: 150, renderCell: (row: OpenItemRow) => (row.PARTNER === null ? '' : `${row.PARTNER} ${roleText(row.PARTNER_ROLE, texts)}`) },
    amount<OpenItemRow>('AMOUNT', texts.amount),
  ];
}
