# TRFIN: general ledger

TRFIN is the general ledger of a tenant. This version brings its configuration and master
data: currencies, charts of accounts, companies, ledgers, document types, balance sheet
structures, posting periods, the accounts of charts and companies and the company data of
customers and suppliers; and journal entries, posted to the ledgers with totals, running
balances of customers and suppliers and the reclassification of their opposite balances,
reversed, and their open items cleared. Reports follow. TRFIN requires TRBASE 1.11.0 or later and TRBP
1.0.0 or later; all its tables hold data of a tenant, and amounts are `DEC(18,2)`.

## Configuration

Configuration is maintained in business objects of the data class `configuration`, checked
with `/TRBASE/CONFIGURATION`; posting periods are `runtimeConfiguration`.

| Business object | Tables (key) | Rules |
|---|---|---|
| `/TRFIN/CURRENCY` | `/TRFIN/CURRENCIES` (`CURRENCY`), `/TRFIN/CURRENCY_TEXTS` | `DECIMALS` 0 to 2 |
| `/TRFIN/CHART` | `/TRFIN/CHARTS` (`CHART`), `/TRFIN/CHART_TEXTS` | `KIND` `OPERATIONAL` or `COUNTRY`; `ACCOUNT_LENGTH` 1 to 10, fixed once created; `COUNTRY` for charts of a country only; a chart with accounts, companies or structures stays |
| `/TRFIN/COMPANY` | `/TRFIN/COMPANIES` (`COMPANY`), `/TRFIN/COMPANY_LEDGERS` (`COMPANY`, `LEDGER`) | `NAME`, `COUNTRY`, an existing `CURRENCY`, an operational `CHART` (fixed once created) and `COUNTRY_CHART`, `null` or a chart of the country of the company, which stays while accounts of the company have country accounts; the ledgers besides the primary one |
| `/TRFIN/LEDGER` | `/TRFIN/LEDGERS` (`LEDGER`), `/TRFIN/LEDGER_TEXTS` | exactly one ledger of a tenant is `PRIMARY` once there are ledgers |
| `/TRFIN/LEDGER_GROUP` | `/TRFIN/LEDGER_GROUPS` (`LEDGER_GROUP`), `/TRFIN/LEDGER_GROUP_MEMBERS` (+`LEDGER`), `/TRFIN/LEDGER_GROUP_TEXTS` | the `REPRESENTATIVE` is a member |
| `/TRFIN/DOCUMENT_TYPE` | `/TRFIN/DOCUMENT_TYPES` (`DOCUMENT_TYPE`), `/TRFIN/DOCUMENT_TYPE_TEXTS` | the `INTERVAL` of `/TRFIN/JOURNAL_ENTRY`, at least one of `ALLOW_ACCOUNTS`, `ALLOW_CUSTOMERS`, `ALLOW_SUPPLIERS`, a `REVERSAL_TYPE` (`null` for the type itself), and at most one of `LEDGER` and `LEDGER_GROUP` (`null` for no restriction) |
| `/TRFIN/STATEMENT_STRUCTURE` | `/TRFIN/STATEMENT_STRUCTURES` (`STRUCTURE`), its texts, `/TRFIN/STATEMENT_NODES` (+`NODE`), `/TRFIN/STATEMENT_NODE_TEXTS`, `/TRFIN/STATEMENT_NODE_RANGES` (+`RANGE_NO`) | see below |
| `/TRFIN/POSTING_PERIOD` | `/TRFIN/POSTING_PERIODS` (`COMPANY`, `FISCAL_YEAR`) | the open periods `FROM_PERIOD` to `TO_PERIOD`, 1 to 12 |

**Ledgers.** The primary ledger belongs to every company without assignment;
`/TRFIN/COMPANY_LEDGERS` assigns further ledgers. A posting goes to a ledger, to the members
of a group of ledgers, or without either to the ledgers of its company. All members of a
group must be assigned to the company of a posting; `/TRFIN/LEDGER_SETS` gives the sets:
`OF_COMPANY(company)`, `MEMBERS(group)`, `TARGETS(company, ledger, group)`,
`UNASSIGNED(company, ledgers)` and `UNASSIGNED_MEMBERS(company, group)`.

