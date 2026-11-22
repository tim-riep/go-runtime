# TRFIN: general ledger

TRFIN is the general ledger of a tenant. This version brings its configuration and master
data: currencies, charts of accounts, companies, ledgers, document types, balance sheet
structures, posting periods, the accounts of charts and companies and the company data of
customers and suppliers; and journal entries, posted to the ledgers with totals, running
balances of customers and suppliers and the reclassification of their opposite balances,
reversed, and their open items cleared; reports of balances, open items and the balance sheet,
a reconciliation of totals and balances with the ledger entries, and sample data. TRFIN requires TRBASE 1.11.0 or later and TRBP
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
differ by entries posted later with earlier dates. The report `RECLASSIFICATION_CHECK`
(below) compares the reclassified amounts with the balances by posting date up to a key date.

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
| `/TRFIN/JOURNAL_ENTRY` | `COMPANY`, `DOCUMENT_TYPE` | `READ`, `CREATE`, `UPDATE`, `CANCEL` | reading, posting (`POST`, `CHECK`, `CLEAR` and deep inserts, which need `UPDATE` for the items as well), changing the texts, reversing journal entries and resetting clearings (`CANCEL`); `READ` starts the action `RECONCILE` |
| `/TRFIN/REPORT` | `COMPANY` | `EXECUTE`, `EXPORT` | running the reports and the reconciliation of a company, exporting reports as workbook |

Reading over OData and with `read business object` filters by the same fields. Ledger
entries are read where the user may read the journal entry and the ledger.

| Role | Grants |
|---|---|
| `/TRFIN/MASTER_DATA` | all activities on accounts of charts and companies and on company data of customers and suppliers; reading partners and the configuration |
| `/TRFIN/ACCOUNTANT` | posting, clearing, changing the texts of, reversing and reading journal entries and resetting clearings in all companies and ledgers; running and exporting the reports and the reconciliation; reading configuration, master data and partners |
| `/TRFIN/DISPLAY` | reading configuration, master data, partners, ledgers and journal entries; running and exporting the reports |
| `/TRFIN/CONFIGURATION` | maintaining the configuration of TRFIN and TRBP and the intervals of `/TRFIN/JOURNAL_ENTRY` and `/TRBP/PARTNER` |
| `/TRFIN/ODATA_USER` | starting the services of TRFIN below `/rt/odata` and the nodes `/trfin/reports` and `/trfin/number-ranges` |

## Services

`/TRFIN/CONFIGURATION_SERVICE` offers `Currencies`, `Charts`, `Companies` with
`CompanyLedgers`, `Ledgers`, `LedgerGroups` with `LedgerGroupMembers`, `DocumentTypes`,
`StatementStructures` with `StatementNodes` and `StatementNodeRanges`, `PostingPeriods` and
the texts. `/TRFIN/MASTER_DATA_SERVICE` offers `Accounts`, `AccountTexts`,
`CompanyAccounts`, the partners read only as `Partners`, and their company data as
`Customers` and `Suppliers`. `/TRFIN/JOURNAL_ENTRY_SERVICE` offers `JournalEntries` (deep
insert with `JournalEntryItems`, changes of the texts), `JournalEntryItems`, and read only
`LedgerEntries` and `ClearedItems`, with the actions `POST`, `CHECK`, `CLEAR` and `RECONCILE` at the
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

## Reports

The class `/TRFIN/REPORTS` holds the reports as static methods that take a selection structure
and return a report structure, for code, `rt run` and the node `/trfin/reports`. Every report
runs for one company and one ledger (`LEDGER` `null` is the primary ledger), needs
`/TRFIN/REPORT` with `EXECUTE` for the company and reads with `authorized` in SQL: totals and
sums with `/TRFIN/REPORT` and `/TRFIN/LEDGER`, single ledger entries (open items, line items)
with `/TRFIN/JOURNAL_ENTRY` and `/TRFIN/LEDGER`, so a user sees nothing of a ledger without
`READ` on it. Amounts are debit positive and credit negative unless said otherwise; periods
`null` are 1 and 12, and `CHART_KIND` `null` is the operational chart.

