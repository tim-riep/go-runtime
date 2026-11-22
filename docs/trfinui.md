# TRFINUI: the general ledger in the browser

TRFINUI brings the general ledger of TRFIN to the start page of TRFRONTEND. It requires
TRFRONTEND 1.1.0 or later, TRFIN 1.0.0 or later and TRBPUI, whose partner pages it extends
with the company data of customers and suppliers. It holds the UI annotations of the generic
list and detail pages, remote apps of its own for posting, clearing, the reports and the
number ranges, and the apps, tiles and spaces that offer both.

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

Every app has a tile of the same name and the suffix `_TILE`; the list apps open the generic
pages of TRFRONTEND, the remote apps (below) the pages of TRFINUI:

| Space and role contents | Page sections | Apps |
|---|---|---|
| `/TRFINUI/POSTING_SPACE` "Posting": `/TRFIN/ACCOUNTANT` | journal entries and clearing | the remote apps `POST_JOURNAL_ENTRY` and `CLEAR_OPEN_ITEMS` |
| `/TRFINUI/ACCOUNTING_SPACE` "Accounting": `/TRFIN/ACCOUNTANT`, `/TRFIN/DISPLAY` | journal entries, reports, ledgers | `JOURNAL_ENTRIES`, `JOURNAL_ENTRY_ITEMS`, `LEDGER_ENTRIES`, the metric tile `/TRFINUI/OPEN_ITEMS_TILE`, and the remote apps `BALANCE_SHEET`, `ACCOUNT_BALANCES`, `PARTNER_BALANCES`, `OPEN_ITEMS_REPORT` and `LINE_ITEMS` |
| `/TRFINUI/MASTER_DATA_SPACE` "General ledger master data": `/TRFIN/MASTER_DATA`, `/TRFIN/ACCOUNTANT`, `/TRFIN/DISPLAY` | accounts, customers and suppliers | `ACCOUNTS`, `COMPANY_ACCOUNTS` and the app `/TRBPUI/PARTNERS` of TRBPUI: customers and suppliers are business partners with the role `/TRFIN/CUSTOMER` or `/TRFIN/SUPPLIER`, whose roles and company data are maintained on the page of the partner with the sections `/TRFINUI/CUSTOMER_DATA` and `/TRFINUI/SUPPLIER_DATA`; there are no apps of their own for them |
| `/TRFINUI/CONFIGURATION_SPACE` "General ledger configuration": `/TRFIN/CONFIGURATION` | currencies and charts of accounts, companies and ledgers, posting; the configuration page of TRBPUI | `CURRENCIES`, `CHARTS`, `STATEMENT_STRUCTURES`, `COMPANIES`, `LEDGERS`, `LEDGER_GROUPS`, `DOCUMENT_TYPES`, `POSTING_PERIODS`, the remote app `NUMBER_RANGES`, and `/TRBPUI/GROUPINGS` |

The app collections `/TRFINUI/POSTING_COLLECTION` (only for `/TRFIN/ACCOUNTANT`),
`/TRFINUI/ACCOUNTING_COLLECTION`, `/TRFINUI/MASTER_DATA_COLLECTION` and
`/TRFINUI/CONFIGURATION_COLLECTION` hold the tiles of the spaces; the configuration also gets
`/TRBPUI/CONFIGURATION_COLLECTION`, since `/TRFIN/CONFIGURATION` maintains the groupings of
partners. Posting has a space of its own, so that display users see the same accounting page
without tiles they could not use.

**Open items.** The metric tile `/TRFINUI/OPEN_ITEMS_TILE` counts the items of journal entries
with `OPEN_ITEM eq true and CLEARING_NO eq null` that the user may read, and opens the app of
items. It counts items rather than ledger entries, which exist once per ledger of an entry.

Apps appear for users who may start their service or node: `/TRFIN/ODATA_USER` for the
services of TRFIN, `/TRBP/ODATA_USER` for the groupings and `/TRFINUI/USER` for the remote
apps below `/trfinui`. What the apps then read and change follows the roles of TRFIN.

## Remote apps

The remote apps are React pages in `packages/TRFINUI/web`, built with Vite and
`@module-federation/vite` into a Module Federation remote: `mf-manifest.json`,
`remoteEntry.js` and their chunks. The shell of TRFRONTEND loads them from the node
`/trfinui`, which serves the asset repository `web` of TRFINUI with `/TRBASE/ASSET_HANDLER`;
every app of the kind `REMOTE` names the node, the path `mf-manifest.json` and its module.
React and TRUI come from the shell as singletons, so the build contains neither, and the pages
use the context of TRFRONTEND 1.1.0: the OData services of TRFIN, `fetchJSON` for the nodes
`/trfin/reports` and `/trfin/number-ranges`, `download` for exports, `navigate` to other apps
and to the object page of a journal entry, the language and toasts. Texts are English and
German; amounts are exact decimal strings throughout, and `null` stays apart from `""`.