**Balance sheet structures.** A structure belongs to a chart. Its nodes form a tree by
`PARENT_NODE` within the structure, have a `SIDE` (`ASSETS` or `LIABILITIES`, the side of
their parent), a `SORT_ORDER` and a `KIND`: `NORMAL`, or at most one node each of `RESULT`
and `UNASSIGNED`. Ranges of accounts belong to nodes of the kind `NORMAL`: `FROM_ACCOUNT` to
`TO_ACCOUNT` with the length of the accounts of the chart, and `BALANCE` `ANY`, `DEBIT` or
`CREDIT`, so that the debit and the credit balance of an account can go to different nodes.
Ranges of the same balance side, or of `ANY`, do not overlap. `/TRFIN/BALANCE_SHEET` gives
the node of an account for a balance side (`NODE_OF`), the nodes of an account in all
structures of a chart (`ASSIGNMENTS`) and whether two accounts of a company belong to the
same nodes in the structures of its operational chart and, through their country accounts,
of its country chart (`SAME_NODES`).

**Fiscal calendar.** `/TRFIN/FISCAL_CALENDAR.PERIOD_OF(company, date)` returns the structure
`/TRFIN/FISCAL_PERIOD` with `FISCAL_YEAR` and `PERIOD`; every part of TRFIN takes them from
there. Today the fiscal year is the calendar year; a fiscal year variant of the company will
take its place in this method. `IS_OPEN(company, year, period)` tells whether a period is
open for postings.

**Numbers.** Journal entries take their numbers from the number range object
`/TRFIN/JOURNAL_ENTRY`: 10 digits, the subobject `COMPANY`, intervals per fiscal year,
without buffering, so without gaps. The document type names the interval:

```bash
rt number-range interval add /TRFIN/JOURNAL_ENTRY 01 --tenant ACME --subobject 1000 --year 2026 --from 1 --to 999999999
```

## Master data

| Business object | Tables (key) | Rules |
|---|---|---|
| `/TRFIN/ACCOUNT` | `/TRFIN/ACCOUNTS` (`CHART`, `ACCOUNT`), `/TRFIN/ACCOUNT_TEXTS` (+`LANGUAGE`: `SHORT_TEXT`, `LONG_TEXT`) | `KIND` `BALANCE_SHEET` or `PROFIT_LOSS`, `BLOCKED`; the number has the length of the chart; accounts of country charts are in the same table |
| `/TRFIN/COMPANY_ACCOUNT` | `/TRFIN/COMPANY_ACCOUNTS` (`COMPANY`, `ACCOUNT`) | an account of the operational chart of the company; `COUNTRY_ACCOUNT` `null` or an account of its country chart that no other account of the company has; `RECONCILIATION` `NONE`, `CUSTOMERS` or `SUPPLIERS`; `OPEN_ITEMS`, `AUTOMATIC_ONLY`, `BLOCKED` |
| extension of `/TRBP/PARTNER` | `/TRFIN/CUSTOMER_COMPANIES`, `/TRFIN/SUPPLIER_COMPANIES` (`PARTNER`, `COMPANY`) | `RECONCILIATION_ACCOUNT` of the kind `CUSTOMERS` (customers) or `SUPPLIERS` (suppliers), `ALTERNATIVE_ACCOUNT`, mandatory, of the opposite kind, `BLOCKED`; `ROLE` is always `/TRFIN/CUSTOMER` or `/TRFIN/SUPPLIER` |

- Reconciliation accounts are balance sheet accounts without open items of their own; they
  keep their kind of reconciliation and their account kind while partners use them.
- TRFIN ships the partner roles `/TRFIN/CUSTOMER` and `/TRFIN/SUPPLIER` as objects of
  `/TRBP/PARTNER_ROLE`. Company data needs the role; a role with company data cannot be
  removed. Deleting the partner deletes its company data with it.
- The alternative account takes the opposite balance of a partner: the credit balance of a
  customer, the debit balance of a supplier.
- **Movements.** An account of a chart, an account of a company and the company data of a
  partner with movements cannot be deleted; blocking always works. Their kind and the kind of
  reconciliation stay. The reconciliation account of a partner with movements changes only to
  one with the same nodes for debit and credit balance in the structures of the operational
  chart and of the country chart; the alternative account does not change while a part of the
  balance of the partner is on it in any ledger. `/TRFIN/MOVEMENTS` answers these questions
  from the tables `/TRFIN/ACCOUNT_TOTALS`, `/TRFIN/PARTNER_TOTALS` and
  `/TRFIN/PARTNER_BALANCES`, which postings write.
