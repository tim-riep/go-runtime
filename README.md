# go-runtime

`go-runtime` is a kernel written in Go that manages installable software packages and their
database structures on a single SQLite or PostgreSQL database. Applications embed it as a
library; the `rt` command line tool is a thin layer on top.

A package brings domains (technical types), tables with primary and foreign keys, and
extensions that add fields to tables of other packages. The kernel validates packages,
resolves their dependencies, plans the DDL for the target dialect and executes it in one
transaction together with its own bookkeeping.

## Concepts

- **Package**: a directory with a manifest and schema files. Its ID consists of upper-case
  letters and digits and starts with a letter, e.g. `TRBASE`, `TRFIN`, `TROTTO`.
- **Ownership**: every object belongs to exactly one package. A table created by `TRFIN`
  belongs to `TRFIN`, even if `TROTTO` extends it.
- **Domain**: base type, length, decimals and optional fixed values. Usable by the owning
  package and by every package that requires it directly or transitively.
- **Data element**: a domain with field labels in every language. A field references either a
  domain or a data element; fields without data element have no labels.
- **Table**: fields, a primary key, foreign keys and the mandatory attributes
  `tenantSpecific` and `dataClass`.
- **Extension**: additional fields and foreign keys for a table of a required package. Field
  names receive the namespace of the extending package: `OTTO_ID` in package `TROTTO` becomes
  the column `/TROTTO/OTTO_ID`. Extension fields are never key fields and must be nullable or
  have a default. Several packages can extend the same table without collisions.
- **Repository**: kernel tables that describe all installed objects, their owners and the
  physical layout. The repository, not database introspection, is the basis for every
  comparison. `Check` compares it with the actual schema and reports discrepancies.
- **Tenants**: the kernel table `/RT/TENANTS`. Tenant-specific tables get the field
  `/RT/TENANT` as first key field and a foreign key to the tenant table.
- **Languages**: the kernel table `/RT/LANGUAGES` lists the languages translatable texts may
  use. English always exists.
- **Setting**: tables that together form one configuration, as a tree below a root table
  connected through foreign keys. Other packages can add dependent tables to a setting.

## Package format

```
TRFIN/
  manifest.json
  schema/
    domains/*.json             one domain per file, named <NAME>.json
    data-elements/*.json       one data element per file, named <NAME>.json
    tables/*.json              one table per file, named <NAME>.json
    extensions/*.json          one extension per extended table
    settings/*.json            one setting per file, named <ID>.json
    setting-extensions/*.json  one extension per extended setting, named <ID>.json
```

`manifest.json`:

```json
{
  "id": "TROTTO",
  "version": "1.0.0",
  "description": "Connection of companies to the OTTO marketplace",
  "requires": [{ "id": "TRFIN", "version": ">=1.0.0" }]
}
```

Version constraints are comparisons separated by commas or spaces: `=1.2.3`, `>=1.0.0`,
`<2.0.0`, `^1.2.0`, `~1.2.0`. An omitted constraint accepts every version.

Domain, table and extension (abridged; see [examples](examples)):

```json
{ "name": "COMPANY_STATUS", "type": "CHAR", "length": 10,
  "values": [{ "value": "ACTIVE" }, { "value": "CLOSED" }] }
```

```json
{
  "name": "COMPANIES",
  "tenantSpecific": true,
  "dataClass": "configuration",
  "fields": [
    { "name": "COMPANY_ID", "dataElement": "COMPANY" },
    { "name": "COUNTRY", "dataElement": "COUNTRY", "notNull": true },
    { "name": "STATUS", "domain": "COMPANY_STATUS", "notNull": true, "default": "ACTIVE" }
  ],
  "primaryKey": ["COMPANY_ID"],
  "foreignKeys": [
    { "name": "COUNTRY", "table": "COUNTRIES", "fields": [{ "field": "COUNTRY", "references": "COUNTRY_CODE" }] }
  ]
}
```