| Method | Selection | Result |
|---|---|---|
| `BALANCES` | `/TRFIN/BALANCE_SELECTION`: `LEDGER`, `COMPANY`, `FISCAL_YEAR`, `FROM_PERIOD`, `TO_PERIOD`, `FROM_ACCOUNT`, `TO_ACCOUNT`, `CHART_KIND` | per account `CARRY_FORWARD` (balance sheet accounts: the prior years; profit and loss accounts: none), `PRIOR_BALANCE` (the periods of the year before the range), `DEBIT`, `CREDIT` and `PERIOD_BALANCE` of the range and `CUMULATIVE_BALANCE`; `TOTALS`; with the country chart the rows of the country accounts and in `UNASSIGNED` the accounts of the company without country account |
| `PARTNER_BALANCES` | `/TRFIN/PARTNER_SELECTION`: `LEDGER`, `COMPANY`, `FISCAL_YEAR`, periods, `PARTNER_ROLE`, `PARTNER` | the same per role and partner from `/TRFIN/PARTNER_TOTALS`, with the name; partners always carry their balance forward; also filtered by `/TRFIN/PARTNER_COMPANY` with `READ` |
| `OPEN_ITEMS` | `/TRFIN/OPEN_ITEM_SELECTION`: `LEDGER`, `COMPANY`, `KEY_DATE`, `ACCOUNT`, `PARTNER_ROLE`, `PARTNER` | the ledger entries of open items posted up to the key date and not cleared up to it, with `DUE_DATE` (by default the posting date) and `DAYS_OVERDUE` (key date minus due date); `TOTAL`, `NOT_DUE` (due on the key date or later), `OVERDUE` and its buckets `OVERDUE_1_30`, `OVERDUE_31_60`, `OVERDUE_61_90` and `OVERDUE_OVER_90` days |
| `BALANCE_SHEET` | `/TRFIN/STATEMENT_SELECTION`: `LEDGER`, `COMPANY`, `KEY_DATE`, `CHART_KIND`, `STRUCTURE` (`null` for the first structure of the chart) | rows of the nodes and their accounts with `LEVEL`, `NODE`, `PARENT_NODE`, `NODE_KIND`, `SIDE`, `ACCOUNT` (`null` for nodes), `LABEL` and `AMOUNT`; `TOTAL_ASSETS`, `TOTAL_LIABILITIES`, `RESULT`, `DIFFERENCE`, `BALANCED`, `UNASSIGNED` and `PROBLEMS` |
| `LINE_ITEMS` | `/TRFIN/LINE_ITEM_SELECTION`: `LEDGER`, `COMPANY`, `FISCAL_YEAR`, periods, `CHART_KIND`, `ACCOUNT`, `PARTNER_ROLE`, `PARTNER` | the ledger entries with the key of their journal entry (`COMPANY`, `FISCAL_YEAR`, `DOCUMENT_NO`), `LINE_NO`, `ITEM_NO`, document type, dates, amount, kind, open item and clearing; `TOTAL`; with the country chart `ACCOUNT` is a country account and the entries of its accounts are listed |
| `RECLASSIFICATION_CHECK` | `/TRFIN/RECLASSIFICATION_SELECTION`: `LEDGER`, `COMPANY`, `KEY_DATE` | the partners whose reclassification lines on alternative accounts up to the key date (`POSTED`) differ from the part of their balance by posting date up to the key date that belongs there (`EXPECTED`), with `BALANCE` and `DIFFERENCE` |

**Balance sheet.** The balance of an account at the key date is the sum of its totals of the
prior years and of the periods of the year before the period of the key date, plus its ledger
entries of that period posted up to the key date. Balance sheet accounts go to the node whose
range takes them for their balance side (`/TRFIN/BALANCE_SHEET.NODE_OF`); with the country
chart the accounts of the company go to their country accounts first. The profit and loss
accounts of all years up to the key date go to the node of the kind `RESULT`, since no program
carries their balance forward to equity yet; `RESULT` is the profit, positive. Nodes add up
their accounts and the nodes below them; a node on the assets side shows debit balances as
positive amounts, one on the liabilities side credit balances. Accounts without node, and with
the country chart accounts without country account, are listed in `UNASSIGNED` and shown
under the node of the kind `UNASSIGNED`. Assets equal liabilities with the result when every
ledger is balanced and everything has a node; a structure without a node for the result or
for unassigned accounts gives `PROBLEMS` and `BALANCED` `false` with the `DIFFERENCE`.

**Reclassification check.** The reclassification follows the running balance in the order of
postings. `RECLASSIFICATION_CHECK` recomputes the balance of every partner by posting date up
to the key date and the part of it on the alternative account (the credit balance of a
customer, the debit balance of a supplier) and compares it with the sum of the
reclassification lines on accounts of the opposite kind of reconciliation posted up to the key
date. An invoice posted back before a payment that made the balance of its customer a credit
balance moves the reclassification back on its own earlier date: on key dates between the two
the report lists the customer.