- Once journal entries are posted, the primary ledger stays; a ledger with movements is not
  deleted and stays assigned to the companies it has movements in; the currency of a company
  with movements stays, and a document type with journal entries is not deleted.

## Journal entries

The business object `/TRFIN/JOURNAL_ENTRY` holds the journal entries of a company:

| Entity | Key | Fields |
|---|---|---|
| `/TRFIN/JOURNAL_ENTRIES` | `COMPANY`, `FISCAL_YEAR`, `DOCUMENT_NO` | `DOCUMENT_TYPE`, `DOCUMENT_DATE`, `POSTING_DATE`, `PERIOD`, `REFERENCE`, `HEADER_TEXT`, `LEDGER` or `LEDGER_GROUP` (`null` for the ledgers of the company), `CURRENCY` of the company, `KIND` `NORMAL`, `CLEARING` or `REVERSAL`, `STATUS` `POSTED` or `REVERSED`, `REVERSAL_YEAR` and `REVERSAL_NO` (the entry that reverses it), `REVERSED_YEAR` and `REVERSED_NO` (the entry it reverses), administrative fields and the ETag `VERSION` |
| `/TRFIN/JOURNAL_ENTRY_ITEMS` | + `ITEM_NO` | `ACCOUNT_KIND` `ACCOUNT`, `CUSTOMER` or `SUPPLIER`, `ACCOUNT` (for partners their reconciliation account), `PARTNER`, `DEBIT_CREDIT` `D` or `C`, `AMOUNT` greater than zero, `ITEM_TEXT`, `ASSIGNMENT`, `DUE_DATE`, `OPEN_ITEM`, the clearing fields `CLEARING_YEAR`, `CLEARING_NO`, `CLEARING_DATE` (the clearing entry and its posting date, `null` while the item is open) and the ETag `VERSION` |
| `/TRFIN/CLEARED_ITEMS` | + `LINE_NO` | `ITEM_YEAR`, `ITEM_DOCUMENT_NO`, `ITEM_NO`: the items a clearing entry cleared; only clearing entries have them |
| `/TRFIN/LEDGER_ENTRIES` | + `LEDGER`, `LINE_NO` | maintained by the save method: `DOCUMENT_TYPE`, `PERIOD`, `POSTING_DATE`, `ITEM_NO` (`null` for reclassifications), `ACCOUNT`, `PARTNER`, `PARTNER_ROLE`, `AMOUNT` (debit positive, credit negative), `LINE_KIND` `ITEM` or `RECLASSIFICATION`, `OPEN_ITEM`, `DUE_DATE` and the clearing fields |

A posted entry changes only in `HEADER_TEXT`, `ITEM_TEXT` and `ASSIGNMENT`, which explain it
and have no effect on balances or reports; everything else is read-only or read-only on
update, entries are never deleted and posted entries get no further items. Fiscal year and
number come from the kernel when the entry is saved: callers leave them out.

**Posting.** Code and services create an entry with its items, over OData by a deep insert
on `JournalEntries`, or with the static action `POST`, which takes the structure
`/TRFIN/POSTING` with the items in `ITEMS` (`ITEM_NO` by default their position,
`ACCOUNT_KIND` by default `ACCOUNT`) and names the new entry by its `LINK`. The save
sequence then runs:

1. Determinations on modify: the period of the posting date from
   `/TRFIN/FISCAL_CALENDAR.PERIOD_OF`, the currency of the company, for an entry without
   ledger and group the ledger or group its document type is restricted to; for customers
   and suppliers the reconciliation account of their company data as `ACCOUNT` and the due
   date, by default the posting date; `OPEN_ITEM` for partners always, for accounts by
   their `OPEN_ITEMS`.
