package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	s "github.com/serhioromano/dbsync/msc/schema"
)

// Engine implements s.Engine for SQLite.
type Engine struct {
	conn   *sql.DB
	prefix string
}

// Snapshot introspects a SQLite database and returns its Schema.
func (e *Engine) Snapshot(cfg s.Config) (*s.Schema, error) {
	conn, err := sql.Open("sqlite", cfg.DB)
	if err != nil {
		return nil, err
	}
	e.conn = conn
	e.prefix = cfg.Prefix

	fmt.Printf("Connected to SQLite: %s\n", cfg.DB)

	// Enable foreign keys
	e.conn.Exec("PRAGMA foreign_keys = ON")

	schema := &s.Schema{
		Name:   cfg.DB,
		Prefix: e.prefix,
	}

	tables, err := e.getTables()
	if err != nil {
		return nil, err
	}

	for _, tableName := range tables {
		shortName := strings.Replace(tableName, e.prefix, "", 1)

		fields, err := e.getFields(tableName)
		if err != nil {
			return nil, err
		}

		indexes, err := e.getIndexes(tableName)
		if err != nil {
			return nil, err
		}

		constraints, err := e.getConstraints(tableName)
		if err != nil {
			return nil, err
		}

		// Build TableDef
		td := s.TableDef{
			Name:       shortName,
			Engine:     "SQLite",
			Collation:  "BINARY",
			Comment:    "",
			PrimaryKey: "",
		}

		for _, f := range fields {
			fd := s.FieldDef{
				Name:       f.Name,
				ColumnType: f.Type,
				DataType:   extractDataType(f.Type),
				IsNullable: !f.NotNull,
				Comment:    "",
				IsPrimary:  f.PK > 0,
				IsAutoIncr: f.PK > 0 && strings.Contains(strings.ToUpper(f.Type), "INTEGER"),
			}
			if f.DefaultValue.Valid {
				val := f.DefaultValue.String
				fd.DefaultValue = &val
			}
			if fd.IsPrimary {
				td.PrimaryKey = fd.Name
			}
			td.Fields = append(td.Fields, fd)
		}

		// Indexes (exclude PK and FK-related)
		for _, idx := range indexes {
			if idx.Name == "PRIMARY" || strings.Contains(idx.Name, "fk_") {
				continue
			}

			cols := idx.Columns
			isUnique := idx.Unique

			// Merge single-column unique indexes into field
			if isUnique && len(cols) == 1 {
				for j := range td.Fields {
					if td.Fields[j].Name == cols[0] && !td.Fields[j].IsPrimary {
						td.Fields[j].IsUnique = true
					}
				}
				continue
			}

			td.Indexes = append(td.Indexes, s.IndexDef{
				Name:      idx.Name,
				Columns:   cols,
				IsUnique:  isUnique,
				IndexType: "BTREE",
			})
		}

		// Constraints
		for _, c := range constraints {
			refTable := strings.Replace(c.RefTable, e.prefix, "", 1)
			td.Constraints = append(td.Constraints, s.ConstraintDef{
				Name:              c.Name,
				ColumnName:        c.Column,
				RefTableName:      refTable,
				RefColumnName:     c.RefColumn,
				UpdateRule:        strings.ToUpper(c.UpdateRule),
				DeleteRule:        strings.ToUpper(c.DeleteRule),
			})
		}

		schema.Tables = append(schema.Tables, td)
	}

	return schema, nil
}

