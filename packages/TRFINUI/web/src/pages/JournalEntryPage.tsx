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

// Entering a journal entry: the header, the items in an editable table with
// the running balance, CHECK and POST of /TRFIN/JOURNAL_ENTRY_SERVICE. The
// messages of CHECK show at the fields and cells they are about; a failed
// POST is checked again to place its messages the same way.

import { Banner, Button, Form, FormField, FormGroup, type Message, MessageList, PageHeader, Select, Tag, TextField } from '@tim-riep/trui';
import { useFormat } from '@tim-riep/trui/core';
import { useCallback, useState } from 'react';
import { errorText, journalEntryService, type RemoteContext, type RemoteProps } from '../shared/context.ts';
import { CompanyField, DateField, LedgerField } from '../shared/fields.tsx';
import { ItemsTable } from '../shared/ItemsTable.tsx';
import { balanceOf, cellId, type ItemDraft, isZero, itemFields, missingFields, newItem, type PostingItem, type PostingMessage, postingItems, sortMessages } from '../shared/items.ts';
import { useChoices, useCompanyData } from '../shared/masterdata.ts';
import { openEntry, param, todayHere } from '../shared/report.tsx';
import { buttonRowStyle, fieldsStyle, pageStyle } from '../shared/styles.ts';
import { format, type Texts, textsOf } from '../shared/texts.ts';

export interface Posting {
  COMPANY: string;
  DOCUMENT_TYPE: string;
  DOCUMENT_DATE: string;
  POSTING_DATE: string;
  REFERENCE: string | null;
  HEADER_TEXT: string | null;
  LEDGER: string | null;
  ITEMS: PostingItem[];
}

export interface CheckResult {
  FISCAL_YEAR: number | null;
  PERIOD: number | null;
  MESSAGES: PostingMessage[];
}

export interface PostedEntry {
  COMPANY: string;
  FISCAL_YEAR: number;
  DOCUMENT_NO: string;
}

/** The fields of the header with an input, as targets of messages. */
const headerFields: Record<string, string> = {
  COMPANY: 'je-company',
  DOCUMENT_TYPE: 'je-document-type',
  DOCUMENT_DATE: 'je-document-date',
  POSTING_DATE: 'je-posting-date',
  REFERENCE: 'je-reference',
  HEADER_TEXT: 'je-header-text',
  LEDGER: 'je-ledger',
};

/**
 * The messages for the list of the page, with the inputs they are about as
 * targets: fields of the header, cells of the items (the account of a
 * partner item is its partner).
 */
export function listMessages(prefix: string, messages: readonly PostingMessage[], texts: Texts, header: Record<string, string>, items: readonly ItemDraft[]): Message[] {
  return messages.map((m, i) => {
    let target: string | undefined;
    if (m.ITEM_NO === null) {
      target = m.FIELD !== null ? header[m.FIELD] : undefined;
    } else if (m.FIELD !== null && itemFields.includes(m.FIELD)) {
      const partner = items[m.ITEM_NO - 1]?.ACCOUNT_KIND !== 'ACCOUNT';
      target = cellId(prefix, m.ITEM_NO, partner && m.FIELD === 'ACCOUNT' ? 'PARTNER' : m.FIELD);
    }
    return {
      id: `message-${i}`,
      type: 'error' as const,
      title: m.ITEM_NO === null ? texts.entryMessage : format(texts.itemMessage, { no: m.ITEM_NO }),
      description: m.TEXT,
      ...(target ? { target } : {}),
    };
  });
}

/** Runs CHECK; the messages and the period, or the error of the request. */
export async function check(context: RemoteContext, posting: Posting): Promise<CheckResult> {
  const r = await context.odata(journalEntryService).action('CHECK', posting as unknown as Record<string, never>);
  return r.entity as unknown as CheckResult;
}