2. Validations, with messages at the entry or the item and the field: debit equals credit,
   at least two items, the period is open, the kind of account is allowed by the document
   type, the account exists in the company and is blocked neither there nor in the chart,
   no direct posting to reconciliation accounts or accounts posted only automatically, the
   partner exists, is not blocked, has the role, valid on the posting date, and company data
   that are not blocked, the reconciliation account is one for the role; ledger or group
   (not both) exist, are assigned to the company and fit the document type (a type
   restricted to a ledger takes only it, one restricted to a group its members or the
   group); amounts are greater than zero with at most the decimals of the currency; the user
   may post the document type in the company (`/TRFIN/JOURNAL_ENTRY`, `CREATE`) and to every
   ledger of the entry (`/TRFIN/LEDGER`, `CREATE`).
3. Late numbering: the fiscal year of the posting date and the next number of the interval
   of the document type in `/TRFIN/JOURNAL_ENTRY` with the company as subobject, in the
   savepoint of the save, so numbers have no gaps per company and year: a failure later in
   the save gives the number back.
4. The kernel writes the entry and its items.
5. The save method `WRITE_LEDGERS` posts every new entry to its ledgers: its ledger, the
   members of its group, or the primary ledger and the ledgers assigned to the company; a
   reversal to the ledgers of the reversed entry. Per ledger it writes a ledger entry per
   item, adds the change of every partner to its running balance in
   `/TRFIN/PARTNER_BALANCES`, writes the reclassification (below), and adds all ledger
   entries to `/TRFIN/ACCOUNT_TOTALS` and the items of partners to `/TRFIN/PARTNER_TOTALS`
   per fiscal year and period. Running balances and totals change with
   `update ... set A = A + x`; a missing row is inserted, and when a parallel posting
   inserted it first, the update runs again. All changes of an entry run in the order of
   their keys, running balances first, so parallel postings wait for each other instead of
   locking each other: PostgreSQL holds the locks of the rows until the commit, SQLite has
   one writer.

The static action `CHECK` takes the same structure, runs the same determinations and
checks without writing anything and returns `/TRFIN/CHECK_RESULT` with the fiscal year, the
period and the messages, each with `ITEM_NO` (`null` for the entry), `FIELD` and `TEXT`.

**Reclassification.** The part of the balance of a partner that is opposite to its usual
side belongs on its alternative account: the credit balance of a customer, the debit balance
of a supplier. With the running balance before (`s0`) and after (`s1`) a posting, the share
is `u(s) = max(0, -s)` for a customer and `u(s) = max(0, s)` for a supplier, and
`d = u(s1) - u(s0)`. When `d` is not zero the posting writes two ledger entries of the kind
`RECLASSIFICATION` with the partner: for a customer `+d` on the reconciliation account and
`-d` on the alternative account, for a supplier `-d` and `+d`. A customer with a debit
balance of 100 who pays 150 has -50: the payment moves 50 from the reconciliation account to
the alternative account, and an invoice of 80 later moves them back. Every entry stays
balanced in every ledger, the items of the entry stay as they are, and the totals of
partners hold only the items.

The reclassification follows the running balance at the time of posting, in the order of
postings, not in the order of posting dates: an entry posted with an earlier posting date
than others reclassifies by the balance after all of them. The amounts on the alternative
accounts are right for the balances of today; a balance sheet for an earlier key date can
differ by entries posted later with earlier dates. A check that compares the reclassified
amounts with the balances by posting date up to a key date comes with the reports.

**Reversal.** The instance action `REVERSE` takes `/TRFIN/REVERSAL` with the posting date,
`null` for that of the reversed entry. It is rejected when an item is cleared or the entry is
reversed already. The reversal has the reversal type of the document type, the items with
debit and credit swapped, the same ledgers, `KIND` `REVERSAL` and `REVERSED_YEAR` and
`REVERSED_NO`; the reversed entry gets `STATUS` `REVERSED` and the number of the reversal in
`REVERSAL_YEAR` and `REVERSAL_NO` when the reversal is numbered. The reversal goes through
the same checks and follows the same reclassification, which cancels that of the reversed
entry when nothing was posted to the partner in between. A clearing entry is reversed only
after its clearing is reset, and one without items is not reversed but reset.

