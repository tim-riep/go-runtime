# TRBPUI: business partners in the browser

TRBPUI brings the business partners of TRBP to the start page of TRFRONTEND. It requires
TRFRONTEND 1.1.0 or later and TRBP 1.0.0 or later and holds only objects of the types of
TRFRONTEND; the pages are the generic list and detail pages.

## UI annotations

| Object | Entity | List | Detail page |
|---|---|---|---|
| `/TRBPUI/PARTNERS_UI` | `/TRBP/PARTNERS` | number, category, names, search term, grouping, blocked; filters by number, names, search term, category, grouping and blocked | `GENERAL` with the groups `MAIN`, `NAME` and `ADMINISTRATION`, `ROLES` with the roles as table |
| `/TRBPUI/PARTNER_ROLES_UI` | `/TRBP/PARTNER_ROLES` | role, valid from, valid to | the role |
| `/TRBPUI/GROUPINGS_UI` | `/TRBP/GROUPINGS` | grouping, interval, external | `GENERAL`, `TEXTS` as table |
| `/TRBPUI/GROUPING_TEXTS_UI` | `/TRBP/GROUPING_TEXTS` | language, label | the text |

Packages that extend the partner add their sections and fields to these, as TRFINUI does with
the company data of customers and suppliers.

## Apps, tiles and spaces

| App | Target | Tiles |
|---|---|---|
| `/TRBPUI/PARTNERS` | `Partners` of `/TRBP/PARTNER_SERVICE` | `/TRBPUI/PARTNERS_TILE`, the metric `/TRBPUI/BLOCKED_PARTNERS_TILE` (`BLOCKED eq true`) |
| `/TRBPUI/GROUPINGS` | `Groupings` of `/TRBP/PARTNER_SERVICE` | `/TRBPUI/GROUPINGS_TILE` |

The app collection `/TRBPUI/PARTNERS_COLLECTION` holds the tiles of partners,
`/TRBPUI/CONFIGURATION_COLLECTION` that of groupings. The space `/TRBPUI/PARTNERS_SPACE`
"Business partners" has the page `/TRBPUI/PARTNERS_PAGE` with both;
`/TRBPUI/CONFIGURATION_SPACE` has the page `/TRBPUI/CONFIGURATION_PAGE` with the groupings
only, which TRFINUI also shows in the configuration of the general ledger.

| Role | App collections | Spaces |
|---|---|---|
| `/TRBP/PARTNER_ADMIN`, `/TRBP/DISPLAY` | partners, configuration | Business partners |
| `/TRBP/CONFIGURATION` | configuration | Business partner configuration |

The apps appear for users who may also start the service, with `/TRBP/ODATA_USER`, and the
shell needs `/TRFRONTEND/USER`.

```bash
make trfrontend
rt install packages/TRFRONTEND/dist/TRFRONTEND packages/TRBP packages/TRBPUI --yes
rt http activate /trui
rt http activate /trfrontend
rt service activate /TRBP/PARTNER_SERVICE
rt role assign paula /TRFRONTEND/USER --tenant ACME
rt role assign paula /TRBP/PARTNER_ADMIN --tenant ACME
rt role assign paula /TRBP/ODATA_USER --tenant ACME
```

## Tests

`go test ./kernel -run FrontendLedgerUI` installs TRFRONTEND, TRBP, TRFIN, TRBPUI and TRFINUI
against SQLite and, with `RT_TEST_POSTGRES_DSN`, PostgreSQL; it checks that the objects install
without messages, that every entity type of the services has the pages in its metadata, the
start pages of the roles and the metric tiles.