```json
{ "table": "COMPANIES", "fields": [{ "name": "OTTO_ID", "domain": "OTTO_ID" }] }
```

Every file is validated against the JSON Schemas in [schemas](schemas). Errors name the
package, the file and the JSON Pointer inside the document.

Data element (`schema/data-elements/COUNTRY.json`). All four labels are required for every
language given; English is mandatory. Lengths in characters: `short` 10, `medium` 20, `long`
40, `heading` 40.

```json
{
  "name": "COUNTRY",
  "domain": "COUNTRY_CODE",
  "texts": {
    "en": { "short": "Country", "medium": "Country", "long": "Country key", "heading": "Country" },
    "de": { "short": "Land", "medium": "Land", "long": "Länderschlüssel", "heading": "Land" }
  }
}
```

Setting (`schema/settings/TRFIN_COMPANIES.json`) and an extension of it in another package
(`schema/setting-extensions/TRFIN_COMPANIES.json`). `foreignKey` names the key of the table to
its parent; labels have at most 60 characters.

```json
{
  "id": "TRFIN_COMPANIES",
  "texts": { "en": { "label": "Companies" }, "de": { "label": "Firmen" } },
  "root": { "table": "COMPANIES", "operations": { "create": true, "update": true, "delete": true } },
  "tables": [
    { "table": "BANK_ACCOUNTS", "parent": "COMPANIES", "foreignKey": "COMPANY",
      "operations": { "create": true, "update": true, "delete": false } }
  ]
}
```

```json
{
  "setting": "TRFIN_COMPANIES",
  "tables": [
    { "table": "OTTO_CONNECTIONS", "parent": "COMPANIES", "foreignKey": "COMPANY",
      "operations": { "create": true, "update": true, "delete": true } }
  ]
}
```

### Base types

| Type | Meaning | PostgreSQL | SQLite (STRICT tables) |
|---|---|---|---|
| CHAR(n) | string with maximum length | VARCHAR(n) | TEXT, length check |
| STRING | string without limit | TEXT | TEXT |
| INT4, INT8 | integers | INTEGER, BIGINT | INTEGER, range check for INT4 |
| DEC(p,s) | exact decimal, p ≤ 18 | NUMERIC(p,s) | INTEGER holding value × 10^s, range check |
| DATE | calendar date | DATE | TEXT `YYYY-MM-DD`, format check |
| TIMESTAMP | point in time in UTC, microseconds | TIMESTAMPTZ | TEXT `YYYY-MM-DDTHH:MM:SS.ffffffZ`, format check |
| BOOL | truth value | BOOLEAN | INTEGER 0/1 |
| UUID | UUID | UUID | TEXT in lower case, format check |
| BLOB | binary data | BYTEA | BLOB |

Fixed values (CHAR, INT4, INT8) become CHECK constraints. Domains are expanded into column
types and checks instead of `CREATE DOMAIN`, so both dialects behave the same.

**DEC on SQLite** is stored as a scaled 64-bit integer (12.34 in `DEC(15,2)` is stored as
1234). It is exact, compares and sorts correctly in SQL, works in foreign keys and sums
correctly. The price is a maximum precision of 18 digits, which applies to both dialects so
packages stay portable. Values in text form would be exact too, but sort incorrectly and need
normalization (`1.0` vs `1.00`).

## Rules

- Table and domain names are unique in the database. Names are always quoted in SQL.
- Identifiers, including namespaces and generated constraint names such as
  `COMPANIES#FK#/TROTTO/MAIN_SHOP`, are limited to 63 bytes in both dialects. Longer names
  are rejected during validation instead of being truncated.
- Constraint names: `<TABLE>#PK`, `<TABLE>#FK#<KEY>`, `<TABLE>#CK#<FIELD>`. `#` cannot occur
  in package names, so constraint names never collide with tables.