**Clearing.** Items with `OPEN_ITEM` (all items of customers and suppliers, items of
accounts with `OPEN_ITEMS`) stay open until a clearing entry clears them. The static action
`CLEAR` takes `/TRFIN/CLEARING`: company, document type, posting date, optionally document
date (by default the posting date), reference, header text and `LINK`, the open items in
`OPEN_ITEMS` by `FISCAL_YEAR`, `DOCUMENT_NO` and `ITEM_NO`, and new items in `ITEMS` as for
`POST`, empty for none. It answers with `/TRFIN/POSTED_ENTRY` like `POST`. The action rejects
the clearing with error messages and creates nothing unless:

- at least one item is selected, each once; every item exists in the company, is an open
  item, is not cleared and is posted no later than the posting date of the clearing;
- all items are on one account, or on one customer or supplier (kind and partner);
- all items are posted to the same ledgers: the clearing entry posts to exactly those, so
  no item is cleared in one ledger and open in another;
- the selected items and the new items on the same account or partner sum up to zero, debit
  positive and credit negative.

Without new items the clearing entry has `KIND` `CLEARING`, no items and no ledger entries.
With new items it is a posting of them like any other, with the same checks, numbers,
running balances, reclassification and totals, to the ledgers of the cleared items; its new
items on the account or partner of the cleared items are cleared with them. A customer who
pays an invoice of 119 is cleared with the items 119 debit on the bank account and 119
credit on the customer: the credit item and the invoice are cleared. A partial payment is
posted as a normal entry and cleared together with the invoice later. The clearing entry
lists the cleared items of other entries in `/TRFIN/CLEARED_ITEMS`; `LEDGER` and
`LEDGER_GROUP` are copied from the entry of the first item, and the user needs `CREATE` for
the company and document type and for every ledger, as for postings.

The cleared items and their ledger entries get the fiscal year and number of the clearing
entry in `CLEARING_YEAR` and `CLEARING_NO` and its posting date in `CLEARING_DATE`. An item
is open at a key date when it is an open item posted up to the key date and its
`CLEARING_DATE` is `null` or after the key date; the ledger entries carry the same fields, so
reports read open items at any key date from them.

**Parallel clearings.** Of two clearings of the same item only one succeeds. The action
marks the items with the clearing date in the buffer, and the save gives them the number of
the clearing entry when it numbers it; the kernel writes the items with their ETag
`VERSION`, so the second save finds the item changed and fails (PostgreSQL makes it wait for
the first commit; SQLite has one writer). The save method then sets the clearing fields of
the ledger entries only where they are still open
(`update ... where <item> and CLEARING_NO is null`) and compares the number of rows with
the number of ledger entries of the item; fewer fail the save, which rolls back to its
savepoint and gives the number of the clearing entry back.

**Resetting a clearing.** The instance action `RESET_CLEARING` on a clearing entry opens the
items it lists and its own items again, as far as they still carry its number, with their
ledger entries; it needs `CANCEL` for the company and document type of the clearing entry.
A clearing entry with items stays `POSTED` and can be reversed afterwards; one without items
gets `STATUS` `REVERSED`. `/TRFIN/CLEARED_ITEMS` keeps the items the entry once cleared.
Entries whose items are cleared, and clearing entries that clear items, are not reversed:
the clearing is reset first.

## Authorizations and roles

| Authorization object | Fields | Activities | Checks |
|---|---|---|---|
| `/TRFIN/ACCOUNT` | `COMPANY` | `READ`, `CREATE`, `UPDATE`, `DELETE` | accounts of companies |
| `/TRFIN/CHART_ACCOUNT` | `CHART` | `READ`, `CREATE`, `UPDATE`, `DELETE` | accounts of charts |
| `/TRFIN/PARTNER_COMPANY` | `COMPANY`, `ROLE` | `READ`, `CREATE`, `UPDATE`, `DELETE` | company data of customers and suppliers |
| `/TRFIN/LEDGER` | `LEDGER` | `READ`, `CREATE` | reading the ledger entries of a ledger and posting to it |
| `/TRFIN/JOURNAL_ENTRY` | `COMPANY`, `DOCUMENT_TYPE` | `READ`, `CREATE`, `UPDATE`, `CANCEL` | reading, posting (`POST`, `CHECK`, `CLEAR` and deep inserts, which need `UPDATE` for the items as well), changing the texts, reversing journal entries and resetting clearings (`CANCEL`) |