### The node /trfin/reports

`POST /trfin/reports/<report>` with the selection as JSON answers with the report as JSON; the
reports are `balances`, `partner-balances`, `open-items`, `balance-sheet`, `line-items`,
`reclassification-check` and `reconciliation` (selection `/TRFIN/RECONCILIATION_SELECTION`
with `COMPANY` and `FISCAL_YEAR`). `?format=xlsx` answers with the rows of the report
(`ROWS`, for the reconciliation `PROBLEMS`) as a workbook from `/TRBASE/XLSX`, with the
headings of the data elements in the language of the request, as a download; it needs
`/TRFIN/REPORT` with `EXPORT` for the company. The node needs `/TRFIN/REPORT` with `EXECUTE`
for any company and `/TRBASE/HTTP_SERVICE` for `/trfin/reports`, which `/TRFIN/ODATA_USER`
grants; other methods than `POST` are answered with 405, unknown reports with 404, a
selection that does not fit or cannot be run with 400 and `{"error": "..."}`, missing
authorizations with 403.

```bash
rt http activate /trfin
curl -u anna -H 'X-Tenant: ACME' -H 'Content-Type: application/json' \
  https://host/trfin/reports/balance-sheet -d '{"COMPANY": "1000", "KEY_DATE": "2026-12-31"}'
curl -u anna -H 'X-Tenant: ACME' -H 'Content-Type: application/json' -o balances.xlsx \
  'https://host/trfin/reports/balances?format=xlsx' -d '{"COMPANY": "1000", "FISCAL_YEAR": 2026}'
```

### The node /trfin/number-ranges

The node `/trfin/number-ranges` maintains the intervals of the number range objects of the
general ledger, `/TRFIN/JOURNAL_ENTRY` (per company and fiscal year) and `/TRBP/PARTNER`, with
`/TRBASE/NUMBER_RANGE_ADMINISTRATION`, for the remote app of number ranges of TRFINUI and
other clients. Intervals are `/TRBASE/NUMBER_INTERVAL` as JSON.

| Request | Answer |
|---|---|
| `GET /trfin/number-ranges` | the objects of TRFIN and TRBP the user may read, with length, subobject and whether they are per fiscal year |
| `GET /trfin/number-ranges/intervals?object=<object>` | the intervals of the object |
| `POST`, `PUT`, `DELETE /trfin/number-ranges/intervals` with an interval | adds, changes or deletes the interval and answers with the intervals of its object |
| `GET /trfin/number-ranges/history?object=<object>` | the changes of the intervals of the object |

A change that moves the current number back, or deletes an interval that gave numbers, is
answered with 409 until it is sent again with `?confirm=true`. Every request needs
`/TRBASE/NUMBER_RANGE` for the object (the node itself `READ` for any object, which
`/TRFIN/CONFIGURATION` grants) and `/TRBASE/HTTP_SERVICE` for `/trfin/number-ranges`, which
`/TRFIN/ODATA_USER` grants. Other objects are answered with 403, unknown objects and intervals
with 404, bodies that are no interval and invalid intervals with 400, other methods with 405,
always with `{"error": "..."}`.

```bash
curl -u conny -H 'X-Tenant: ACME' 'https://host/trfin/number-ranges/intervals?object=/TRFIN/JOURNAL_ENTRY'
curl -u conny -H 'X-Tenant: ACME' -H 'Content-Type: application/json' https://host/trfin/number-ranges/intervals -d '{
  "RANGE_NAME": "/TRFIN/JOURNAL_ENTRY", "INTERVAL_ID": "01", "SUBOBJECT": "1000", "FISCAL_YEAR": 2027,
  "FROM_NO": "1", "TO_NO": "999999", "CURRENT_NO": null, "EXTERNAL": false }'
```

## Reconciliation

