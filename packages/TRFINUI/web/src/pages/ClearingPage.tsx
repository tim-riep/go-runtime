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

// Clearing open items of an account or a partner with CLEAR of
// /TRFIN/JOURNAL_ENTRY_SERVICE: the open items at the posting date from
// /trfin/reports/open-items to select, optional new items such as a
// payment, and the difference that must be zero: the selected items plus
// the new items on the same account or partner.

import { Banner, Button, Form, FormField, FormGroup, PageHeader, Select, Table, Tag, TextField, Title } from '@tim-riep/trui';
import { addDecimal, useFormat } from '@tim-riep/trui/core';
import { addIcon } from '@tim-riep/trui/icons';
import { useCallback, useState } from 'react';
import { errorText, journalEntryService, type RemoteProps } from '../shared/context.ts';
import { CompanyField, DateField, LookupInput } from '../shared/fields.tsx';
import { ItemsTable } from '../shared/ItemsTable.tsx';
import { type AccountKind, clearingDifference, type ItemDraft, isZero, missingFields, offsetItems, type PostingMessage, postingItems, sortMessages } from '../shared/items.ts';
import { accountItems, partnerItems, useChoices, useCompanyData } from '../shared/masterdata.ts';
import { openEntry, param, todayHere, useAmountColumn } from '../shared/report.tsx';
import { buttonRowStyle, fieldsStyle, pageStyle, sectionStyle } from '../shared/styles.ts';
import { format, textsOf } from '../shared/texts.ts';
import { check, type PostedEntry } from './JournalEntryPage.tsx';
import { openItemColumns, openItemKey, type OpenItemReport, type OpenItemRow } from './OpenItemsPage.tsx';
import { customerRole, supplierRole } from './PartnerBalancesPage.tsx';

