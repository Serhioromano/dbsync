# MySQL / SQLite Migration Tool on Steroids

As we all know, migrations are a pain. There are so many points of failure, and those who have been in that cycle know how hard it is to get out of trouble when your migration sequence fails in the middle of the way.

> **Disclaimer:** 
> 1. Works with MySQL and SQLite databases
> 2. All foreign keys (constraints) **MUST** have `fk_` prefix in their name
> 3. Primary auto-increment fields are preferably named `id` (not required)

`dbsync` is a completely new approach to DB migrations. Instead of creating migration files which apply changes from stage to stage, you make a snapshot (`snash`) of a DB structure to a **DBML file** (Database Markup Language). Then you restore this file to another DB. It will compare the target DB against the DBML definition and automatically generate migration queries to alter columns, indexes, create or delete tables and constraints. It will try to do it the most non-destructive way.

### Multi-Database Support

`dbsync` supports both **MySQL** and **SQLite** databases through a clean abstraction layer. Use the `--engine` flag to choose:

```bash
dbsync snash --engine=mysql --db=mydb ...
dbsync snash --engine=sqlite --db=/path/to/database.sqlite ...
```

### Why DBML?

DBML is an open-source DSL for database schema definition. It is:
- **Human readable** — much easier to read and review diffs than JSON
- **Visualizable** — you can paste your `.dbml` files directly into [https://dbdiagram.io/home](https://dbdiagram.io/home) to see a beautiful ER diagram of your schema
- **Version control friendly** — clean text format that works well with git
- **Standardized** — used by thousands of developers for database design

## Architecture

The project is structured with a clean abstraction layer:

```
msc/
  schema/          — Shared types: Config, Schema, TableDef, FieldDef, IndexDef,
  │                 ConstraintDef, Engine interface
  dbml/
  │  writer.go     — Serializes Schema → DBML text
  │  parser.go     — Parses DBML text → Schema
  mysql/
  │  engine.go     — MySQL adapter (snapshot + restore)
  sqlite/
     engine.go     — SQLite adapter (snapshot + restore)
cmd/               — CLI commands
```

All adapters implement the `Engine` interface:
```go
type Engine interface {
    Snapshot(cfg Config) (*Schema, error)
    Restore(cfg Config, schema *Schema) error
}
```

Adding support for a new database (PostgreSQL, SQL Server, etc.) is as simple as creating a new package under `msc/` that implements the `Engine` interface.

## What it supports

1. Creation and deletion of tables
2. Altering and deleting table fields
3. Altering and deleting indexes
4. Altering constraints (foreign keys)
5. [planned] Updating views
6. [planned] Updating routines
7. [planned] Updating triggers

You can use it as a CLI tool or programmatically.

## Install

### npm (prebuilt binaries, no Go needed)

```bash
npm install -g @serhioromano/dbsync
```

Installs the `dbsync` command with prebuilt binaries for macOS (x64/arm64),
Linux (x64/arm64) and Windows (x64). The package is scoped, but the command it
installs is plain `dbsync`.

### Go

```
go get -u github.com/serhioromano/dbsync
```

## Use CLI

### Creating a Snapshot

```bash
# MySQL
dbsync snash --engine=mysql --user=root --pass=root --host=localhost --port=3306 --db=mydb

# SQLite
dbsync snash --engine=sqlite --db=/path/to/database.sqlite
```

### Restoring a Snapshot

```bash
# MySQL
dbsync restore --engine=mysql --user=root --pass=root --host=localhost --port=3306 --db=mydb

# SQLite
dbsync restore --engine=sqlite --db=/path/to/database.sqlite
```

You can see documentation for all options using `dbsync snash --help` or `dbsync restore --help`.

### Profile File

If you do not want to pass all parameters because there are a lot of them, you can create a profile file `.dbsync.json`. Here is an example:

```json
{
    "files_path": "./snash",
    "profiles": {
        "dev": {
            "engine": "mysql",
            "dbname": "icod_project",
            "user": "root",
            "pass": "root",
            "host": "localhost",
            "port": "3306",
            "prefix": ""
        },
        "prod": {
            "engine": "mysql",
            "dbname": "p_8",
            "user": "root",
            "pass": "root",
            "host": "localhost",
            "port": "3306",
            "prefix": "p_8_",
            "file_name": "icod_project.dbml",
            "delete_table": true,
            "delete_column": true,
            "delete_index": true,
            "delete_constraint": true,
            "optimize": true
        },
        "sqlite_dev": {
            "engine": "sqlite",
            "dbname": "./data/dev.sqlite",
            "prefix": ""
        }
    }
}
```

Now you can call the CLI tool with only one parameter `-p` or `--profile`:

```
dbsync snash -p=dev
dbsync restore -p=prod
dbsync snash -p=sqlite_dev
```

The first command creates `./snash/icod_project.dbml` with the `icod_project` DB model snapshot, and the second command restores it to the `p_8` DB with the new prefix.

### Parameters

#### Global (all commands)

| JSON Param | Flag | Description |
|---|---|---|
| — | `-h`, `--help` | Help for command |
| — | `--config` | Config file to load (default: `$PWD/.dbsync.json`) |
| — | `-p`, `--profile` | Name of connection profile in configuration file |
| — | `--engine` | Database engine: `mysql` or `sqlite` (default: `mysql`) |
| `files_path` | `--path` | Path where snapshot files are stored |
| `dbname` | `--db` | DB schema name (MySQL) or file path (SQLite) |
| `file_name` | `-f`, `--file` | File to save snapshot or restore from (default: `<dbname>.dbml`) |
| `host` | `--host` | DB host (MySQL only) |
| `pass` | `--pass` | DB password (MySQL only) |
| `port` | `--port` | DB port (MySQL only) |
| `prefix` | `--prefix` | DB table prefix. Stripped on snapshot, added on restore |
| `user` | `--user` | DB user name (MySQL only) |

#### Restore command only

| JSON Param | Flag | Description |
|---|---|---|
| `delete_column` | `-c`, `--d-column` | Delete table columns not in the snapshot (default: `true`) |
| `delete_constraint` | `-k`, `--d-constraint` | Delete constraints not in the snapshot (default: `true`) |
| `delete_index` | `-i`, `--d-index` | Delete indexes not in the snapshot (default: `true`) |
| `delete_table` | `-t`, `--d-table` | Delete tables not in the snapshot (default: `true`) |
| `optimize` | `-o`, `--optimize` | Run `OPTIMIZE TABLE` after finish (default: `true`) |

## DBML Snapshot Format

Snapshot files use the [DBML (Database Markup Language)](https://dbml.dbdiagram.io/docs/) format. Here's an example:

```dbml
// Schema: my_database
// Prefix: p_8_

Table "users" {
  "id" int [pk, increment, not null]
  "username" varchar(255) [not null]
  "email" varchar(255) [not null, unique]
  "status" tinyint [not null, default: 1]
  "created_at" timestamp [not null, note: 'Account creation timestamp']

  Indexes {
    (username) [name: "username_idx", type: btree]
  }

  Note: '''Engine: InnoDB | Collation: utf8mb4_general_ci | Comment: User accounts table'''
}

Table "posts" {
  "id" int [pk, increment, not null]
  "title" varchar(255) [not null]
  "body" text [null]
  "user_id" int [not null]

  Note: '''Engine: InnoDB | Collation: utf8mb4_general_ci'''
}

Ref: "posts"."user_id" > "users"."id" [delete: cascade, update: cascade]
```

### DBML Elements

- **`// Schema: <name>`** — Database schema name (header comment)
- **`// Prefix: <prefix>`** — Table prefix used in this snapshot (header comment)
- **`Table "<name>" { ... }`** — Table definition block
- **`"column" type [settings]`** — Column definition with optional settings:
  - `pk` — Primary key
  - `increment` — Auto increment
  - `not null` — Not nullable
  - `null` — Nullable
  - `unique` — Unique constraint (single column)
  - `default: value` — Default value (backtick-quoted for strings, bare for numbers)
  - `note: 'comment'` — Column comment
- **`Indexes { ... }`** — Multi-column or named index block:
  - `(col1, col2) [name: "idx_name", type: btree, unique]`
- **`Note: '''...'''`** — Table metadata (Engine, Collation, Comment separated by `|`)
- **`Ref: "table"."col" > "table"."col" [delete: rule, update: rule]`** — Foreign key reference

### Visualizing Your Schema

One of the best features of DBML is that you can copy-paste your entire snapshot file into [https://dbdiagram.io/home](https://dbdiagram.io/home) to generate a beautiful, interactive ER diagram of your database. This is invaluable for:

- Reviewing schema changes visually
- Onboarding new team members
- Documentation and architecture discussions

## How the Prefix System Works

- The `prefix` is **stripped** from table names when creating a snapshot
- The `prefix` is **added** to table names when restoring

This allows dev tables (e.g., `mytable`) and production tables (e.g., `p_8_mytable`) to map to the same snapshot effortlessly.

## How Restore Diffs Work

On restore, for each table in the DBML:

1. **Table exists in target?** → Compare and alter collation, engine, comment, columns, indexes
2. **Table missing in target?** → `CREATE TABLE IF NOT EXISTS`
3. **Extra tables in target?** (if `DTable=true`) → `DROP TABLE IF EXISTS`

For columns: compare `IS_NULLABLE`, `COLUMN_TYPE`, `COLUMN_DEFAULT`, `COLUMN_COMMENT`. Missing columns are added; extra columns are dropped (if `DColumn=true`).

For constraints: dropped and re-created to match the DBML. Foreign keys must have `fk_` prefix in their name.

### Session SQL Modes (MySQL)

The restore sets specific `sql_mode` values at different stages to allow constraint manipulation:
- `ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION`
- During constraint phase: `FOREIGN_KEY_CHECKS=0`, `UNIQUE_CHECKS=0`

### SQLite Notes

- SQLite enables `PRAGMA foreign_keys = ON` before operations
- Table rebuilds are used for structural changes since SQLite has limited `ALTER TABLE` support
- `OPTIMIZE` runs `PRAGMA optimize` instead of `OPTIMIZE TABLE`
- Default engine is `SQLite` and collation is `BINARY`

## Use Programmatically

### Using the Engine Interface (recommended)

```go
package main

import (
    "fmt"
    "os"

    "github.com/serhioromano/dbsync/msc/schema"
    "github.com/serhioromano/dbsync/msc/dbml"
    "github.com/serhioromano/dbsync/msc/mysql"
)

func main() {
    // --- Snapshot ---
    cfg := schema.Config{
        Engine:    "mysql",
        User:      "root",
        Pass:      "root",
        Host:      "localhost",
        Port:      "3306",
        DB:        "test",
        Prefix:    "",
        FilesPath: "./snash",
        File:      "test.dbml",
    }

    engine := &mysql.Engine{}
    sch, err := engine.Snapshot(cfg)
    if err != nil {
        panic(err)
    }

    // Write to file
    os.MkdirAll(cfg.FilesPath, 0755)
    f, _ := os.Create(cfg.FilesPath + "/" + cfg.File)
    defer f.Close()
    dbml.Write(f, sch)

    // --- Restore ---
    cfg2 := schema.Config{
        Engine:      "mysql",
        User:        "root",
        Pass:        "root",
        Host:        "localhost",
        Port:        "3306",
        DB:          "test_prod",
        Prefix:      "p_8_",
        FilesPath:   "./snash",
        File:        "test.dbml",
        DTable:      true,
        DColumn:     true,
        DIndex:      true,
        DConstraint: true,
        Optimize:    true,
    }

    dat, _ := os.ReadFile(cfg2.FilesPath + "/" + cfg2.File)
    sch2, _ := dbml.Parse(string(dat))

    engine2 := &mysql.Engine{}
    err = engine2.Restore(cfg2, sch2)
    if err != nil {
        panic(err)
    }
    fmt.Println("Done!")
}
```

### Using SQLite Engine

```go
import "github.com/serhioromano/dbsync/msc/sqlite"

engine := &sqlite.Engine{}
cfg := schema.Config{
    Engine:    "sqlite",
    DB:        "/path/to/database.sqlite",
    FilesPath: "./snash",
    File:      "test.dbml",
}
sch, err := engine.Snapshot(cfg)
```

### Backward-Compatible API

The original `msc.Snash()` and `msc.Restore{}.Run()` API is still available for MySQL:

```go
import "github.com/serhioromano/dbsync/msc"

options := msc.Config{
    User: "root", Pass: "root", Host: "localhost",
    Port: "3306", DB: "test",
    FilesPath: "./snash", File: "test.dbml",
}
msc.Snash(options)

runner := msc.Restore{}
runner.Run(options)
```

## Adding a New Database Engine

To add support for a new database (e.g., PostgreSQL):

1. Create a new package under `msc/` (e.g., `msc/postgres/`)
2. Implement the `schema.Engine` interface:
   ```go
   type Engine struct{}
   func (e *Engine) Snapshot(cfg schema.Config) (*schema.Schema, error) { ... }
   func (e *Engine) Restore(cfg schema.Config, schema *schema.Schema) error { ... }
   ```
3. Register it in `cmd/snash.go`'s `getEngine()` function

That's it — the DBML serialization, CLI flags, and diff logic are all handled by the shared packages.

## Known Quirks

- The command is spelled "snash" (not "snapshot") — this is intentional and used throughout the project
- Foreign key constraints require `fk_` prefix in their names
- `Optimize: true` only runs `OPTIMIZE TABLE` for InnoDB and MyISAM table types (MySQL)
- SQLite uses `PRAGMA optimize` instead
- Views, routines, and triggers are planned but not yet implemented
- DBML engine/collation/comment metadata is stored inside the table `Note` block using a `key: value | key: value` format
- SQLite column type changes require a full table rebuild (limited ALTER TABLE support)