- The kernel owns the namespace `/RT/`; the package ID `RT` is reserved.
- A foreign key references the complete primary key of its target, which must belong to the
  same package or a required one. Field types must match (same base type, CHAR not longer
  than the target, DEC with equal precision and scale).
- Kernel domains and the tenant table are available to every package. Other kernel tables
  can neither be referenced nor extended.

### Data classes

| Class | Meaning |
|---|---|
| `configuration` | Defines the behavior of the application; set up once, rarely changed. |
| `application` | Master and transactional data of the running business. |
| `runtimeConfiguration` | Configuration maintained during operation, e.g. exchange rates. |
| `system` | Reserved for the kernel's native tables. |

Extensions inherit the class of their table. Changing a class on upgrade is not destructive
and needs no statements; the plan marks it with `[DATA CLASS]`. Databases created by the first
kernel version get the class `application` for all package tables when they are migrated, as
a provisional value until each package is applied again.

### Languages and texts

Language codes are ISO 639-1 codes in lower case. Texts of data elements and settings are
stored in text tables per language. Installing a package with texts in a language that is not
set up fails with `rterr.MissingLanguages`, which names the missing languages. A language with
texts can only be removed with confirmation, which removes its texts as well.

### Settings

- A setting contains only tables of the classes `configuration` or `runtimeConfiguration`, all
  of the same class and the same tenant flag, which the setting takes over.
- A table belongs to at most one setting. Tables without setting are allowed.
- Tables of the setting belong to its owner or to a required package. A setting always covers
  all fields of its tables, including extension fields of other packages.
- An extending package must require the owner of the setting. It only adds dependent tables,
  anywhere in the tree, also below tables added by packages it requires. It cannot remove
  tables or change operations.
- Value helps are not declared; they are derived from fixed values and foreign keys.
- Settings carry no data, so their changes are never destructive. A change that would make
  any installed setting invalid, also one of another package, is rejected and the error names
  the setting. Uninstalling a package removes its setting extensions.

`kernel.Setting` (and `rt setting show`) returns the resolved definition from package
[setting](setting): the setting merged with all installed extensions as a tree of tables with
parent key, operations, class, tenant flag and origin, and per field the data element with
labels in all languages, domain, type, length, decimals, fixed values, nullability, key,
origin, whether the parent key fills it and the value helps. The tenant field never appears.
`Definition.JSON` serializes it; `FormatVersion` identifies the structure.

### Tenants

For a table with `tenantSpecific: true` the kernel

- adds `/RT/TENANT` as first field and first key field,
- adds a foreign key from it to `/RT/TENANTS`,
- prepends the tenant field to every foreign key to another tenant-specific table, so a row
  can never reference data of another tenant.

Foreign keys from tenant-specific to shared tables run without the tenant field. Foreign keys
from shared to tenant-specific tables are rejected. Extensions inherit the flag of their
table; changing the flag of an existing table is destructive.

The tenant key is `CHAR(16)` of upper-case letters, digits and underscores: short, because it
is part of every key of every tenant-specific table, readable in logs and on the command line,
and stable across databases. A tenant with data cannot be deleted through a plain `DELETE`.
`Tenants().Delete` with confirmation removes its rows from all tenant-specific tables in
dependency order first.

## Operations

All operations share one path: read the repository, compute the target schema, validate it as
a whole, diff, let the dialect plan the statements, and run them in one transaction with the
repository update and a log entry. On error everything is rolled back; the failure is logged
afterwards. With `DryRun` only the plan is returned.

- **Install**: packages given together are installed in dependency order in one transaction.
- **Upgrade**: compares the new version with the repository. Allowed without further options:
  new tables, new fields that are nullable or have a default, longer CHAR, new foreign keys,
  new fixed values, relaxing NOT NULL, changed defaults. Everything that removes or restricts
  (fields, tables, foreign keys, fixed values, shorter or different types, key changes, NOT
  NULL on existing fields, tenant flag) needs `AllowDestructive` and is marked
  `[DESTRUCTIVE]` in the plan. Changes that break another installed package are rejected
  regardless.