Reading over OData and with `read business object` filters by the same fields. Ledger
entries are read where the user may read the journal entry and the ledger.

| Role | Grants |
|---|---|
| `/TRFIN/MASTER_DATA` | all activities on accounts of charts and companies and on company data of customers and suppliers; reading partners and the configuration |
| `/TRFIN/ACCOUNTANT` | posting, clearing, changing the texts of, reversing and reading journal entries and resetting clearings in all companies and ledgers; reading configuration, master data and partners |
| `/TRFIN/DISPLAY` | reading configuration, master data, partners, ledgers and journal entries |
| `/TRFIN/CONFIGURATION` | maintaining the configuration of TRFIN and TRBP and the intervals of `/TRFIN/JOURNAL_ENTRY` and `/TRBP/PARTNER` |
| `/TRFIN/ODATA_USER` | starting the services of TRFIN below `/rt/odata` |

## Services

`/TRFIN/CONFIGURATION_SERVICE` offers `Currencies`, `Charts`, `Companies` with
`CompanyLedgers`, `Ledgers`, `LedgerGroups` with `LedgerGroupMembers`, `DocumentTypes`,
`StatementStructures` with `StatementNodes` and `StatementNodeRanges`, `PostingPeriods` and
the texts. `/TRFIN/MASTER_DATA_SERVICE` offers `Accounts`, `AccountTexts`,
`CompanyAccounts`, the partners read only as `Partners`, and their company data as
`Customers` and `Suppliers`. `/TRFIN/JOURNAL_ENTRY_SERVICE` offers `JournalEntries` (deep
insert with `JournalEntryItems`, changes of the texts), `JournalEntryItems`, and read only
`LedgerEntries` and `ClearedItems`, with the actions `POST`, `CHECK` and `CLEAR` at the
root of the service and `REVERSE` and `RESET_CLEARING` bound to `JournalEntries`. `POST`
and `CLEAR` answer with the final key of the new entry. Activate them with `rt service activate`.

```bash
curl -u anna -H 'X-Tenant: ACME' -H 'Content-Type: application/json' \
  https://host/rt/odata/trfin/journal_entry_service/POST -d '{
  "COMPANY": "1000", "DOCUMENT_TYPE": "SA", "DOCUMENT_DATE": "2026-03-01", "POSTING_DATE": "2026-03-01",
  "ITEMS": [ { "ACCOUNT": "100000", "DEBIT_CREDIT": "D", "AMOUNT": "100.00" },
             { "ACCOUNT": "400000", "DEBIT_CREDIT": "C", "AMOUNT": "100.00" } ] }'
```

```bash
curl -u anna -H 'X-Tenant: ACME' -H 'Content-Type: application/json' \
  https://host/rt/odata/trfin/journal_entry_service/CLEAR -d '{
  "COMPANY": "1000", "DOCUMENT_TYPE": "DZ", "POSTING_DATE": "2026-03-10",
  "OPEN_ITEMS": [ { "FISCAL_YEAR": 2026, "DOCUMENT_NO": "0001000000", "ITEM_NO": 1 } ],
  "ITEMS": [ { "ACCOUNT": "100000", "DEBIT_CREDIT": "D", "AMOUNT": "119.00" },
             { "ACCOUNT_KIND": "CUSTOMER", "PARTNER": "0000000001", "DEBIT_CREDIT": "C", "AMOUNT": "119.00" } ] }'
```

## Tests

`go test ./kernel -run Ledger` installs TRBP and TRFIN from `packages/` with the test package
`testdata/ledger/TRFINTEST` against SQLite and, with `RT_TEST_POSTGRES_DSN`, PostgreSQL. The
tests of postings cover every check, numbers without gaps, ledgers, groups, totals,
reversals and reclassification; under PostgreSQL eight parallel postings to one customer,
after which `/TRFINTEST/API.CONSISTENCY` compares totals, running balances and
reclassifications with the ledger entries, the clearing of ledger entries with that of their
items, and checks that every clearing sums up to zero per ledger. The tests of clearing
cover every check of `CLEAR`, clearing with and without new items, open items at key dates,
resetting, reversing after a reset and the rejected reversal of cleared entries, the
service, and under PostgreSQL parallel clearings of the same item, of which exactly one
succeeds.
