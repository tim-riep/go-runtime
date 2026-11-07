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
- **Ownership and namespace**: every object belongs to exactly one package, and its name
  starts with the namespace of that package: `/TRFIN/COMPANIES` belongs to `TRFIN`, even if
  `TROTTO` extends it. The full name is used everywhere, in package files for definitions and
  references alike.
- **Domain**: base type, length, decimals and optional fixed values. Usable by the owning
  package and by every package that requires it directly or transitively.
- **Data element**: a domain with field labels in every language. A field references either a
  domain or a data element; fields without data element have no labels.
- **Table**: fields, a primary key, foreign keys and the mandatory attributes
  `tenantSpecific` and `dataClass`.
- **Extension**: additional fields and foreign keys for a table of a required package. Their
  names carry the namespace of the extending package, e.g. the column `/TROTTO/OTTO_ID` in
  `/TRFIN/COMPANIES`. Extension fields are never key fields and must be nullable or have a
  default. Several packages can extend the same table without collisions. Fields and foreign
  keys declared by the table itself have no namespace.
- **Repository**: kernel tables that describe all installed objects, their owners and the
  physical layout. The repository, not database introspection, is the basis for every
  comparison. `Check` compares it with the actual schema and reports discrepancies.
- **Tenants**: the kernel table `/RT/TENANTS`. Tenant-specific tables get the field
  `/RT/TENANT` as first key field and a foreign key to the tenant table.
- **Languages**: the kernel table `/RT/LANGUAGES` lists the languages translatable texts may
  use. English always exists.
- **Asset repository**: a folder below `assets/` of a package with files (assets) and
  subfolders, such as images, stylesheets, fonts or print templates. It is identified by the
  package ID and its folder name, installed and removed with the package and read-only at
  runtime.
- **Setting**: tables that together form one configuration, as a tree below a root table
  connected through foreign keys. Other packages can add dependent tables to a setting.
- **Structure**: named components without database table. A component has a data element, a
  domain, a structure or an array type. A structure is flat when all its components are
  elementary (data element or domain), otherwise deep. Every table is a flat structure as
  well: its fields without tenant field, keys and foreign keys.
- **Array type**: rows of a structure, table, data element or domain, optionally with a key.
- **Include**: an entry in the components of a structure or the fields of a table whose
  place is taken by all components of an included structure or table.
