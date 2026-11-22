# TRFINUI: the general ledger in the browser

TRFINUI brings the general ledger of TRFIN to the start page of TRFRONTEND. It requires
TRFRONTEND 1.1.0 or later, TRFIN 1.0.0 or later and TRBPUI, whose partner pages it extends
with the company data of customers and suppliers. This version holds the UI annotations of the
generic list and detail pages and the apps, tiles and spaces; pages of their own for entering
journal entries, clearing and the reports follow as remote apps.

## UI annotations

Every entity of the three services of TRFIN has a list and a detail page, with the dependent
entities of an entity as tables on its detail page:

| Service | Entities (tables on the detail page) |
|---|---|
| `/TRFIN/CONFIGURATION_SERVICE` | currencies (texts), charts of accounts (texts), companies (further ledgers), ledgers (texts), ledger groups (members, texts), document types (texts), balance sheet structures (nodes, texts), nodes (account ranges, texts), posting periods |
| `/TRFIN/MASTER_DATA_SERVICE` | accounts of charts (texts), accounts of companies, the company data of customers and suppliers |
| `/TRFIN/JOURNAL_ENTRY_SERVICE` | journal entries (items, ledger entries, cleared items) with the actions `REVERSE` in the list and `REVERSE` and `RESET_CLEARING` on the detail page; items; ledger entries; cleared items |

`/TRFINUI/PARTNERS_UI` adds the sections `/TRFINUI/CUSTOMER_DATA` and
`/TRFINUI/SUPPLIER_DATA` with the company data as tables to the detail page of the partner of
TRBPUI, in every service that offers the partner: the service of TRBP and the master data of
TRFIN. The extension of TRFIN adds no fields to the partner itself, so there are no group
additions.

## Apps, tiles and spaces

Every app is a list app with a tile of the same name and the suffix `_TILE`:

| Space and role contents | Page sections | Apps |
|---|---|---|
| `/TRFINUI/ACCOUNTING_SPACE` "Accounting": `/TRFIN/ACCOUNTANT`, `/TRFIN/DISPLAY` | journal entries, ledgers | `JOURNAL_ENTRIES`, `JOURNAL_ENTRY_ITEMS`, `LEDGER_ENTRIES`, and the metric tile `/TRFINUI/OPEN_ITEMS_TILE` |
| `/TRFINUI/MASTER_DATA_SPACE` "General ledger master data": `/TRFIN/MASTER_DATA`, `/TRFIN/ACCOUNTANT`, `/TRFIN/DISPLAY` | accounts, customers and suppliers | `ACCOUNTS`, `COMPANY_ACCOUNTS`, `PARTNERS` (the partners of the master data service with their company data), `CUSTOMERS`, `SUPPLIERS` |
| `/TRFINUI/CONFIGURATION_SPACE` "General ledger configuration": `/TRFIN/CONFIGURATION` | currencies and charts of accounts, companies and ledgers, posting; the configuration page of TRBPUI | `CURRENCIES`, `CHARTS`, `STATEMENT_STRUCTURES`, `COMPANIES`, `LEDGERS`, `LEDGER_GROUPS`, `DOCUMENT_TYPES`, `POSTING_PERIODS`, and `/TRBPUI/GROUPINGS` |

The app collections `/TRFINUI/ACCOUNTING_COLLECTION`, `/TRFINUI/MASTER_DATA_COLLECTION` and
`/TRFINUI/CONFIGURATION_COLLECTION` hold the tiles of the spaces; the configuration also gets
`/TRBPUI/CONFIGURATION_COLLECTION`, since `/TRFIN/CONFIGURATION` maintains the groupings of
partners.

**Open items.** The metric tile `/TRFINUI/OPEN_ITEMS_TILE` counts the items of journal entries
with `OPEN_ITEM eq true and CLEARING_NO eq null` that the user may read, and opens the app of
items. It counts items rather than ledger entries, which exist once per ledger of an entry.

Apps appear for users who may start their service: `/TRFIN/ODATA_USER` for the services of
TRFIN, `/TRBP/ODATA_USER` for the groupings. From an empty database with the sample data of
TRFIN:

```bash
export RT_DATABASE=sqlite:ledger.db
rt init --admin admin --tenant ACME
rt language add de
make trfrontend
rt install packages/TRFRONTEND/dist/TRFRONTEND packages/TRBP packages/TRFIN packages/TRBPUI packages/TRFINUI --yes
rt http activate /trui
rt http activate /trfrontend
rt http activate /trfin
for s in /TRBP/PARTNER_SERVICE /TRFIN/CONFIGURATION_SERVICE /TRFIN/MASTER_DATA_SERVICE /TRFIN/JOURNAL_ENTRY_SERVICE; do rt service activate $s; done
for r in /TRFRONTEND/USER /TRFIN/CONFIGURATION /TRFIN/MASTER_DATA /TRFIN/ACCOUNTANT /TRFIN/ODATA_USER /TRBP/PARTNER_ADMIN /TRBP/ODATA_USER; do
  rt role assign admin $r --tenant ACME
done
rt run TRFIN /TRFIN/SAMPLE_DATA SETUP '[]' --tenant ACME --user admin
rt serve --insecure-session-cookie
```

## Tests

`go test ./kernel -run FrontendLedgerUI` installs TRFRONTEND, TRBP, TRFIN, TRBPUI and TRFINUI
against SQLite and, with `RT_TEST_POSTGRES_DSN`, PostgreSQL; it checks the metadata of all
four services, the start pages of the roles with the sample data and that the tile of open
items counts what the service counts.