export default function JournalEntryPage({ params, context }: RemoteProps) {
  const texts = textsOf(context.language);
  const fmt = useFormat();
  const onError = useCallback((e: unknown) => context.notify(errorText(e), 'error'), [context]);
  const choices = useChoices(context, onError);
  const [company, setCompany] = useState<string | null>(param(params, 'COMPANY'));
  const [documentType, setDocumentType] = useState<string | null>(param(params, 'DOCUMENT_TYPE'));
  const [postingDate, setPostingDate] = useState<string | null>(todayHere());
  const [documentDate, setDocumentDate] = useState<string | null>(todayHere());
  const [reference, setReference] = useState<string | null>(null);
  const [headerText, setHeaderText] = useState<string | null>(null);
  const [ledger, setLedger] = useState<string | null>(null);
  const [items, setItems] = useState<ItemDraft[]>(() => [newItem('D'), newItem('C')]);
  const [messages, setMessages] = useState<PostingMessage[]>([]);
  const [info, setInfo] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [posted, setPosted] = useState<PostedEntry | null>(null);
  const companyInfo = choices?.companies.find((c) => c.COMPANY === company) ?? null;
  const data = useCompanyData(context, companyInfo, onError);
  const currency = companyInfo?.CURRENCY ?? 'EUR';
  const balance = balanceOf(items);
  const sorted = sortMessages(messages);

  const posting = (): Posting | null => {
    const local = missingFields(items, texts);
    const head: PostingMessage[] = [];
    if (company === null) head.push({ ITEM_NO: null, FIELD: 'COMPANY', TEXT: format(texts.required, { field: texts.company }) });
    if (documentType === null) head.push({ ITEM_NO: null, FIELD: 'DOCUMENT_TYPE', TEXT: format(texts.required, { field: texts.documentType }) });
    if (postingDate === null) head.push({ ITEM_NO: null, FIELD: 'POSTING_DATE', TEXT: format(texts.required, { field: texts.postingDate }) });
    if (documentDate === null) head.push({ ITEM_NO: null, FIELD: 'DOCUMENT_DATE', TEXT: format(texts.required, { field: texts.documentDate }) });
    if (head.length || local.length || company === null || documentType === null || postingDate === null || documentDate === null) {
      setMessages([...head, ...local]);
      return null;
    }
    return {
      COMPANY: company,
      DOCUMENT_TYPE: documentType,
      DOCUMENT_DATE: documentDate,
      POSTING_DATE: postingDate,
      REFERENCE: reference,
      HEADER_TEXT: headerText,
      LEDGER: ledger,
      ITEMS: postingItems(items),
    };
  };

  const runCheck = async () => {
    setInfo(null);
    setFailure(null);
    const p = posting();
    if (!p) return;
    setBusy(true);
    try {
      const r = await check(context, p);
      setMessages(r.MESSAGES);
      if (!r.MESSAGES.length) setInfo(format(texts.checkedFine, { period: r.PERIOD ?? '', year: r.FISCAL_YEAR ?? '' }));
    } catch (e) {
      setFailure(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const runPost = async () => {
    setInfo(null);
    setFailure(null);
    const p = posting();
    if (!p) return;
    setBusy(true);
    try {
      const r = await context.odata(journalEntryService).action('POST', p as unknown as Record<string, never>);
      const entry = r.entity as unknown as PostedEntry;
      setMessages([]);
      setPosted(entry);
      context.notify(format(texts.posted, { no: entry.DOCUMENT_NO, year: entry.FISCAL_YEAR }), 'success');
    } catch (e) {
      // The messages of a failed posting come without items; CHECK
      // places them at the cells.
      try {
        const r = await check(context, p);
        setMessages(r.MESSAGES);
        if (!r.MESSAGES.length) setFailure(errorText(e));
      } catch {
        setFailure(errorText(e));
      }
    } finally {
      setBusy(false);
    }
  };

  const restart = () => {
    setPosted(null);
    setItems([newItem('D'), newItem('C')]);
    setReference(null);
    setHeaderText(null);
    setMessages([]);
    setInfo(null);
  };

  const headerMessage = (field: string) => {
    const list = sorted.entry.filter((m) => m.FIELD === field);
    return list.length ? { valueState: 'error' as const, valueStateMessage: list.map((m) => m.TEXT).join(' ') } : {};
  };
  const documentTypes = choices?.documentTypes ?? [];

  if (posted) {
    return (
      <div style={pageStyle}>
        <PageHeader title={texts.postJournalEntry} headingLevel={1} />
        <Banner tone="success" title={format(texts.posted, { no: posted.DOCUMENT_NO, year: posted.FISCAL_YEAR })}>
          <div style={buttonRowStyle}>
            <Button variant="primary" onClick={() => void openEntry(context, posted.COMPANY, posted.FISCAL_YEAR, posted.DOCUMENT_NO)}>
              {texts.openEntry}
            </Button>
            <Button onClick={restart}>{texts.newEntry}</Button>
          </div>
        </Banner>
      </div>
    );
  }

  return (
    <div style={pageStyle}>
      <PageHeader
        title={texts.postJournalEntry}
        headingLevel={1}
        status={isZero(balance) ? <Tag tone="success">{texts.balanced}</Tag> : <Tag tone="warning">{format(texts.notBalancedYet, { amount: fmt.decimal(balance, { scale: 2, currency }) })}</Tag>}
      />
      <Form
        aria-label={texts.header}
        columns={3}
        onSubmit={(e) => {
          e.preventDefault();
          void runCheck();
        }}
      >
        <FormGroup title={texts.header} headingLevel={2} span={3}>
          <div style={fieldsStyle}>
          <CompanyField id="je-company" companies={choices?.companies ?? []} value={company} onChange={setCompany} texts={texts} />
          <FormField label={texts.documentType} required inputId="je-document-type">
            <Select id="je-document-type" items={documentTypes} value={documentType ?? ''} onChange={(v) => setDocumentType(v)} required {...headerMessage('DOCUMENT_TYPE')} />
          </FormField>
          <LedgerField id="je-ledger" ledgers={choices?.ledgers ?? []} value={ledger} onChange={setLedger} texts={texts} />
          <DateField id="je-posting-date" label={texts.postingDate} required value={postingDate} onChange={setPostingDate} />
          <DateField id="je-document-date" label={texts.documentDate} required value={documentDate} onChange={setDocumentDate} />
          <FormField label={texts.reference} inputId="je-reference">
            <TextField id="je-reference" nullable maxLength={16} value={reference} onChange={(v) => setReference(v)} {...headerMessage('REFERENCE')} />
          </FormField>
          <FormField label={texts.headerText} inputId="je-header-text">
            <TextField id="je-header-text" nullable maxLength={50} value={headerText} onChange={(v) => setHeaderText(v)} {...headerMessage('HEADER_TEXT')} />
          </FormField>
          </div>
        </FormGroup>
      </Form>
      <ItemsTable id="je" label={texts.items} items={items} onChange={setItems} data={data} currency={currency} messages={sorted} texts={texts} />
      {messages.length ? <MessageList messages={listMessages('je', messages, texts, headerFields, items)} /> : null}
      {info !== null ? <Banner tone="success">{info}</Banner> : null}
      {failure !== null ? (
        <Banner tone="error" title={texts.failed}>
          {failure}
        </Banner>
      ) : null}
      <div style={buttonRowStyle}>
        <Button onClick={() => void runCheck()} loading={busy}>
          {texts.check}
        </Button>
        <Button variant="primary" onClick={() => void runPost()} loading={busy}>
          {texts.post}
        </Button>
      </div>
    </div>
  );
}