// Restore applies the given Schema to a SQLite database.
func (e *Engine) Restore(cfg s.Config, schema *s.Schema) error {
	conn, err := sql.Open("sqlite", cfg.DB)
	if err != nil {
		return err
	}
	e.conn = conn
	e.prefix = cfg.Prefix

	fmt.Printf("Connected to SQLite: %s\n", cfg.DB)

	e.conn.Exec("PRAGMA foreign_keys = ON")

	currentTables, err := e.getTables()
	if err != nil {
		return err
	}

	// Process each table from the schema
	for _, td := range schema.Tables {
		newName := e.prefix + td.Name

		if contains(currentTables, newName) {
			// Table exists: diff columns and indexes
			currentFields, _ := e.getFields(td.Name)
			for _, fd := range td.Fields {
				if fd.IsPrimary {
					continue
				}
				nul := ""
				if !fd.IsNullable {
					nul = "NOT NULL"
				}
				def := formatSQLiteDefault(fd)
				colSpec := fmt.Sprintf("`%s` %s %s %s", fd.Name, fd.ColumnType, nul, def)
				colSpec = strings.TrimSpace(colSpec)

				if cf, ok := findSQLiteField(currentFields, fd.Name); ok {
					// SQLite has limited ALTER TABLE; we can't easily change columns
					// Skip for now; full table rebuild would be needed for schema changes
					_ = cf
				} else {
					e.exec(fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN %s", newName, colSpec))
				}
			}

			// Indexes
			currentIndexes, _ := e.getIndexes(td.Name)
			for _, idx := range td.Indexes {
				if idx.Name == "PRIMARY" || strings.Contains(idx.Name, "fk_") {
					continue
				}
				if findSQLiteIndex(currentIndexes, idx.Name) {
					continue
				}
				uniqueStr := ""
				if idx.IsUnique {
					uniqueStr = "UNIQUE "
				}
				e.exec(fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS `%s` ON `%s` (%s)",
					uniqueStr, idx.Name, newName, strings.Join(idx.Columns, ", ")))
			}

			if cfg.DIndex {
				for _, ci := range currentIndexes {
					if ci.Name == "PRIMARY" || strings.Contains(ci.Name, "fk_") {
						continue
					}
					if !findIndexInSchema(td.Indexes, ci.Name) {
						e.silentExec(fmt.Sprintf("DROP INDEX IF EXISTS `%s`", ci.Name))
					}
				}
			}

		} else {
			// Create table
			var parts []string
			for _, fd := range td.Fields {
				if fd.IsPrimary {
					pk := "PRIMARY KEY"
					if fd.IsAutoIncr {
						pk += " AUTOINCREMENT"
					}
					parts = append(parts, fmt.Sprintf("`%s` %s %s", fd.Name, fd.ColumnType, pk))
					continue
				}
				nul := ""
				if !fd.IsNullable {
					nul = "NOT NULL"
				}
				def := formatSQLiteDefault(fd)
				colSpec := fmt.Sprintf("`%s` %s %s %s", fd.Name, fd.ColumnType, nul, def)
				parts = append(parts, strings.TrimSpace(colSpec))
			}

			for _, idx := range td.Indexes {
				if idx.Name == "PRIMARY" {
					continue // already handled in column
				}
				if isFKIndex(idx.Name, td.Constraints) {
					continue
				}
				uniqueStr := ""
				if idx.IsUnique {
					uniqueStr = "UNIQUE "
				}
				parts = append(parts, fmt.Sprintf("%s(`%s`)",
					strings.TrimSpace(uniqueStr),
					strings.Join(idx.Columns, "`, `")))
			}

			// Foreign keys inline in CREATE TABLE for SQLite
			for _, c := range td.Constraints {
				refTable := e.prefix + c.RefTableName
				parts = append(parts, fmt.Sprintf("FOREIGN KEY (`%s`) REFERENCES `%s`(`%s`) ON DELETE %s ON UPDATE %s",
					c.ColumnName, refTable, c.RefColumnName, c.DeleteRule, c.UpdateRule))
			}

			e.exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (\n\t%s\n)",
				newName, strings.Join(parts, ",\n\t")))
		}
	}

	// Drop extra tables (reverse order for FK dependencies)
	if cfg.DTable {
		for i := len(currentTables) - 1; i >= 0; i-- {
			if !findTableInSchema(schema.Tables, e.prefix, currentTables[i]) {
				e.exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", currentTables[i]))
			}
		}
	}

	if cfg.Optimize {
		e.silentExec("PRAGMA optimize")
	}

	return nil
}

// --- query helpers ---

type sqliteField struct {
	Name         string
	Type         string
	NotNull      bool
	DefaultValue sql.NullString
	PK           int
}

type sqliteIndex struct {
	Name    string
	Unique  bool
	Columns []string
}

type sqliteConstraint struct {
	Name       string
	Column     string
	RefTable   string
	RefColumn  string
	UpdateRule string
	DeleteRule string
}