- **Uninstall**: only if no installed package requires the package; needs `Confirm`. Removes
  its tables, domains and its extension fields in tables of other packages.
- **Check**: compares repository and database (tables, columns, types, nullability, defaults,
  keys, foreign keys, constraint names) and validates the repository, including all settings.

### Dialect notes

- SQLite: every connection enforces foreign keys. Because `PRAGMA foreign_keys` has no effect
  inside a transaction, the kernel switches it off on a dedicated connection before a schema
  transaction and runs `PRAGMA foreign_key_check` before commit. Changes that `ALTER TABLE`
  cannot express rebuild the table (create, copy, drop, rename), keeping the fields of all
  extending packages.
- PostgreSQL: DDL is transactional, operations are serialized with an advisory lock, foreign
  keys are `DEFERRABLE INITIALLY IMMEDIATE`. The kernel uses the first schema of the
  `search_path`.

## Library

```go
k, err := kernel.Open(ctx, "sqlite:app.db") // or "postgres://user@host/db"
if err != nil { ... }
defer k.Close()

if _, err := k.Init(ctx, kernel.Options{}); err != nil { ... }

base, _ := kernel.LoadDir("examples/TRBASE")
fin, _ := kernel.LoadDir("examples/TRFIN")
p, err := k.Install(ctx, []*model.Package{base, fin}, kernel.Options{})
fmt.Print(p.Text())

_, err = k.Upgrade(ctx, newVersion, kernel.Options{DryRun: true})
var dc *rterr.DestructiveChange
if errors.As(err, &dc) { ... }

tenant, err := k.Tenants().Create(ctx, "ACME", "Acme Corp")
discrepancies, err := k.Check(ctx)
```

All errors are typed (package [rterr](rterr)) and can be inspected with `errors.As`.
Several problems found at once are returned as `rterr.List`. `kernel.LoadFS` reads a package
from any `fs.FS`. `Dialect().Encode` and `Decode` convert Go values to and from the stored
representation.

## Command line

```bash
go install github.com/tim-riep/go-runtime/cmd/rt@latest
```

```bash
export RT_DATABASE=sqlite:app.db
rt init
rt language add de
rt install examples/TRBASE examples/TRFIN examples/TROTTO
rt plan examples/upgrade/TROTTO
rt upgrade examples/upgrade/TROTTO
rt list
rt show COMPANIES
rt check
rt setting list
rt setting show TRFIN_COMPANIES
rt setting show TRFIN_COMPANIES --json
rt show data-element:COUNTRY
rt tenant create ACME --description "Acme Corp"
rt tenant delete ACME --yes
rt uninstall TROTTO --yes
rt log
```

Flags may appear anywhere: `--db`, `--dry-run`, `--allow-destructive`, `--yes`, `--json`,
`--description`, `--name`. Exit codes: 0 success, 2 usage, 3 validation or dependency error,
4 destructive change or confirmation required, 5 check found discrepancies, 6 execution
failed and was rolled back, 1 other errors.

## Development

```bash
go test ./...
```

```bash
RT_TEST_POSTGRES_DSN=postgres://localhost/go_runtime_test go test ./...
```

SQLite tests always run. PostgreSQL tests run when `RT_TEST_POSTGRES_DSN` is set; each test
works in its own schema that is dropped afterwards. The generated DDL per dialect is compared
with golden files in `testdata/golden`; after an intended change, update them with:

```bash
go test ./internal/planner ./setting -update
```

## Not included yet

Executable code in packages, permissions, user interface, views, indexes beyond keys,
data migrations with custom logic, remote package sources, installing from ZIP files (the
loader already works on `fs.FS`), a data access layer that restricts queries to a tenant,
reading and writing data through settings, display properties of fields, translation packages
for objects of other packages, and export or protection of configuration by data class.

## License

Apache License 2.0, see [LICENSE](LICENSE).
