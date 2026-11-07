# Rules for working on go-runtime

These rules apply to every change in this repository.

## Language

- Everything in the repository is English: code, identifiers, comments, error messages, CLI
  output, JSON Schemas with their descriptions, README, documentation, example packages and
  commit messages.
- Do not use the terminology of other vendors' enterprise software products anywhere in the
  repository or in commit messages, not even as comparison. Use neutral terms such as
  schema, domain, table, extension, setting or asset repository. The list of banned terms is
  kept outside the repository. Before every commit, search the changed files and the commit
  message for those terms, ignoring case.

## Namespaces

- Every named object of a package starts with the namespace of its package:
  `/<PACKAGE-ID>/<NAME>`, e.g. `/TRFIN/COMPANIES`. The full name is used everywhere: in
  package files for definitions and for every reference (also within the same package), in
  the repository, the database, the CLI and messages. There is no short form and the kernel
  never adds a namespace.
- Fields and foreign keys declared by a table belong to the table and have no namespace.
  Fields and foreign keys added by an extension are objects of the extending package and do.
- Objects identified by package ID plus their own key (asset repositories) have no name of
  their own and are exempt.
- The kernel checks namespaces centrally in the package loader on import, right after the
  JSON Schema check and before anything is built, resolved or compared with the repository:
  definitions need the own namespace; references the own one, that of a package listed in
  the manifest, or the kernel's (`/RT/`). All violations are reported together.
- Every object type registers with the loader (`internal/pkgload/objecttypes.go`) and states
  how to read the names it defines and references. A new object type must register; the
  loader rejects files that belong to no registered type, and a test checks that every
  registered type takes part in the namespace check.
- Generated SQL names (constraints, indexes, rebuild tables) stay within 63 bytes using
  `model.GeneratedName`. Names defined by packages are rejected when too long, never
  shortened.

## null and the empty string

- `null` means "no value"; `""` is a value. They are different in every layer: package files,
  validation, repository, database, Go API, JSON and CLI. Nothing converts one into the other,
  not when reading, writing, comparing or serializing.
- In Go, values that may be absent use `model.Optional[T]` (or a dedicated type such as
  `model.Default`); an empty Go string never stands for null. JSON writes `null` for absent
  values. A missing default, the default `null` and the default `""` are three cases.
- CHAR(n) is VARCHAR(n) in PostgreSQL and a length check in SQLite; values are neither padded
  nor trimmed.

## Work style

- Plan non-trivial changes and present the plan before writing code.
- Every milestone ends with green tests against SQLite and, with `RT_TEST_POSTGRES_DSN`,
  PostgreSQL.
- Every Go source file starts with the Apache 2.0 license header.