`/TRFIN/LEDGER_RECONCILIATION.RUN(company, year)` returns `/TRFIN/RECONCILIATION_RESULT` with
`CONSISTENT` and the `PROBLEMS` it finds in the ledgers the user may read: totals of accounts
of the year that differ from the sums of the ledger entries per ledger, account and period, or
are missing; totals of partners of the year that differ from the sums of the items per ledger,
role, partner and period; running balances that differ from the sums of the items of all
years; parts of balances on alternative accounts that differ from the reclassification lines
there; entries of the year that are not balanced in a ledger; ledger entries that are not
cleared as their items; and clearings of the year that do not sum up to zero per ledger. It
needs `/TRFIN/REPORT` with `EXECUTE` for the company. The static action `RECONCILE` of
`/TRFIN/JOURNAL_ENTRY` (in `/TRFIN/JOURNAL_ENTRY_SERVICE`) runs it with
`/TRFIN/RECONCILIATION_SELECTION` and answers with an error message when the company does not
exist or the user may not reconcile it. The class is not called `/TRFIN/RECONCILIATION`
because the domain of the kind of reconciliation account has that name.

```bash
rt run TRFIN /TRFIN/LEDGER_RECONCILIATION RUN '["1000", 2026]' --tenant ACME --user anna
```

## Sample data

`/TRFIN/SAMPLE_DATA.SETUP()` sets up a tenant without currencies, charts, ledgers and
companies of TRFIN, and is rejected with `/TRFIN/SAMPLE_DATA_ERROR` in any other; a step that
fails raises the same exception with its result, and the steps before it stay saved. It
creates:

- the currency `EUR`; the operational chart `TRAC` with 40 accounts of six digits (fixed assets
  `0…`, current assets `10…`–`15…` with `140000` receivables, `141000` supplier debit
  balances, payables `160000` and `161000` customer credit balances, other liabilities
  `17…`–`19…`, equity and provisions `3…`, revenue `4…`, expenses `6…`–`7…`) and the country
  chart `TRDE` for Germany with 19 accounts of four digits; each chart has a balance sheet
  structure (`BSOP`, `BSDE`) with assets, equity and liabilities, a node for the result and one
  for unassigned accounts;
- the ledgers `0L` (primary) and `2L` and the group `ALL`; the companies `1000` (DE, with the
  country chart and the ledger `2L`; 19 of its accounts have a country account) and `2000`
  (AT, only the primary ledger);
- the document types `SA` (accounts), `DR` and `DZ` (customer invoices and payments), `KR`
  and `KZ` (supplier invoices and payments) and `AB` (clearing), the open periods and the
  intervals `01` to `04` of `/TRFIN/JOURNAL_ENTRY` for 2025 to 2027 in both companies;
- the grouping `0001` with the interval `01` of `/TRBP/PARTNER` when they do not exist, two
  customers and two suppliers with company data in `1000`;
- 17 journal entries in 2025 and 2026, among them a clearing of an invoice with its payment
  without posting and a payment that clears an invoice.

The user needs `/TRFIN/CONFIGURATION`, `/TRFIN/MASTER_DATA`, `/TRFIN/ACCOUNTANT` and
`/TRBP/PARTNER_ADMIN`, or all authorizations. From an empty database:

```bash
export RT_DATABASE=sqlite:ledger.db
rt init --admin admin --tenant ACME
rt language add de
rt install packages/TRBP packages/TRFIN --yes
rt run TRFIN /TRFIN/SAMPLE_DATA SETUP '[]' --tenant ACME --user admin
rt run TRFIN /TRFIN/REPORTS BALANCES '[{"COMPANY": "1000", "FISCAL_YEAR": 2026}]' --tenant ACME --user admin
rt run TRFIN /TRFIN/REPORTS BALANCE_SHEET '[{"COMPANY": "1000", "KEY_DATE": "2026-12-31", "CHART_KIND": "COUNTRY"}]' --tenant ACME --user admin
rt run TRFIN /TRFIN/REPORTS OPEN_ITEMS '[{"COMPANY": "1000", "KEY_DATE": "2026-12-31"}]' --tenant ACME --user admin
rt run TRFIN /TRFIN/LEDGER_RECONCILIATION RUN '["1000", 2026]' --tenant ACME --user admin
```

The balance sheet of `1000` at 2026-12-31 has assets and liabilities of 76,300.00 with a
result of 23,300.00 from both years.

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
succeeds. The tests of reports cover balances with carry-forward over two years and by the
country chart, balances of partners, open items before and after a clearing with their days
overdue, the balance sheet at a key date within a period with its result and unassigned
accounts and without the nodes for them, the reclassification check after a back-dated
posting, the reconciliation of clean and of manipulated totals, the node with JSON and
workbooks, authorizations by company, ledger and export, and the sample data, whose balance
sheets balance. The test of the node `/trfin/number-ranges` adds, changes and deletes
intervals, moves a current number back with and without confirmation, reads the history and
checks the answers to other objects, wrong bodies and methods and users without
authorization.
