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

// The balances of customers and suppliers of a company
// (/trfin/reports/partner-balances); a row opens the line items of the
// partner.

import { FormField, Select, Table } from '@tim-riep/trui';
import { useCallback, useState } from 'react';
import { errorText, type RemoteProps } from '../shared/context.ts';
import { CompanyField, IntegerField, LedgerField, LookupInput } from '../shared/fields.tsx';
import { partnerItems, useChoices, useCompanyData } from '../shared/masterdata.ts';
import { numberParam, param, paramsOf, ReportFrame, type Totals, TotalsLine, useAmountColumn, useReport, useRunOnOpen } from '../shared/report.tsx';
import { sectionStyle } from '../shared/styles.ts';
import { type Texts, textsOf } from '../shared/texts.ts';

export interface PartnerSelection {
  LEDGER: string | null;
  COMPANY: string;
  FISCAL_YEAR: number;
  FROM_PERIOD: number | null;
  TO_PERIOD: number | null;
  PARTNER_ROLE: string | null;
  PARTNER: string | null;
}

export interface PartnerBalanceRow {
  PARTNER_ROLE: string;
  PARTNER: string;
  NAME: string | null;
  CARRY_FORWARD: string;
  PRIOR_BALANCE: string;
  DEBIT: string;
  CREDIT: string;
  PERIOD_BALANCE: string;
  CUMULATIVE_BALANCE: string;
}

export interface PartnerBalanceReport {
  CURRENCY: string;
  FROM_PERIOD: number;
  TO_PERIOD: number;
  ROWS: PartnerBalanceRow[];
  TOTALS: Totals;
}

export const customerRole = '/TRFIN/CUSTOMER';
export const supplierRole = '/TRFIN/SUPPLIER';

/** The text of a role of TRFIN. */
export function roleText(role: string | null, texts: Texts): string {
  return role === customerRole ? texts.customer : role === supplierRole ? texts.supplier : (role ?? '');
}

/** The choice of customers or suppliers; null for both. */
export function RoleField({ id, value, onChange, texts, nullable = true }: { id: string; value: string | null; onChange: (r: string | null) => void; texts: Texts; nullable?: boolean }) {
  const items = [
    { value: customerRole, label: texts.customers },
    { value: supplierRole, label: texts.suppliers },
  ];
  return (
    <FormField label={texts.partnerRole} inputId={id} required={!nullable}>
      {nullable ? (
        <Select id={id} items={items} nullable placeholder={texts.anyRole} value={value} onChange={(v) => onChange(v)} />
      ) : (
        <Select id={id} items={items} value={value ?? ''} onChange={(v) => onChange(v)} required />
      )}
    </FormField>
  );
}

export default function PartnerBalancesPage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const onError = useCallback((e: unknown) => context.notify(errorText(e), 'error'), [context]);
  const choices = useChoices(context, onError);
  const [company, setCompany] = useState<string | null>(param(params, 'COMPANY'));
  const [ledger, setLedger] = useState<string | null>(param(params, 'LEDGER'));
  const [year, setYear] = useState<number | null>(numberParam(params, 'FISCAL_YEAR'));
  const [from, setFrom] = useState<number | null>(numberParam(params, 'FROM_PERIOD'));
  const [to, setTo] = useState<number | null>(numberParam(params, 'TO_PERIOD'));
  const [role, setRole] = useState<string | null>(param(params, 'PARTNER_ROLE'));
  const [partner, setPartner] = useState<string | null>(param(params, 'PARTNER'));
  const companyInfo = choices?.companies.find((c) => c.COMPANY === company) ?? null;
  const data = useCompanyData(context, companyInfo, onError);
  const report = useReport<PartnerSelection, PartnerBalanceReport>(context, 'partner-balances', texts);
  const amount = useAmountColumn();

  const selection = (): PartnerSelection | null =>
    company !== null && year !== null ? { LEDGER: ledger, COMPANY: company, FISCAL_YEAR: year, FROM_PERIOD: from, TO_PERIOD: to, PARTNER_ROLE: role, PARTNER: partner } : null;
  const run = useCallback(() => {
    const s = selection();
    if (s) void report.run(s);
  }, [company, ledger, year, from, to, role, partner, report.run]);
  useRunOnOpen(company !== null && year !== null && 'COMPANY' in params, run);

  const r = report.result;
  const s = report.ran;
  const drill = (row: PartnerBalanceRow) => {
    if (!s || !r) return;
    context.navigate(
      '/TRFINUI/LINE_ITEMS',
      paramsOf({ COMPANY: s.COMPANY, LEDGER: s.LEDGER, FISCAL_YEAR: s.FISCAL_YEAR, FROM_PERIOD: r.FROM_PERIOD, TO_PERIOD: r.TO_PERIOD, PARTNER_ROLE: row.PARTNER_ROLE, PARTNER: row.PARTNER }),
    );
  };
  const partners =
    role === supplierRole ? partnerItems(data.partners, 'SUPPLIER') : role === customerRole ? partnerItems(data.partners, 'CUSTOMER') : [...partnerItems(data.partners, 'CUSTOMER'), ...partnerItems(data.partners, 'SUPPLIER')];
  return (
    <ReportFrame
      title={texts.partnerBalances}
      texts={texts}
      canRun={selection() !== null}
      running={report.running}
      error={report.error}
      onRun={run}
      onExport={s ? () => void report.exportRows(s) : null}
      selection={
        <>
          <CompanyField id="partner-balances-company" companies={choices?.companies ?? []} value={company} onChange={setCompany} texts={texts} />
          <LedgerField id="partner-balances-ledger" ledgers={choices?.ledgers ?? []} value={ledger} onChange={setLedger} texts={texts} />
          <IntegerField id="partner-balances-year" label={texts.fiscalYear} required value={year} onChange={setYear} min={1900} max={9999} />
          <IntegerField id="partner-balances-from-period" label={texts.fromPeriod} value={from} onChange={setFrom} min={1} max={16} />
          <IntegerField id="partner-balances-to-period" label={texts.toPeriod} value={to} onChange={setTo} min={1} max={16} />
          <RoleField id="partner-balances-role" value={role} onChange={setRole} texts={texts} />
          <LookupInput id="partner-balances-partner" label={texts.partner} items={partners} value={partner} onChange={setPartner} />
        </>
      }
    >
      {r ? (
        <section style={sectionStyle} aria-label={texts.result}>
          <Table<PartnerBalanceRow>
            aria-label={texts.partnerBalances}
            columns={[
              { key: 'PARTNER_ROLE', header: texts.partnerRole, width: 110, renderCell: (row) => roleText(row.PARTNER_ROLE, texts) },
              { key: 'PARTNER', header: texts.partner, width: 120 },
              { key: 'NAME', header: texts.name, width: 200 },
              amount<PartnerBalanceRow>('CARRY_FORWARD', texts.carryForward),
              amount<PartnerBalanceRow>('DEBIT', texts.debit),
              amount<PartnerBalanceRow>('CREDIT', texts.credit),
              amount<PartnerBalanceRow>('PERIOD_BALANCE', texts.periodBalance),
              amount<PartnerBalanceRow>('CUMULATIVE_BALANCE', texts.cumulativeBalance),
            ]}
            rows={r.ROWS}
            getRowKey={(row) => `${row.PARTNER_ROLE} ${row.PARTNER}`}
            onRowPress={drill}
            renderEmpty={() => texts.noRows}
          />
          <TotalsLine totals={r.TOTALS} currency={r.CURRENCY} texts={texts} />
        </section>
      ) : null}
    </ReportFrame>
  );
}