func (e *Engine) getTables() ([]string, error) {
	rows, err := e.conn.Query("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE ? || '%'", e.prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		// Skip sqlite_ internal tables
		if strings.HasPrefix(name, "sqlite_") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func (e *Engine) getFields(table string) ([]sqliteField, error) {
	rows, err := e.conn.Query(fmt.Sprintf("PRAGMA table_info(`%s%s`)", e.prefix, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []sqliteField
	for rows.Next() {
		var f sqliteField
		var cid int
		if err := rows.Scan(&cid, &f.Name, &f.Type, &f.NotNull, &f.DefaultValue, &f.PK); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func (e *Engine) getIndexes(table string) ([]sqliteIndex, error) {
	// PRAGMA index_list
	listRows, err := e.conn.Query(fmt.Sprintf("PRAGMA index_list(`%s%s`)", e.prefix, table))
	if err != nil {
		return nil, err
	}
	defer listRows.Close()

	type idxMeta struct {
		Name   string
		Unique bool
	}
	var idxMetas []idxMeta
	for listRows.Next() {
		var im idxMeta
		var seq, unique int
		var origin, partial string
		if err := listRows.Scan(&seq, &im.Name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		im.Unique = unique == 1
		idxMetas = append(idxMetas, im)
	}

	var out []sqliteIndex
	for _, im := range idxMetas {
		infoRows, err := e.conn.Query(fmt.Sprintf("PRAGMA index_info(`%s`)", im.Name))
		if err != nil {
			return nil, err
		}
		var cols []string
		for infoRows.Next() {
			var seqno, cid int
			var colName string
			if err := infoRows.Scan(&seqno, &cid, &colName); err != nil {
				infoRows.Close()
				return nil, err
			}
			cols = append(cols, colName)
		}
		infoRows.Close()

		out = append(out, sqliteIndex{
			Name:    im.Name,
			Unique:  im.Unique,
			Columns: cols,
		})
	}
	return out, nil
}

func (e *Engine) getConstraints(table string) ([]sqliteConstraint, error) {
	rows, err := e.conn.Query(fmt.Sprintf("PRAGMA foreign_key_list(`%s%s`)", e.prefix, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []sqliteConstraint
	for rows.Next() {
		var c sqliteConstraint
		var id, seq int
		var onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &c.RefTable, &c.Column, &c.RefColumn,
			&onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}
		c.UpdateRule = onUpdate
		c.DeleteRule = onDelete
		c.Name = fmt.Sprintf("fk_%s_%s", table, c.Column)
		out = append(out, c)
	}
	return out, nil
}

// --- DDL execution ---

func (e *Engine) exec(sql string) {
	fmt.Println(sql)
	fmt.Println()
	_, err := e.conn.Exec(sql)
	if err != nil {
		panic(err.Error())
	}
}

func (e *Engine) silentExec(sql string) {
	fmt.Println(sql)
	fmt.Println()
	e.conn.Exec(sql)
}

// --- helpers ---

func contains(list []string, item string) bool {
	for _, s := range list {
		if s == item {
			return true
		}
	}
	return false
}

func findSQLiteField(fields []sqliteField, name string) (sqliteField, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return sqliteField{}, false
}

func findSQLiteIndex(indexes []sqliteIndex, name string) bool {
	for _, i := range indexes {
		if i.Name == name {
			return true
		}
	}
	return false
}

func findIndexInSchema(indexes []s.IndexDef, name string) bool {
	for _, i := range indexes {
		if i.Name == name {
			return true
		}
	}
	return false
}

func findTableInSchema(tables []s.TableDef, prefix, name string) bool {
	for _, t := range tables {
		if prefix+t.Name == name {
			return true
		}
	}
	return false
}

func isFKIndex(name string, constraints []s.ConstraintDef) bool {
	for _, c := range constraints {
		if strings.Contains(c.Name, name) {
			return true
		}
	}
	return false
}

func formatSQLiteDefault(fd s.FieldDef) string {
	if fd.DefaultValue == nil {
		return ""
	}
	def := *fd.DefaultValue
	if def == "" {
		return ""
	}
	dt := strings.ToLower(fd.DataType)
	if dt == "text" || dt == "blob" || strings.Contains(dt, "text") || strings.Contains(dt, "blob") {
		return ""
	}
	if isSQLiteNumeric(dt) {
		return "DEFAULT " + def
	}
	return "DEFAULT '" + strings.ReplaceAll(def, "'", "''") + "'"
}

func isSQLiteNumeric(dt string) bool {
	switch dt {
	case "int", "integer", "bigint", "smallint", "tinyint", "mediumint",
		"float", "double", "decimal", "numeric", "real", "boolean":
		return true
	}
	return false
}

func extractDataType(colType string) string {
	paren := strings.Index(colType, "(")
	if paren >= 0 {
		return strings.ToLower(strings.TrimSpace(colType[:paren]))
	}
	space := strings.Index(colType, " ")
	if space >= 0 {
		return strings.ToLower(strings.TrimSpace(colType[:space]))
	}
	return strings.ToLower(strings.TrimSpace(colType))
}