export default function ClearingPage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const fmt = useFormat();
  const amount = useAmountColumn();
  const onError = useCallback((e: unknown) => context.notify(errorText(e), 'error'), [context]);
  const choices = useChoices(context, onError);
  const [company, setCompany] = useState<string | null>(param(params, 'COMPANY'));
  const [kind, setKind] = useState<AccountKind>((param(params, 'ACCOUNT_KIND') as AccountKind | null) ?? 'CUSTOMER');
  const [target, setTarget] = useState<string | null>(param(params, 'PARTNER') ?? param(params, 'ACCOUNT'));
  const [documentType, setDocumentType] = useState<string | null>(param(params, 'DOCUMENT_TYPE'));
  const [postingDate, setPostingDate] = useState<string | null>(todayHere());
  const [headerText, setHeaderText] = useState<string | null>(null);
  const [open, setOpen] = useState<OpenItemReport | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [items, setItems] = useState<ItemDraft[]>([]);
  const [messages, setMessages] = useState<PostingMessage[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [cleared, setCleared] = useState<PostedEntry | null>(null);
  const companyInfo = choices?.companies.find((c) => c.COMPANY === company) ?? null;
  const data = useCompanyData(context, companyInfo, onError);
  const currency = companyInfo?.CURRENCY ?? 'EUR';

  const targets = kind === 'ACCOUNT' ? accountItems(data.accounts, true) : partnerItems(data.partners, kind);
  const kinds = [
    { value: 'CUSTOMER', label: texts.customer },
    { value: 'SUPPLIER', label: texts.supplier },
    { value: 'ACCOUNT', label: texts.accounts },
  ];
  const rows = (open?.ROWS ?? []).filter((r) => r.ITEM_NO !== null);
  const chosen = rows.filter((r) => selected.has(openItemKey(r)));
  const difference = clearingDifference(chosen, items, kind, target);

  const load = async () => {
    if (company === null || target === null || postingDate === null) return;
    setFailure(null);
    setBusy(true);
    try {
      const selection =
        kind === 'ACCOUNT'
          ? { LEDGER: null, COMPANY: company, KEY_DATE: postingDate, ACCOUNT: target, PARTNER_ROLE: null, PARTNER: null }
          : { LEDGER: null, COMPANY: company, KEY_DATE: postingDate, ACCOUNT: null, PARTNER_ROLE: kind === 'CUSTOMER' ? customerRole : supplierRole, PARTNER: target };
      setOpen(await context.fetchJSON<OpenItemReport>('/trfin/reports/open-items', { body: selection }));
      setSelected(new Set());
    } catch (e) {
      setOpen(null);
      setFailure(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const clear = async () => {
    setFailure(null);
    if (company === null || documentType === null || postingDate === null || !chosen.length) {
      setFailure(texts.nothingSelected);
      return;
    }
    const local = missingFields(items, texts);
    if (local.length) {
      setMessages(local);
      return;
    }
    const clearing = {
      COMPANY: company,
      DOCUMENT_TYPE: documentType,
      DOCUMENT_DATE: null,
      POSTING_DATE: postingDate,
      REFERENCE: null,
      HEADER_TEXT: headerText,
      OPEN_ITEMS: chosen.map((r) => ({ FISCAL_YEAR: r.FISCAL_YEAR, DOCUMENT_NO: r.DOCUMENT_NO, ITEM_NO: r.ITEM_NO })),
      ITEMS: postingItems(items),
    };
    setBusy(true);
    try {
      const r = await context.odata(journalEntryService).action('CLEAR', clearing as unknown as Record<string, never>);
      const entry = r.entity as unknown as PostedEntry;
      setMessages([]);
      setCleared(entry);
      context.notify(format(texts.cleared, { no: entry.DOCUMENT_NO, year: entry.FISCAL_YEAR }), 'success');
    } catch (e) {
      // Messages at new items come from CHECK of the same items.
      if (items.length) {
        try {
          const c = await check(context, { COMPANY: company, DOCUMENT_TYPE: documentType, DOCUMENT_DATE: postingDate, POSTING_DATE: postingDate, REFERENCE: null, HEADER_TEXT: headerText, LEDGER: null, ITEMS: postingItems(items) });
          setMessages(c.MESSAGES.filter((m) => m.ITEM_NO !== null));
        } catch {
          // The error of CLEAR says enough.
        }
      }
      setFailure(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const restart = () => {
    setCleared(null);
    setItems([]);
    setMessages([]);
    void load();
  };

  if (cleared) {
    return (
      <div style={pageStyle}>
        <PageHeader title={texts.clearOpenItems} headingLevel={1} />
        <Banner tone="success" title={format(texts.cleared, { no: cleared.DOCUMENT_NO, year: cleared.FISCAL_YEAR })}>
          <div style={buttonRowStyle}>
            <Button variant="primary" onClick={() => void openEntry(context, cleared.COMPANY, cleared.FISCAL_YEAR, cleared.DOCUMENT_NO)}>
              {texts.openEntry}
            </Button>
            <Button onClick={restart}>{texts.loadItems}</Button>
          </div>
        </Banner>
      </div>
    );
  }

  return (
    <div style={pageStyle}>
      <PageHeader
        title={texts.clearOpenItems}
        headingLevel={1}
        status={
          open ? (
            <Tag tone={isZero(difference) && chosen.length ? 'success' : 'warning'}>
              {texts.differenceToClear}: {fmt.decimal(difference, { scale: 2, currency })}
            </Tag>
          ) : undefined
        }
      />
      <Form
        aria-label={texts.selection}
        columns={3}
        onSubmit={(e) => {
          e.preventDefault();
          void load();
        }}
        actions={
          <Button type="submit" disabled={company === null || target === null || postingDate === null} loading={busy}>
            {texts.loadItems}
          </Button>
        }
      >
        <FormGroup title={texts.selection} headingLevel={2} span={3}>
          <div style={fieldsStyle}>
          <CompanyField id="clearing-company" companies={choices?.companies ?? []} value={company} onChange={setCompany} texts={texts} />
          <FormField label={texts.accountKind} required inputId="clearing-kind">
            <Select
              id="clearing-kind"
              items={kinds}
              value={kind}
              onChange={(v) => {
                setKind(v as AccountKind);
                setTarget(null);
                setOpen(null);
              }}
              required
            />
          </FormField>
          <LookupInput id="clearing-target" label={kind === 'ACCOUNT' ? texts.account : texts.partner} items={targets} value={target} onChange={setTarget} />
          <FormField label={texts.documentType} required inputId="clearing-document-type">
            <Select id="clearing-document-type" items={choices?.documentTypes ?? []} value={documentType ?? ''} onChange={(v) => setDocumentType(v)} required />
          </FormField>
          <DateField id="clearing-posting-date" label={texts.postingDate} required value={postingDate} onChange={setPostingDate} />
          <FormField label={texts.headerText} inputId="clearing-header-text">
            <TextField id="clearing-header-text" nullable maxLength={50} value={headerText} onChange={(v) => setHeaderText(v)} />
          </FormField>
          </div>
        </FormGroup>
      </Form>
      {open ? (
        <section style={sectionStyle} aria-label={texts.openItems}>
          <Title level={2} size="m">
            {texts.openItems}
          </Title>
          <Table<OpenItemRow>
            aria-label={texts.openItems}
            columns={openItemColumns(texts, amount)}
            rows={rows}
            getRowKey={openItemKey}
            selectionMode="multiple"
            selectedKeys={selected}
            onSelectionChange={setSelected}
            renderEmpty={() => texts.noRows}
          />
          <div style={buttonRowStyle}>
            <span>
              {texts.selected}: {fmt.decimal(chosen.reduce((s, r) => addDecimal(s, r.AMOUNT), '0'), { scale: 2, currency })}
            </span>
            {chosen.length && !isZero(difference) && !items.length ? (
              <Button icon={addIcon} onClick={() => setItems(offsetItems(difference, kind, target))}>
                {texts.offsetItems}
              </Button>
            ) : null}
          </div>
          <Title level={2} size="m">
            {texts.newItems}
          </Title>
          <ItemsTable id="clearing" label={texts.newItems} items={items} onChange={setItems} data={data} currency={currency} messages={sortMessages(messages)} texts={texts} />
          {failure !== null ? (
            <Banner tone="error" title={texts.failed}>
              {failure}
            </Banner>
          ) : null}
          <div style={buttonRowStyle}>
            <Button variant="primary" onClick={() => void clear()} loading={busy} disabled={!chosen.length || !isZero(difference)}>
              {texts.clear}
            </Button>
          </div>
        </section>
      ) : failure !== null ? (
        <Banner tone="error" title={texts.failed}>
          {failure}
        </Banner>
      ) : null}
    </div>
  );
}
