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

// The configuration and master data the pages choose from: companies,
// ledgers, document types, and the accounts and partners of a company with
// their texts, read once per context through the services of TRFIN.

import type { ListItem } from '@tim-riep/trui/core';
import { useEffect, useState } from 'react';
import { allEntities, configurationService, type Entity, literal, masterDataService, type RemoteContext } from './context.ts';

export interface Company {
  COMPANY: string;
  NAME: string;
  CURRENCY: string;
  CHART: string;
  COUNTRY_CHART: string | null;
}

export interface Account {
  ACCOUNT: string;
  TEXT: string;
  /** NONE, CUSTOMERS or SUPPLIERS. */
  RECONCILIATION: string;
  BLOCKED: boolean;
}

export type PartnerKind = 'CUSTOMER' | 'SUPPLIER';

export interface Partner {
  PARTNER: string;
  NAME: string;
  KIND: PartnerKind;
}

export interface Structure {
  STRUCTURE: string;
  CHART: string;
  LABEL: string;
}

export interface Choices {
  companies: Company[];
  ledgers: ListItem[];
  documentTypes: ListItem[];
  structures: Structure[];
}

export interface CompanyData {
  accounts: Account[];
  partners: Partner[];
}

const choices = new WeakMap<RemoteContext, Promise<Choices>>();
const companyData = new WeakMap<RemoteContext, Map<string, Promise<CompanyData>>>();

/** The text of a language among texts, else English, else the first. */
function textOf(texts: Entity[] | undefined, language: string, field: string): string {
  const all = texts ?? [];
  const t = all.find((x) => x.LANGUAGE === language) ?? all.find((x) => x.LANGUAGE === 'en') ?? all[0];
  const v = t?.[field];
  return typeof v === 'string' ? v : '';
}

async function loadChoices(context: RemoteContext): Promise<Choices> {
  const config = context.odata(configurationService);
  const [companies, ledgers, types, structures] = await Promise.all([
    allEntities(config, 'Companies', { orderby: 'COMPANY' }),
    allEntities(config, 'Ledgers', { orderby: 'LEDGER', expand: ['LedgerTexts'] }),
    allEntities(config, 'DocumentTypes', { orderby: 'DOCUMENT_TYPE', expand: ['DocumentTypeTexts'] }),
    allEntities(config, 'StatementStructures', { orderby: 'STRUCTURE', expand: ['StatementStructureTexts'] }),
  ]);
  return {
    companies: companies.map((c) => ({
      COMPANY: String(c.COMPANY),
      NAME: typeof c.NAME === 'string' ? c.NAME : '',
      CURRENCY: String(c.CURRENCY),
      CHART: String(c.CHART),
      COUNTRY_CHART: typeof c.COUNTRY_CHART === 'string' ? c.COUNTRY_CHART : null,
    })),
    ledgers: ledgers.map((l) => ({ value: String(l.LEDGER), label: String(l.LEDGER), description: textOf(l.LedgerTexts as Entity[], context.language, 'LABEL') })),
    documentTypes: types.map((t) => ({
      value: String(t.DOCUMENT_TYPE),
      label: String(t.DOCUMENT_TYPE),
      description: textOf(t.DocumentTypeTexts as Entity[], context.language, 'LABEL'),
    })),
    structures: structures.map((x) => ({ STRUCTURE: String(x.STRUCTURE), CHART: String(x.CHART), LABEL: textOf(x.StatementStructureTexts as Entity[], context.language, 'LABEL') })),
  };
}

/** Companies, ledgers and document types, read once per context. */
export function choicesOf(context: RemoteContext): Promise<Choices> {
  let p = choices.get(context);
  if (!p) {
    p = loadChoices(context);
    p.catch(() => choices.delete(context));
    choices.set(context, p);
  }
  return p;
}

/** Groups of OData eq conditions, so that filters stay short. */
function chunks<T>(list: T[], size: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size));
  return out;
}

