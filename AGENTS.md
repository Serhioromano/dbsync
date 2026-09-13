# AGENTS.md

Orientation for AI agents working in this repository.

This document was written by reading the source tree. It was **not** verified by
building or running anything (shell execution was unavailable in the environment
where it was authored). Where documentation and code disagree, this file follows
the **code**, and the disagreement is called out in
[Traps: docs vs. reality](#9-traps-docs-vs-reality).

---

## 1. What this project is

`mysqlsync` (Go module `github.com/serhioromano/mysqlsync`) is a **schema
synchronization** CLI and library for **MySQL** and **SQLite**.

There are no migration files. The model is:

1. **`snash`** — introspect a source database and write its schema to a
   **DBML** file. (`snash` is a deliberate misspelling of "snapshot"; it is used
   everywhere, including command names. Do not "fix" it.)
2. **`restore`** — parse that DBML, diff it against a target database, and
   execute generated DDL to make the target match.

What it is **not**:

- Not a migration-history tool. There are no versions, no ordering, no
  down-migrations, and no rollback.
- Not a data migration tool. `Records`, `Enum`, `TableGroup`, and `Project`
  blocks in a DBML input are parsed and then **discarded**.
- Not a full schema tool. Views, routines, and triggers are declared as planned
  but are not implemented.

Restore is intended to be non-destructive: it alters existing objects and only
drops tables/columns/indexes/constraints when the corresponding flag is enabled.

---

## 2. Commands

Derived from `Makefile` and `package.json`. Not executed, so treat exit codes as
unverified.

```bash
# Build / run
go build .                                  # produces ./mysqlsync
go run . snash   -p=dev                     # snapshot using profile "dev"
go run . restore -p=prod                    # restore using profile "prod"
go run . snash   --engine=sqlite --db=/path/to/db.sqlite
go run . snash   --help
go run . restore --help

# Dependency hygiene
make deps                                   # = go mod tidy

# Packaging (cross-compiled static binaries)
make build        # CGO_ENABLED=0, all PLATFORMS -> bin/
make pack         # npm pack    (prepack runs `make build` first)
make publish      # npm publish (prepack runs `make build` first)
make clean
```

`package.json` scripts are thin wrappers: `npm run build|pack|bump|release|clean`
shell out to the equivalent `make` target, and `npm run snash|restore` run
`go run . … -p=dev|prod`. There is deliberately **no npm `publish` script** — npm
fires a lifecycle script of that name during `npm publish`, which would recurse.

Binaries are built with `CGO_ENABLED=0`, which is why SQLite must remain a
pure-Go driver (`modernc.org/sqlite`). Do not reintroduce a cgo-based SQLite
driver without also reworking the Makefile's "fully static" promise.

### Versioning, packaging and release

The published npm package **is the repository root**: the root `package.json` is
the manifest npm uploads. There is no staging directory and no separate
publishing manifest.

```bash
make version                  # print current version
make bump BUMP=patch          # patch | minor | major
make bump BUMP=minor TAG=0    # TAG=0: rewrite package.json only, no git commit/tag
make release BUMP=patch       # bump -> publish
make release-minor            # shorthand for `make release BUMP=minor`
make pack                     # npm pack (tarball for inspection)
make publish                  # npm publish the current version
```

How the tarball stays minimal: `package.json` sets
`"bin": {"mysqlsync": "bin/mysqlsync"}` and `"files": ["bin/"]`, so only the Node
launcher and the prebuilt `bin/mysqlsync-<os>-<arch>` binaries ship (plus the
always-included `package.json`/`README`/`LICENSE`) — never the Go sources.
`prepack` runs `make build`, so `npm pack`/`npm publish` rebuild first, and
`bin/mysqlsync-*` is gitignored yet still packed.

Gotchas when editing this area:

- Do **not** add an npm script named `publish` to `package.json`: npm runs it as
  a lifecycle script during `npm publish`, which would recurse into
  `make publish` → `npm publish`. The Makefile target `publish` and the npm
  script `release` are both fine.
- Keep `VERSION` **lazily expanded** (`?=`/`=`), never `:=`, so `make bump`
  reports the new value within the same run.
- A version cannot be passed as a bare make argument: `make release patch` would
  be read as two goals and fail. Use `BUMP=patch` or `release-patch`.
- `bump` delegates to `npm version`, so with `TAG=1` (default) it also creates
  the `vX.Y.Z` commit and tag, and like `npm version` it **refuses to run on a
  dirty git tree** (`TAG=0` skips git entirely).
- `os`/`cpu` in the root manifest are install-time constraints and can make
  `npm install` fail inside this repo on an unsupported platform.

---

## 3. Layout

| Path | Role |
|---|---|
| `main.go` | Entry point. Blank-imports the MySQL driver and `modernc.org/sqlite`, then calls `cmd.Execute()`. |
| `cmd/root.go` | Root cobra command, all persistent flags, viper config init. |
| `cmd/snash.go` | `snash` subcommand; owns `getEngine(name)` — the engine registry. |
| `cmd/restore.go` | `restore` subcommand; maps profile JSON and flags into `schema.Config`. |
| `msc/schema/types.go` | Shared domain types (`Config`, `Schema`, `TableDef`, `FieldDef`, `IndexDef`, `ConstraintDef`) and the `Engine` interface. **Start here.** |
| `msc/dbml/writer.go` | `Schema` → DBML text. |
| `msc/dbml/parser.go` | DBML text → `Schema`. Hand-rolled line/string parser, no dependency. |
| `msc/mysql/engine.go` | MySQL `Snapshot` + `Restore` (uses `INFORMATION_SCHEMA`, `SHOW INDEXES`). |
| `msc/sqlite/engine.go` | SQLite `Snapshot` + `Restore` (uses `PRAGMA table_info` / `index_list` / `foreign_key_list`). |
| `msc/msc.go` | Backward-compat shim. Type aliases to `schema.*`, plus `Snash()` / `Restore.Run()` hardwired to MySQL. |
| `test/db.dbml` | dbdiagram.io-style sample schema (with `Records` blocks). Not referenced by any code. |
| `test/sqlite_test.sqlite` | Binary SQLite fixture. Not referenced by any code. |
| `.mysqlsync.json` | Committed example config: `files_path` + `profiles` (`dev`, `prod`). |
| `bin/mysqlsync` | Tracked Node launcher (the npm `bin` entry) that picks the prebuilt `bin/mysqlsync-<os>-<arch>` binary for the host platform. |
| `Makefile` | Cross-platform build + npm packaging + version bumping. |
| `package.json` | The published npm manifest: version source of truth, `bin`/`files`/`os`/`cpu`, and script wrappers. Publishing runs from the repo root. |
| `.vscode/extensions.json` | Recommends DBML syntax/visualization extensions. |
| `.pi/SYSTEM.md` | **Stale.** See traps. |
| `README.md` | Mostly accurate user-facing docs; a few claims are stale (see traps). |

---

## 4. Architecture and data flow

```
                 cmd/snash.go                       cmd/restore.go
                      │                                   │
        schema.Engine (interface, msc/schema/types.go)    │
              ┌───────┴────────┐                          │
     msc/mysql/engine.go   msc/sqlite/engine.go           │
              │  Snapshot()        Snapshot()             │
              └───────┬────────────┘                      │
                      ▼                                   │
              *schema.Schema ──► msc/dbml/writer.go ──► .dbml file
                                                          │
                                       msc/dbml/parser.go ◄┘
                                                          ▼
                                            Engine.Restore(cfg, *Schema)
                                                          ▼
                                        generated DDL executed against target
```

The `Engine` interface is the single extension point:

```go
type Engine interface {
    Snapshot(cfg Config) (*Schema, error)
    Restore(cfg Config, schema *Schema) error
}
```

Both engines share the same DBML serialization and the same per-table diff
strategy: compare metadata (engine, collation, comment), then fields, then
indexes; create the table if missing; drop extras only when the corresponding
`D*` flag is set.

Note the import alias convention: engine packages import `msc/schema` as `s`.

### Restore phases

1. Connect; on MySQL set a session `sql_mode`.
2. Per table in the snapshot: alter metadata / add missing columns / drop extra
   columns (if `DColumn`) / add missing indexes / drop extra indexes (if `DIndex`).
3. Drop tables not present in the snapshot (if `DTable`).
4. MySQL only: a separate **constraints phase** with `UNIQUE_CHECKS=0` and
   `FOREIGN_KEY_CHECKS=0`, which drops and re-adds foreign keys and then runs
   `OPTIMIZE TABLE` for InnoDB/MyISAM (if `Optimize`).
5. SQLite: `PRAGMA optimize` (if `Optimize`).

---

## 5. The DBML dialect

`writer.go` produces a **canonical** subset; `parser.go` accepts the canonical
subset **plus** ordinary dbdiagram.io syntax, so hand-written and
dbdiagram.io-generated files both work.

Canonical output:

```dbml
// Schema: my_database
// Prefix: p_8_

Table "users" {
  "id" int [pk, increment, not null]
  "email" varchar(255) [not null, unique]
  "status" tinyint [not null, default: 1]
  "created_at" timestamp [not null, note: 'Account created']

  Indexes {
    (username) [name: "username_idx", type: btree]
  }

  Note: '''Engine: InnoDB | Collation: utf8mb4_general_ci | Comment: Users'''
}

Ref: "posts"."user_id" > "users"."id" [delete: cascade, update: cascade]
```

Accepted by the parser:

- Table names and column names may be quoted (`"name"`) or bare.
- Field settings: `pk` / `primary key`, `increment` / `auto increment`,
  `not null`, `null`, `unique`, `default: <value>`, `note: '<text>'`.
  Defaults may be wrapped in backticks or single quotes; both are stripped.
- Index entries: `(col1, col2) [name: "...", type: btree|fulltext|hash, unique]`.
- Refs: `Ref: "t"."c" > "t"."c"`, `Ref: "t"."c" < "t"."c"` (direction is
  normalized), and `Ref <name>: t.c > t.c [delete: …, update: …]`.
- Ignored entirely: `Records`, `Enum`, `TableGroup`, `Project`, top-level
  `Note:`, and `//` comments.
- Table metadata travels inside the table `Note` as a
  `key: value | key: value` list (`Engine`, `Collation`, `Comment`).

Names are escaped by doubling/backslashing quotes (`\"`), not by DBML's own
quoting rules — the parser mirrors this, so the round trip holds, but a strict
external DBML tool may disagree.

---

## 6. The prefix system

- `prefix` is **stripped** from table names on snapshot.
- `prefix` is **added** to table names on restore.

So dev table `users` and prod table `p_8_users` map to one snapshot.

⚠️ **Snapshot with a non-empty prefix is broken in both engines.** During
snapshot the table name is already the raw, prefixed name, but the field/index/
constraint helpers prepend `e.prefix` again, producing a double-prefixed lookup
(`p_8_p_8_users`) that returns no rows. Restore passes the short name and is
therefore correct. Workaround: snapshot with an empty prefix, or fix the
helpers. Verify before relying on prefixed snapshots.

The prefix is also applied to **constraint names** on MySQL restore
(`e.prefix + c.Name`), while snapshots store the raw constraint name. Constraint
comparison strips one prefix from each side.

---

## 7. Configuration and flag precedence

Config file discovery is **hardcoded** to `.mysqlsync.json` in `$HOME/.mysqlsync`
and `.` (via viper). The `--config` flag is registered but **never read** — it is
a no-op.

Precedence, in the order `initConfig` applies it:

1. Config file defaults (top-level `files_path` → key `path`).
2. Profile values, when `-p/--profile` is given: `user`, `db` (from `dbname`),
   `port`, `pass`, `host`, `prefix`, `file` (from `file_name`), `engine`.
3. Non-empty CLI flags override the above.

⚠️ Because the `--engine` flag has a non-empty default (`"mysql"`), step 3 always
fires for it, so a profile's `engine` is **always clobbered back to `mysql`**.
Every other flag defaults to `""` and so does not clobber. Consequence: **SQLite
via a profile does not work** — you must pass `--engine=sqlite` explicitly.

`restore` additionally copies `delete_table`, `delete_index`, `delete_column`,
`delete_constraint`, and `optimize` out of the selected profile.

⚠️ The boolean restore flags default to `true` and are only assigned when the
parsed value is `true`, so `--d-table=false` is silently ignored. Disable them in
the profile JSON instead.

Profile keys are `dbname`, `file_name`, `delete_table`, `delete_column`,
`delete_index`, `delete_constraint`, `optimize`, `engine`, `user`, `pass`,
`host`, `port`, `prefix`.

---

## 8. Conventions and invariants

- **`snash` is always spelled `snash`** (command, file names, help text).
- Foreign key constraints are expected to be named with an **`fk_` prefix**.
  This is convention, not enforcement: MySQL index handling skips indexes whose
  name contains `fk_`, and `isFKIndex` matches by substring containment.
- Primary-key columns are conventionally named `id` (not required by code).
- Adding a database engine means: create `msc/<engine>/`, implement
  `schema.Engine`, and register it in `getEngine()` in `cmd/snash.go`.
- DBML is the snapshot format. Do not reintroduce JSON snapshots.
- Keep SQLite cgo-free (see §2).
- Errors are inconsistent across the codebase: public methods return `error`,
  but the private `exec` helpers **panic** (see §10). Preserve or fix
  deliberately — do not assume a returned `error` covers DDL failures.

---

## 9. Traps: docs vs. reality

| Document claim | Reality |
|---|---|
| `.pi/SYSTEM.md` describes JSON snapshots and files `msc/snash.go`, `msc/restore.go` | All false. Snapshots are DBML; those files do not exist. The whole document is obsolete — **do not trust it**. |
| `.pi/SYSTEM.md` lists `github.com/fatih/color` as a dependency | `fatih/color` is not imported anywhere. |
| `README.md`: "Table rebuilds are used for structural changes" (SQLite) | No rebuild exists. `msc/sqlite/engine.go` explicitly skips existing-column changes with a `Skip for now` comment. |
| `README.md` implies `--d-column` drops columns on SQLite | The SQLite engine ignores `DColumn` entirely. |
| `README.md` implies constraint changes are applied on SQLite | FKs are only emitted inline during `CREATE TABLE`; existing tables never gain or lose FKs, and `DConstraint` is ignored. |
| `README.md` lists `-c/-i/-k/-t/-o` as usable flags | Registered, but `false` cannot be expressed on the CLI (see §7). |

When editing docs, prefer correcting these to matching the code.

---

## 10. Bugs and sharp edges

Verified by reading; each is worth confirming with a real database before
"fixing", and worth mentioning to the user rather than silently changing.

1. **DBML headers are never parsed.** In `dbml.Parse`, inline `//` comments are
   stripped before the `// Schema:` / `// Prefix:` check, so those lines become
   empty strings and the header branch is unreachable. Result: `Schema.Name` and
   `Schema.Prefix` are always empty after parsing. Restore still works because
   the prefix comes from config.
2. **Double-prefix on snapshot** — see §6. Affects both engines.
3. **MySQL `CREATE TABLE` forces `AUTO_INCREMENT`** on every primary-key column
   (`NULL AUTO_INCREMENT`), ignoring `IsAutoIncr`. A non-auto-increment PK is
   created as auto-increment.
4. **MySQL existing-table path skips PK columns entirely** (`if fd.IsPrimary {
   continue }`), so PK type/nullability/default changes are never applied.
5. **Indexes are never altered**, only added when the name is absent or dropped
   when absent from the snapshot. Changing an index's columns or uniqueness under
   the same name is a silent no-op (both engines).
6. **`exec` panics on any DDL error** (both engines). A failed statement aborts
   the process mid-restore instead of returning an error; `Restore` almost never
   returns non-nil. `silentExec` swallows errors and still prints the SQL.
7. **MySQL defaults are dropped for `text`/`blob` and `datetime`/`timestamp`**
   in `formatDefault`, so e.g. `DEFAULT CURRENT_TIMESTAMP` is lost on restore.
   There is also an unreachable `dt == "int" && def == ""` branch.
8. **Column order is not preserved** when adding columns to existing tables —
   MySQL `ADD COLUMN` has no `AFTER` clause, so new columns land at the end.
9. **`go.mod` still has `replace github.com/serhioromano/mysqlsync/cmd => ../cmd`**,
   pointing outside the module. Stale; should disappear after `make deps`.
10. **`go.sum` has no `modernc.org/sqlite` entries** as of this writing (it still
    carries `mattn/go-sqlite3`). A build will fail until `make deps` / `go mod
    tidy` is run. `main.go` imports `modernc.org/sqlite`, which registers the
    driver name `"sqlite"` — matching `sql.Open("sqlite", …)` in the SQLite
    engine. (The previous cgo driver registered `"sqlite3"` and would not have
    matched.)
11. **Dead / vestigial code**: `msc.escapeDBMLName` is unused;
    `msc.Struct2json` prints a deprecation warning and returns an empty map;
    `--config` is ignored; `parseRefLine` re-declares an anonymous struct.
12. **Refs to unknown tables are dropped silently**, and bare `Table` names are
    passed through `strings.Trim(rest, "\"")`, which strips any number of
    leading/trailing quotes.

---

## 11. Testing and verification

There is **no test suite and no CI**: no `*_test.go` files exist anywhere, and
`test/` holds only manual fixtures that no code references.

Consequences for an agent:

- There is no `go test` signal to lean on. Do not claim a change is verified
  without one of the checks below.
- Meaningful verification requires a **live database** (MySQL server, or a
  throwaway SQLite file) plus a DBML file, e.g.:
  ```bash
  go build -o /tmp/mysqlsync .                       # compile check
  go run . restore --engine=sqlite --db=/tmp/t.sqlite -f test/db.dbml
  ```
- A useful cheap check after touching the DBML layer is a **round trip**:
  parse → write → parse and compare `Schema` values, since writer/parser are
  meant to be symmetric.
- `go vet ./...` is the only static check currently available.

---

## 12. Environment and dependencies

- `go.mod` declares `go 1.16`, but the code uses `os.ReadFile`/`os.MkdirAll`
  (1.16+) and `strings.ReplaceAll` (1.12+); a modern toolchain is fine.
- Runtime dependencies: `github.com/go-sql-driver/mysql`,
  `modernc.org/sqlite` (pure Go, no cgo), `github.com/spf13/cobra`,
  `github.com/spf13/viper`.
- `golang.org/x/sys` is an indirect dependency.
- SQLite support must stay cgo-free for the Makefile's static cross-builds.
- Version lives in the root `package.json` (`1.0.1`) and is bumped with
  `make bump BUMP=patch|minor|major` (or `make release BUMP=…`). npm publishes
  that same manifest, so there is no version injection or placeholder.
  See [Versioning, packaging and release](#versioning-packaging-and-release).

---

## 13. Git

- Current branch: `develop` (checked out at the time of writing). The repo
  follows a git-flow-style model — `.vscode/settings.json` sets
  `"gitflow.variant": "auto"`.
- `.gitignore` covers `snash/` (the default snapshot output directory, which is
  also the `files_path` in `.mysqlsync.json`), the `mysqlsync` binary, the
  cross-compiled `bin/mysqlsync-*` binaries, and packaging artifacts (`dist/`,
  `build/`, `*.exe`, `*.tgz`). The `bin/mysqlsync` launcher must stay tracked.
- Do not commit generated snapshots or `dist/` output.