| App and module | Page |
|---|---|
| `POST_JOURNAL_ENTRY`, `./JournalEntryPage` | the header (company, document type, ledger, dates, reference, text) and the items in an editable table: kind (account, customer, supplier), account or partner with a value help of the company, debit or credit, the amount in the currency of the company, text and due date, with the running balance after every item and the difference until debit equals credit. Missing values are named before anything is sent. `Check` runs `CHECK` and shows its messages at the cells and fields they are about and in a message list; `Post` runs `POST`, and when it fails, `CHECK` places the messages the same way. A posted entry opens on its object page. |
| `CLEAR_OPEN_ITEMS`, `./ClearingPage` | the open items of an account, a customer or a supplier at the posting date to select, new items such as a payment (one button adds the items for the difference), the difference to clear, which must be zero, and `CLEAR` |
| `BALANCE_SHEET`, `./BalanceSheetPage` | the balance sheet at a key date by ledger, chart and structure as indented table: nodes expand and collapse one by one or all, the totals, the result, the difference and the problems of the structure; an account opens its line items of the year of the key date |
| `ACCOUNT_BALANCES`, `./BalancesPage` | the balances of the accounts by fiscal year, periods, range of accounts and chart with totals and the accounts without country account; a row opens its line items |
| `PARTNER_BALANCES`, `./PartnerBalancesPage` | the balances of customers and suppliers with totals; a row opens the line items of the partner |
| `OPEN_ITEMS_REPORT`, `./OpenItemsPage` | the open items at a key date by account or partner, with the totals by due date; a row opens its journal entry |
| `LINE_ITEMS`, `./LineItemsPage` | the ledger entries of an account or partner, where the reports drill down to; a row opens its journal entry |
| `NUMBER_RANGES`, `./NumberRangePage` | the intervals of `/TRFIN/JOURNAL_ENTRY` and `/TRBP/PARTNER` through `/trfin/number-ranges`: add, change and delete with a confirmation where the node asks for one, and the history |

Every report page runs with POST on `/trfin/reports/<report>` and exports the selection of
the shown result with `?format=xlsx` as a download. Parameters of the app, such as `COMPANY`,
`FISCAL_YEAR`, `KEY_DATE`, `ACCOUNT` or `PARTNER`, fill the selection; with a company the
report runs when the page opens, which is how the drill-down hands over its selection.

`make trfinui` runs `npm ci` and `npm run build` in `packages/TRFINUI/web`: Vite builds into
`web/dist-web`, and `scripts/build-package.mjs` assembles the installable package in
`packages/TRFINUI/dist/TRFINUI` with the build in `assets/web`, as `make trfrontend` does for
TRFRONTEND. Neither the build nor the package is checked in; `packages/TRFINUI` itself holds
only the empty repository `web` with its descriptor and cannot be installed as a directory,
since the sources of the user interface lie beside the package (`rt install` takes the package
from `dist`).

From an empty database with the sample data of TRFIN:

```bash
export RT_DATABASE=sqlite:ledger.db
rt init --admin admin --tenant ACME
rt language add de
make trfrontend trfinui
rt install packages/TRFRONTEND/dist/TRFRONTEND packages/TRBP packages/TRFIN packages/TRBPUI packages/TRFINUI/dist/TRFINUI --yes
for n in /trui /trfrontend /trfin /trfinui; do rt http activate $n; done
for s in /TRBP/PARTNER_SERVICE /TRFIN/CONFIGURATION_SERVICE /TRFIN/MASTER_DATA_SERVICE /TRFIN/JOURNAL_ENTRY_SERVICE; do rt service activate $s; done
for r in /TRFRONTEND/USER /TRFIN/CONFIGURATION /TRFIN/MASTER_DATA /TRFIN/ACCOUNTANT /TRFIN/ODATA_USER /TRBP/PARTNER_ADMIN /TRBP/ODATA_USER /TRFINUI/USER; do
  rt role assign admin $r --tenant ACME
done
rt run TRFIN /TRFIN/SAMPLE_DATA SETUP '[]' --tenant ACME --user admin
rt serve --insecure-session-cookie
```

Then open `http://localhost:8080/trfrontend/`, sign in as `admin` and open "Balance sheet" in
"Accounting" with company `1000` and key date 2026-12-31: assets and liabilities of 76,300.00
with a result of 23,300.00.

## Tests

`go test ./kernel -run FrontendLedgerUI` installs TRFRONTEND, TRBP, TRFIN, TRBPUI and TRFINUI
against SQLite and, with `RT_TEST_POSTGRES_DSN`, PostgreSQL; it checks the metadata of all
four services, the start pages of the roles with the sample data (remote apps only with
`/TRFINUI/USER`, posting only for accountants, the target of a remote app) and that the tile
of open items counts what the service counts.

The remote apps have tests of their own in `packages/TRFINUI/web`, which need Node 22.
`npm test` runs the unit tests (running balances, items of postings, messages per item and
field, the difference of clearings, the rows of the balance sheet as tree, value helps,
parameters) and component tests in Chromium, Firefox and WebKit with a context that answers
from memory (the journal entry with its messages at the cells, the balance sheet with expand,
collapse, export and drill-down, the confirmation of number ranges).
`TRUI_PACKAGE=<TRUI directory or ZIP> npm run test:e2e` needs `make trfrontend trfinui`; it
builds `rt`, installs TRUI, TRFRONTEND, TRBP, TRFIN, TRBPUI and TRFINUI with the sample data
in a new SQLite database, starts `rt serve` and drives the shell in the three browsers with
checks of axe: it enters and posts a journal entry and opens it, shows the error of a wrong
account at its cell without posting, clears an invoice of a customer with a payment, and
shows the balance sheet, collapses and expands it, exports it and drills down from an account
over its line items to a journal entry. `TRFINUI_BROWSERS=chromium` limits both to one
browser.