- **Structure extension**: additional components for a structure of a required package. They
  appear wherever the structure is used, including every table that includes it.

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
    structures/*.json          one structure per file, named <NAME>.json
    array-types/*.json         one array type per file, named <NAME>.json
    structure-extensions/*.json  one extension per extended structure
  src/
    <NAME>.class               a class, exception class or test class
    <NAME>.interface           an interface
    <NAME>.include             a members or statements include
  assets/
    <repository>/asset.json    every folder below assets/ is an asset repository
    <repository>/...           its files and folders
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

A package with source files in `src/` states the version of the language in the manifest:
`"languageVersion": 1`. Grammar and canonical form are defined per language version.

Version constraints are comparisons separated by commas or spaces: `=1.2.3`, `>=1.0.0`,
`<2.0.0`, `^1.2.0`, `~1.2.0`. An omitted constraint accepts every version.

Domain, table and extension (abridged; see [examples](examples)):

```json
{ "name": "/TRFIN/COMPANY_STATUS", "type": "CHAR", "length": 10,
  "values": [{ "value": "ACTIVE" }, { "value": "c", "name": "CLOSED" }] }
```

A fixed value is addressed in code by its `name`, e.g. `/TRFIN/COMPANY_STATUS.CLOSED`.
Without a name, the value itself is the name if it consists of upper-case letters, digits
and underscores and starts with a letter. Names are unique within a domain.

```json
{
  "name": "/TRFIN/COMPANIES",
  "tenantSpecific": true,
  "dataClass": "configuration",
  "fields": [
    { "name": "COMPANY_ID", "dataElement": "/TRFIN/COMPANY" },
    { "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY", "notNull": true },
    { "name": "STATUS", "domain": "/TRFIN/COMPANY_STATUS", "notNull": true, "default": "ACTIVE" },
    { "name": "NOTE", "domain": "/TRBASE/TEXT", "default": null }
  ],
  "primaryKey": ["COMPANY_ID"],
  "foreignKeys": [
    { "name": "COUNTRY", "table": "/TRFIN/COUNTRIES", "fields": [{ "field": "COUNTRY", "references": "COUNTRY_CODE" }] }
  ]
}
```

```json
{ "table": "/TRFIN/COMPANIES", "fields": [{ "name": "/TROTTO/OTTO_ID", "dataElement": "/TROTTO/OTTO_PARTNER" }] }
```

Every file is validated against the JSON Schemas in [schemas](schemas). Errors name the
package, the file and the JSON Pointer inside the document.

Data element (`schema/data-elements/COUNTRY.json`). All four labels are required for every
language given; English is mandatory. Lengths in characters: `short` 10, `medium` 20, `long`
40, `heading` 40.

```json
{
  "name": "/TRBASE/COUNTRY",
  "domain": "/TRBASE/COUNTRY_CODE",
  "texts": {
    "en": { "short": "Country", "medium": "Country", "long": "Country key", "heading": "Country" },
    "de": { "short": "Land", "medium": "Land", "long": "Länderschlüssel", "heading": "Land" }
  }
}
```

Setting (`schema/settings/COMPANIES.json`) and an extension of it in another package
(`schema/setting-extensions/COMPANIES.json`). `foreignKey` names the key of the table to
its parent; labels have at most 60 characters.

```json
{
  "id": "/TRFIN/COMPANIES",
  "texts": { "en": { "label": "Companies" }, "de": { "label": "Firmen" } },
  "root": { "table": "/TRFIN/COMPANIES", "operations": { "create": true, "update": true, "delete": true } },
  "tables": [
    { "table": "/TRFIN/BANK_ACCOUNTS", "parent": "/TRFIN/COMPANIES", "foreignKey": "COMPANY",
      "operations": { "create": true, "update": true, "delete": false } }
  ]
}
```

```json
{
  "setting": "/TRFIN/COMPANIES",
  "tables": [
    { "table": "/TROTTO/OTTO_CONNECTIONS", "parent": "/TRFIN/COMPANIES", "foreignKey": "COMPANY",
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

Structure (`schema/structures/ADDRESS.json`), a table including it, an array type and a
structure extension in another package. Every component has exactly one of `dataElement`,
`domain`, `structure` (a structure or a table) and `arrayType`; elementary components may
have `notNull`, `default` and `description`. An include is an entry `{ "include": ... }` at the
place where the components go, in `components` as in `fields`.

```json
{
  "name": "/TRBASE/ADDRESS",
  "texts": { "en": { "label": "Address" }, "de": { "label": "Adresse" } },
  "components": [
    { "name": "STREET", "dataElement": "/TRBASE/STREET" },
    { "name": "CITY", "dataElement": "/TRBASE/CITY" },
    { "name": "COUNTRY", "dataElement": "/TRBASE/COUNTRY" }
  ]
}
```

```json
{
  "name": "/TRFIN/COMPANIES",
  "fields": [
    { "name": "COMPANY_ID", "dataElement": "/TRFIN/COMPANY" },
    { "include": "/TRBASE/ADDRESS" },
    { "name": "STATUS", "domain": "/TRFIN/COMPANY_STATUS", "notNull": true, "default": "ACTIVE" }
  ],
  "primaryKey": ["COMPANY_ID"],
  "foreignKeys": [
    { "name": "COUNTRY", "table": "/TRFIN/COUNTRIES", "fields": [{ "field": "COUNTRY", "references": "COUNTRY_CODE" }] }
  ]
}
```

```json
{
  "name": "/TRFIN/COMPANY_ARRAY",
  "texts": { "en": { "label": "Companies" } },
  "rowType": { "structure": "/TRFIN/COMPANIES" },
  "key": { "fields": ["COMPANY_ID"], "unique": true }
}
```

```json
{ "structure": "/TRBASE/ADDRESS", "components": [{ "name": "/TROTTO/PICKUP_POINT", "dataElement": "/TROTTO/PICKUP_POINT" }] }
```

## Rules

- Names of objects of a package are `/<PACKAGE-ID>/<NAME>`, where `<NAME>` consists of
  upper-case letters, digits and underscores and starts with a letter. Table, domain, data
  element and setting names are unique in the database; tables, structures and array types
  share one set of names, because a component of kind `structure` may name either. Names are always quoted in SQL.
- The loader checks namespaces on import, right after the JSON Schema check and before any
  object is built: definitions must carry the namespace of the package, references that of
  the package, of a package listed in its manifest, or the kernel's. All violations are
  reported together as `rterr.NamespaceViolation`, with file, JSON Pointer, the name found and
  the expected namespaces. Files that belong to no known object type are rejected as well.
  Every object type registers with the loader for this check.
- Names defined by packages are limited to 63 bytes and rejected when longer, never shortened.
- Generated names: `<TABLE>#PK`, `<TABLE>#FK#<KEY>`, `<TABLE>#CK#<FIELD>`. A generated name
  longer than 63 bytes keeps its first 52 bytes followed by `~` and 10 hex digits of the
  SHA-256 of the full name. The repository stores the generated names; `rt check` compares
  them. `#` and `~` cannot occur in object names.
- The kernel owns the namespace `/RT/`; the package ID `RT` is reserved.
- A foreign key references the complete primary key of its target, which must belong to the
  same package or a required one. Field types must match (same base type, CHAR not longer
  than the target, DEC with equal precision and scale).
- Kernel domains and the tenant table are available to every package. Other kernel tables
  can neither be referenced nor extended.

### Asset repositories

`assets/templates/asset.json` of `TRFIN`:

```json
{
  "description": { "en": "Finance print templates", "de": "Druckvorlagen Finanzen" },
  "folders": { "invoice": { "description": { "en": "Invoices", "de": "Rechnungen" } } },
  "files": {
    "invoice/invoice.xsl": {
      "languages": { "en": "invoice/invoice_en.xsl", "de": "invoice/invoice_de.xsl" }
    },
    "data/rates.dat": { "mimeType": "text/csv" }
  }
}
```

- The repository ID is the folder name: up to 32 lower-case letters, digits, `_` and `-`,
  starting with a letter. `web` of `TRBASE` and `web` of `TRFIN` are separate repositories.
  Repositories and assets are always addressed by package ID, repository ID and path.
- `description` (the label) is mandatory and needs English. `folders` adds descriptions and
  folders that may be missing on disk. `files` sets the media type, a description and
  language variants per path.
- A path with `languages` is language-dependent. Its variant files are reachable only through
  that path; a file at the path itself is the language-independent version. Reading with a
  language takes the requested language, then English, then the language-independent version;
  without a language the language-independent version, then English. Variants in languages
  that are not set up are rejected like texts.
- Paths: segments of ASCII letters, digits, `.`, `_` and `-` that do not start with `.`, at
  most 100 bytes per segment and 255 bytes per path, no leading, trailing or double slashes,
  and no two paths that differ only in case.
- Rejected: files directly below `assets/`, folders without `asset.json`, entries in
  `asset.json` without file, and symbolic links anywhere in the package.
- The media type comes from a fixed table of extensions; unknown extensions give
  `application/octet-stream`. Files may have at most 16 MiB by default
  (`Kernel.SetMaxAssetSize`, `--max-asset-size`, `RT_MAX_ASSET_SIZE`).
- Contents are stored in the database in chunks of 256 KiB, addressed by their SHA-256 and
  stored once. Reading streams chunk by chunk. Upgrades compare hashes: new assets are added,
  changed ones replaced, removed ones dropped, and contents no longer referenced are deleted
  in the same transaction. Asset changes are listed one by one in the plan and are never
  destructive. `rt check` verifies every content against its hash.

Reading at runtime:

```go
r, err := k.OpenAsset(ctx, "TRFIN", "templates", "invoice/invoice.xsl", model.Some("de"))
defer r.Close()
info := r.Info() // MimeType, Size, ETag(), ModifiedAt, Language
io.Copy(w, r)    // r is an io.ReadSeeker

fsys, err := k.AssetFS(ctx, "TRFIN", "templates", model.Some("de")) // io/fs.FS for one language
tmpl, err := template.ParseFS(fsys, "mail/*.html")
```

`StatAsset`, `ListAssets`, `AssetExists` and `AssetRepositories` complete the API. Missing
assets return `rterr.AssetNotFound`, which matches `fs.ErrNotExist`.

### null and the empty string

`null` means "no value"; `""` is a value. The kernel keeps them apart in package files,
validation, repository, database, Go API, JSON and CLI and never converts one into the other.

- A field with `notNull: false` accepts `null` and `""` as different values; `notNull: true`
  rejects `null` but accepts `""`.
- Domains of type CHAR and STRING may set `"allowEmpty": false` (default `true`); the kernel
  then adds a check that rejects `""`. Fixed values may be `""`, never `null`.
- Defaults have three cases, in domains and fields alike: no `default` attribute (no
  default), `"default": null` (the explicit default null) and `"default": ""` or another value.
  A field without own default takes the default of its domain. The DDL writes no DEFAULT,
  `DEFAULT NULL` or `DEFAULT '…'` accordingly, and the repository stores the kind. A default
  `null` on a `notNull` field is invalid.
- CHAR(n) is `VARCHAR(n)` in PostgreSQL and a length check in SQLite: values are neither
  padded nor trimmed, trailing spaces are kept.
- In Go, values that may be absent are `model.Optional[T]`; defaults are `model.Default`. JSON
  writes `null` for absent values, `""` for the empty string, and leaves out a default only if
  there is none. The CLI shows `<null>` and quoted strings.

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

### Structures and array types

- Component names are unique after all includes and extensions are resolved. A collision
  names both origins, e.g. `component CITY of /X/SITES collides with component CITY from
  include /TRBASE/ADDRESS`. `/RT/TENANT` is reserved and cannot be a component.
- Structures, tables and array types must not contain themselves, through any mix of
  includes, nested components, array components and row types. The error shows the full
  chain, e.g. `/X/A -> include /X/B -> component C /X/C -> row type /X/A`.
- Referenced objects belong to the own package, a required one or the kernel.
- Structures may include flat and deep structures and tables; tables include only flat ones.
  A structure extension that would make a structure deep is rejected when a table includes
  that structure.
- An included table brings only its fields, including the fields of its table extensions:
  not its tenant field, primary key, foreign keys or data class. Its key fields come without
  their NOT NULL, which follows from the key. The including table may use included columns
  in its primary key and foreign keys, and foreign keys may reference included key columns.
- Components of structure extensions follow the components of the structure, ordered by
  package. Structure extensions follow the rules of table extensions: names in the namespace
  of the extending package, nullable or with a default, no includes.
- `default` and `notNull` exist only for elementary components.
- Array keys consist of elementary top-level components of a structure or table row type.
- A change of a structure changes the columns of every table that includes it, in every
  package; the plan shows the structure, e.g. `add-column /TRFIN/COMPANIES.REGION (CHAR(80))
  via /TRBASE/ADDRESS`. A removed or narrowed component is destructive. A change that makes an
  object of another package invalid is rejected and names that object. Uninstalling a package
  removes its structures, array types and structure extensions with their columns, after
  confirmation.
- The repository stores structures, array types, extensions and includes, and for every
  column the include it comes from (`INCLUDE_NAME`) and the declaring structure or table
  (`SOURCE_NAME`) with origin `INCLUDE`. `rt check` compares the stored columns with the
  resolved includes.

`kernel.Structure` (and `rt show`) returns the resolved definition from package
[structure](structure) with its uses: components in their final order with type, labels,
effective default and origin (declaring object, include chain, extending package); nested
structures and array types are resolved as well. Setting fields name the include and the
source of included columns.

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

tenant, err := k.Tenants().Create(ctx, "ACME", model.Some("Acme Corp"))
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
rt show /TRFIN/COMPANIES
rt check
rt setting list
rt setting show /TRFIN/COMPANIES
rt setting show /TRFIN/COMPANIES --json
rt show data-element:/TRBASE/COUNTRY
rt show /TRBASE/ADDRESS
rt show /TRFIN/COMPANY_ARRAY --json
rt show /TRFIN/COMPANIES_ARCHIVE
rt asset repos
rt asset list TRFIN templates invoice
rt asset show TRFIN templates invoice/invoice.xsl
rt asset get TRFIN templates invoice/invoice.xsl --lang de -o invoice.xsl
rt tenant create ACME --description "Acme Corp"
rt tenant delete ACME --yes
rt uninstall TROTTO --yes
rt log
```

```bash
rt fmt path/to/package
rt fmt --check .
```

Flags may appear anywhere: `--db`, `--dry-run`, `--allow-destructive`, `--yes`, `--json`,
`--description`, `--name`, `--lang`, `-o`, `--max-asset-size`. Exit codes: 0 success, 2 usage, 3 validation or dependency error,
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
go test ./internal/planner ./setting ./kernel -update
```

## Not included yet

Executable code in packages, permissions, user interface, views, indexes beyond keys,
data migrations with custom logic, remote package sources, installing from ZIP files (the
loader already works on `fs.FS`), a data access layer that restricts queries to a tenant,
reading and writing data through settings, display properties of fields, translation packages
for objects of other packages, export or protection of configuration by data class, serving
assets over HTTP, assets uploaded at runtime or per tenant, extending asset repositories of
other packages, references to assets from other objects, compression and image processing,
the runtime meaning of `notNull` for nested and array components, includes in extensions.

## License

Apache License 2.0, see [LICENSE](LICENSE).