async function loadCompanyData(context: RemoteContext, company: Company): Promise<CompanyData> {
  const md = context.odata(masterDataService);
  const c = literal(company.COMPANY);
  const [companyAccounts, chartAccounts, customers, suppliers] = await Promise.all([
    allEntities(md, 'CompanyAccounts', { filter: `COMPANY eq ${c}`, orderby: 'ACCOUNT' }),
    allEntities(md, 'Accounts', { filter: `CHART eq ${literal(company.CHART)}`, expand: ['AccountTexts'] }),
    allEntities(md, 'Customers', { filter: `COMPANY eq ${c}`, orderby: 'PARTNER' }),
    allEntities(md, 'Suppliers', { filter: `COMPANY eq ${c}`, orderby: 'PARTNER' }),
  ]);
  const texts = new Map(chartAccounts.map((a) => [String(a.ACCOUNT), textOf(a.AccountTexts as Entity[], context.language, 'SHORT_TEXT')]));
  const ids = [...new Set([...customers, ...suppliers].map((p) => String(p.PARTNER)))];
  const names = new Map<string, string>();
  for (const part of chunks(ids, 40)) {
    const found = await allEntities(md, 'Partners', { filter: part.map((p) => `PARTNER eq ${literal(p)}`).join(' or '), select: ['PARTNER', 'NAME1'] });
    for (const p of found) names.set(String(p.PARTNER), typeof p.NAME1 === 'string' ? p.NAME1 : '');
  }
  const partner = (kind: PartnerKind) => (p: Entity): Partner => ({ PARTNER: String(p.PARTNER), NAME: names.get(String(p.PARTNER)) ?? '', KIND: kind });
  return {
    accounts: companyAccounts.map((a) => ({
      ACCOUNT: String(a.ACCOUNT),
      TEXT: texts.get(String(a.ACCOUNT)) ?? '',
      RECONCILIATION: String(a.RECONCILIATION ?? 'NONE'),
      BLOCKED: a.BLOCKED === true,
    })),
    partners: [...customers.map(partner('CUSTOMER')), ...suppliers.map(partner('SUPPLIER'))],
  };
}

/** The accounts and partners of a company, read once per context. */
export function companyDataOf(context: RemoteContext, company: Company): Promise<CompanyData> {
  let m = companyData.get(context);
  if (!m) {
    m = new Map();
    companyData.set(context, m);
  }
  let p = m.get(company.COMPANY);
  if (!p) {
    p = loadCompanyData(context, company);
    const map = m;
    p.catch(() => map.delete(company.COMPANY));
    m.set(company.COMPANY, p);
  }
  return p;
}

/** The choices of the context; null while they load. */
export function useChoices(context: RemoteContext, onError: (e: unknown) => void): Choices | null {
  const [c, setC] = useState<Choices | null>(null);
  useEffect(() => {
    let current = true;
    choicesOf(context).then(
      (x) => current && setC(x),
      (e: unknown) => current && onError(e),
    );
    return () => {
      current = false;
    };
    // onError is no dependency: a new callback must not load again.
  }, [context]);
  return c;
}

/** The accounts and partners of a company; empty while they load or without company. */
export function useCompanyData(context: RemoteContext, company: Company | null, onError: (e: unknown) => void): CompanyData {
  const [d, setD] = useState<CompanyData>({ accounts: [], partners: [] });
  useEffect(() => {
    let current = true;
    setD({ accounts: [], partners: [] });
    if (company) {
      companyDataOf(context, company).then(
        (x) => current && setD(x),
        (e: unknown) => current && onError(e),
      );
    }
    return () => {
      current = false;
    };
  }, [context, company?.COMPANY]);
  return d;
}

/** Items of a value help, searched on the client by value, label and description. */
export function searchItems(items: readonly ListItem[]) {
  return async ({ query, offset, limit }: { query: string; offset: number; limit: number }) => {
    const q = query.trim().toLowerCase();
    const found = q ? items.filter((i) => i.value.toLowerCase().includes(q) || i.label.toLowerCase().includes(q) || (i.description ?? '').toLowerCase().includes(q)) : items;
    return { items: found.slice(offset, offset + limit), total: found.length };
  };
}

/** The accounts as items of a value help; reconciliation accounts only when asked for. */
export function accountItems(accounts: Account[], withReconciliation = false): ListItem[] {
  return accounts
    .filter((a) => !a.BLOCKED && (withReconciliation || a.RECONCILIATION === 'NONE'))
    .map((a) => ({ value: a.ACCOUNT, label: a.ACCOUNT, description: a.TEXT }));
}

/** The partners of a kind as items of a value help. */
export function partnerItems(partners: Partner[], kind: PartnerKind): ListItem[] {
  return partners.filter((p) => p.KIND === kind).map((p) => ({ value: p.PARTNER, label: p.PARTNER, description: p.NAME }));
}
