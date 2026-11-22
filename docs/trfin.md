# TRFIN: general ledger

TRFIN is the general ledger of a tenant. This version brings its configuration and master
data: currencies, charts of accounts, companies, ledgers, document types, balance sheet
structures, posting periods, the accounts of charts and companies and the company data of
customers and suppliers. Journal entries, totals, clearing and reports follow. TRFIN
requires TRBASE 1.11.0 or later and TRBP 1.0.0 or later; all its tables hold data of a
tenant, and amounts are `DEC(18,2)`.

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
  `/TRFIN/PARTNER_BALANCES`, which postings will write.

## Authorizations and roles

| Authorization object | Fields | Activities | Checks |
|---|---|---|---|
| `/TRFIN/ACCOUNT` | `COMPANY` | `READ`, `CREATE`, `UPDATE`, `DELETE` | accounts of companies |
| `/TRFIN/CHART_ACCOUNT` | `CHART` | `READ`, `CREATE`, `UPDATE`, `DELETE` | accounts of charts |
| `/TRFIN/PARTNER_COMPANY` | `COMPANY`, `ROLE` | `READ`, `CREATE`, `UPDATE`, `DELETE` | company data of customers and suppliers |
| `/TRFIN/LEDGER` | `LEDGER` | `READ`, `CREATE` | reading and posting to a ledger, with postings |

Reading over OData and with `read business object` filters by the same fields.

| Role | Grants |
|---|---|
| `/TRFIN/MASTER_DATA` | all activities on accounts of charts and companies and on company data of customers and suppliers; reading partners and the configuration |
| `/TRFIN/DISPLAY` | reading configuration, master data, partners and ledgers |
| `/TRFIN/CONFIGURATION` | maintaining the configuration of TRFIN and TRBP and the intervals of `/TRFIN/JOURNAL_ENTRY` and `/TRBP/PARTNER` |
| `/TRFIN/ODATA_USER` | starting the services of TRFIN below `/rt/odata` |

## Services

`/TRFIN/CONFIGURATION_SERVICE` offers `Currencies`, `Charts`, `Companies` with
`CompanyLedgers`, `Ledgers`, `LedgerGroups` with `LedgerGroupMembers`, `DocumentTypes`,
`StatementStructures` with `StatementNodes` and `StatementNodeRanges`, `PostingPeriods` and
the texts. `/TRFIN/MASTER_DATA_SERVICE` offers `Accounts`, `AccountTexts`,
`CompanyAccounts`, the partners read only as `Partners`, and their company data as
`Customers` and `Suppliers`. Activate them with `rt service activate`.

## Tests

`go test ./kernel -run Ledger` installs TRBP and TRFIN from `packages/` with the test package
`testdata/ledger/TRFINTEST` against SQLite and, with `RT_TEST_POSTGRES_DSN`, PostgreSQL.
