# dbsync — MySQL Schema Sync Tool

## Overview

**dbsync** is a Go CLI tool and library that synchronizes MySQL database schemas. Instead of writing migration files, you capture a **snapshot** (`snash`) of a database schema to a JSON file, then **restore** that schema to another database. On restore, it diffs the target DB against the JSON and automatically generates `ALTER TABLE`, `CREATE TABLE`, `DROP TABLE`, and constraint-management queries to match the snapshot.

## Architecture

```
main.go                     — Entry point; registers the MySQL driver, calls cmd.Execute()
cmd/
  root.go                   — Root cobra command, viper config init, global persistent flags
  snash.go                  — "snash" subcommand: creates a DB snapshot JSON
  restore.go                — "restore" subcommand: applies a snapshot JSON to a target DB
msc/                        — Core library (no CLI dependency)
  msc.go                    — DBConn (connection, query helpers), structs (Table, Field, Index, Constrain), Config, Struct2json
  snash.go                  — Snash(): connects, introspects tables/fields/indexes/constraints, writes JSON
  restore.go                — Restore.Run(): reads JSON, diffs vs target DB, runs DDL
```


### Prefix System
- The `prefix` is **stripped** from table names when creating a snapshot.
- The `prefix` is **added** to table names when restoring.
- This allows dev tables (e.g., `mytable`) and prod tables (e.g., `p_8_mytable`) to map to the same snapshot.

### Restore Diffs
On restore, for each table in the JSON:
1. **Exists in target?** → Compare and alter collation, engine, comment, columns, indexes
2. **Missing in target?** → `CREATE TABLE IF NOT EXISTS`
3. **Extra tables in target?** (if `DTable=true`) → `DROP TABLE IF EXISTS`

For columns: compare `IS_NULLABLE`, `COLUMN_TYPE`, `COLUMN_DEFAULT`, `COLUMN_COMMENT`. Missing columns are added; extra columns are dropped (if `DColumn=true`).

For constraints: dropped and re-created to match the JSON. Foreign keys must have `fk_` prefix in their name.

### Session SQL Modes
The restore sets specific `sql_mode` values at different stages to allow constraint manipulation:
- `ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION`
- During constraint phase: `FOREIGN_KEY_CHECKS=0`, `UNIQUE_CHECKS=0`

## Dependencies

| Dependency | Usage |
|---|---|
| `github.com/go-sql-driver/mysql` v1.5.0 | MySQL driver |
| `github.com/spf13/cobra` v1.1.3 | CLI framework |
| `github.com/spf13/viper` v1.7.1 | Config file management |
| `github.com/fatih/color` v1.10.0 | Colored SQL output in restore |

## Configuration

CLI flags or `.dbsync.json` profile file. Profiles are named connection presets:
```json
{
  "files_path": "./snash",
  "profiles": {
    "dev": { "dbname": "...", "user": "...", "host": "...", "prefix": "", ... },
    "prod": { "dbname": "...", "prefix": "p_8_", "delete_table": true, "delete_column": true, ... }
  }
}
```

## CLI Usage
```
dbsync snash -p=dev       # Create snapshot
dbsync restore -p=prod    # Restore to target DB
```

## Programmatic Usage
```go
msc.Snash(msc.Config{User: "root", Pass: "root", Host: "localhost", Port: "3306", DB: "test", FilesPath: "./snash", Prefix: "", File: "test.json"})

r := msc.Restore{}
r.Run(msc.Config{User: "root", ..., DTable: true, DColumn: true, DIndex: true, DConstraint: true, Optimize: true})
```

## Known Quirks
- "snash" is an intentional non-standard spelling used throughout (not "snapshot")
- The JSON key for constraints is `constraines` (misspelled) — must be preserved for compatibility
- `Optimize: true` only runs `OPTIMIZE TABLE` for InnoDB and MyISAM engines
- Foreign key constraints require `fk_` prefix in their names
- Views, routines, and triggers are planned but not yet implemented
- Go module has a `replace` directive: `github.com/serhioromano/dbsync/cmd => ../cmd`
- The package.json is only used for npm-based build scripts (Go cross-compilation), not an npm package
