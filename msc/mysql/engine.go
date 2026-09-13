package mysql

import (
	"database/sql"
	"fmt"
	"strings"

	s "github.com/serhioromano/dbsync/msc/schema"
)

// Engine implements s.Engine for MySQL.
type Engine struct {
	conn   *sql.DB
	prefix string
	schema string
}

// Snapshot introspects a MySQL database and returns its Schema.
func (e *Engine) Snapshot(cfg s.Config) (*s.Schema, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
		cfg.User, cfg.Pass, cfg.Host, cfg.Port, cfg.DB)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	e.conn = conn
	e.prefix = cfg.Prefix
	e.schema = cfg.DB

	fmt.Printf("Connected on: %s\n", dsn)

	schema := &s.Schema{
		Name:   e.schema,
		Prefix: e.prefix,
	}

	rawTables, err := e.getTables()
	if err != nil {
		return nil, err
	}

	for _, rt := range rawTables {
		tableName := strings.Replace(rt.Name, e.prefix, "", 1)

		fields, err := e.getFields(rt.Name)
		if err != nil {
			return nil, err
		}

		indexes, err := e.getIndexes(rt.Name)
		if err != nil {
			return nil, err
		}

		constraints, err := e.getConstraints(rt.Name)
		if err != nil {
			return nil, err
		}

		// Find primary key
		primaryKey := ""
		for _, idx := range indexes {
			if idx.Key_name == "PRIMARY" {
				primaryKey = idx.Column_name
				break
			}
		}

		// Build TableDef
		td := s.TableDef{
			Name:       tableName,
			Engine:     rt.Engine,
			Collation:  rt.Collation,
			Comment:    rt.Comment,
			PrimaryKey: primaryKey,
		}

		for _, f := range fields {
			fd := s.FieldDef{
				Name:       f.COLUMN_NAME,
				ColumnType: f.COLUMN_TYPE,
				DataType:   f.DATA_TYPE,
				IsNullable: f.IS_NULLABLE == "YES",
				Comment:    f.COLUMN_COMMENT,
				IsPrimary:  f.COLUMN_NAME == primaryKey,
				IsAutoIncr: strings.Contains(f.EXTRA, "auto_increment"),
			}
			if f.COLUMN_DEFAULT != nil {
				fd.DefaultValue = f.COLUMN_DEFAULT
			}
			td.Fields = append(td.Fields, fd)
		}

		// Indexes: collect by name, skip PK and FK
		type idxInfo struct {
			name    string
			cols    []string
			unique  bool
			idxType string
		}
		idxMap := make(map[string]*idxInfo)
		for _, idx := range indexes {
			if idx.Key_name == "PRIMARY" || strings.Contains(idx.Key_name, "fk_") {
				continue
			}
			if _, ok := idxMap[idx.Key_name]; !ok {
				idxMap[idx.Key_name] = &idxInfo{
					name:    idx.Key_name,
					unique:  idx.Non_unique == "0",
					idxType: idx.Index_type,
				}
			}
			idxMap[idx.Key_name].cols = append(idxMap[idx.Key_name].cols, idx.Column_name)
		}

		// Merge single-column unique indexes into field definitions
		for _, ii := range idxMap {
			if ii.unique && len(ii.cols) == 1 {
				for j := range td.Fields {
					if td.Fields[j].Name == ii.cols[0] && !td.Fields[j].IsPrimary {
						td.Fields[j].IsUnique = true
					}
				}
				continue // Don't add to separate indexes list
			}
			td.Indexes = append(td.Indexes, s.IndexDef{
				Name:      ii.name,
				Columns:   ii.cols,
				IsUnique:  ii.unique,
				IndexType: ii.idxType,
			})
		}

		// Constraints
		for _, c := range constraints {
			refTable := strings.Replace(c.REFERENCED_TABLE_NAME, e.prefix, "", 1)
			td.Constraints = append(td.Constraints, s.ConstraintDef{
				Name:              c.CONSTRAINT_NAME,
				ColumnName:        c.COLUMN_NAME,
				RefTableName:      refTable,
				RefColumnName:     c.REFERENCED_COLUMN_NAME,
				UpdateRule:        c.UPDATE_RULE,
				DeleteRule:        c.DELETE_RULE,
			})
		}

		schema.Tables = append(schema.Tables, td)
	}

	return schema, nil
}

// Restore applies the given Schema to a MySQL database.
func (e *Engine) Restore(cfg s.Config, schema *s.Schema) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
		cfg.User, cfg.Pass, cfg.Host, cfg.Port, cfg.DB)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	e.conn = conn
	e.prefix = cfg.Prefix
	e.schema = cfg.DB

	fmt.Printf("Connected on: %s\n", dsn)

	e.exec("USE " + e.schema)
	e.exec("SET @@session.sql_mode =\"ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION\"")

	currentRaw, err := e.getTables()
	if err != nil {
		return err
	}

	var currentTables []currentTable
	for _, t := range currentRaw {
		currentTables = append(currentTables, currentTable{
			Name: t.Name, Engine: t.Engine, Coll: t.Collation, Comment: t.Comment,
		})
	}

	// Process each table from the schema
	for _, td := range schema.Tables {
		newName := e.prefix + td.Name

		if ct, ok := findCurrentTable(currentTables, newName); ok {
			// Update existing table
			if ct.Coll != td.Collation || ct.Engine != td.Engine || ct.Comment != td.Comment {
				e.exec(fmt.Sprintf("ALTER TABLE `%s` COLLATE = %s, ENGINE = %s, COMMENT = '%s'",
					newName, td.Collation, td.Engine, escapeSQL(td.Comment)))
			}

			// Fields
			currentFields, _ := e.getFields(td.Name)
			for _, fd := range td.Fields {
				if fd.IsPrimary {
					continue
				}
				nul := "NULL"
				if !fd.IsNullable {
					nul = "NOT NULL"
				}
				def := formatDefault(fd)
				colSpec := fmt.Sprintf("`%s` %s %s %s COMMENT '%s'",
					fd.Name, fd.ColumnType, nul, def, escapeSQL(fd.Comment))

				if cf, cfOk := findCurrentField(currentFields, fd.Name); cfOk {
					cfNull := "YES"
					if !fd.IsNullable {
						cfNull = "NO"
					}
					cfDef := ""
					if cf.COLUMN_DEFAULT != nil {
						cfDef = *cf.COLUMN_DEFAULT
					}
					if cf.IS_NULLABLE != cfNull || cf.COLUMN_TYPE != fd.ColumnType ||
						cfDef != defVal(fd) || cf.COLUMN_COMMENT != fd.Comment {
						e.exec(fmt.Sprintf("ALTER TABLE `%s` CHANGE COLUMN `%s` %s",
							newName, fd.Name, colSpec))
					}
				} else {
					e.exec(fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN %s", newName, colSpec))
				}
			}

			if cfg.DColumn {
				for _, cf := range currentFields {
					if !findFieldInSchema(td.Fields, cf.COLUMN_NAME) {
						e.exec(fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", newName, cf.COLUMN_NAME))
					}
				}
			}

			// Indexes
			currentIndexes, _ := e.getIndexes(td.Name)
			for _, idx := range td.Indexes {
				if idx.Name == "PRIMARY" || strings.Contains(idx.Name, "fk_") {
					continue
				}
				if _, ok := findCurrentIndex(currentIndexes, idx.Name); ok {
					continue
				}
				cols := strings.Join(idx.Columns, " ASC, ")
				e.exec(fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `%s` (%s ASC)", newName, idx.Name, cols))
			}

			if cfg.DIndex {
				for _, ci := range currentIndexes {
					if ci.Key_name == "PRIMARY" || strings.Contains(ci.Key_name, "fk_") {
						continue
					}
					if !findIndexInSchema(td.Indexes, ci.Key_name) {
						e.silentExec(fmt.Sprintf("DROP INDEX `%s` ON `%s`", ci.Key_name, newName))
					}
				}
			}

		} else {
			// Create new table
			var parts []string
			for _, fd := range td.Fields {
				if fd.IsPrimary {
					parts = append(parts, fmt.Sprintf("`%s` %s NULL AUTO_INCREMENT", fd.Name, fd.ColumnType))
					continue
				}
				nul := "NULL"
				if !fd.IsNullable {
					nul = "NOT NULL"
				}
				def := formatDefault(fd)
				parts = append(parts, fmt.Sprintf("`%s` %s %s %s COMMENT '%s'",
					fd.Name, fd.ColumnType, nul, def, escapeSQL(fd.Comment)))
			}
			for _, idx := range td.Indexes {
				if idx.Name == "PRIMARY" {
					parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(idx.Columns, ", ")))
					continue
				}
				if isFKIndex(idx.Name, td.Constraints) {
					continue
				}
				key := "KEY"
				if strings.ToUpper(idx.IndexType) == "FULLTEXT" {
					key = "FULLTEXT KEY"
				}
				if idx.IsUnique {
					key = "UNIQUE KEY"
				}
				parts = append(parts, fmt.Sprintf("%s `%s` (%s)", key, idx.Name, strings.Join(idx.Columns, ", ")))
			}
			e.exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (\n\t%s\n) ENGINE=%s COLLATE=%s COMMENT='%s'",
				newName, strings.Join(parts, ",\n\t"), td.Engine, td.Collation, escapeSQL(td.Comment)))
		}
	}

	// Drop extra tables
	if cfg.DTable {
		for _, ct := range currentTables {
			if !findTableInSchema(schema.Tables, e.prefix, ct.Name) {
				e.exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", ct.Name))
			}
		}
	}

	// Constraints phase
	e.silentExec("SET @@session.UNIQUE_CHECKS=0")
	e.silentExec("SET @@session.FOREIGN_KEY_CHECKS=0")
	e.silentExec("SET @@session.sql_mode='ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'")

	for _, td := range schema.Tables {
		newName := e.prefix + td.Name
		currentConstraints, _ := e.getConstraints(td.Name)

		for _, cc := range currentConstraints {
			if cfg.DConstraint && !findConstraintInSchema(td.Constraints, cc.CONSTRAINT_NAME, schema.Prefix, e.prefix) {
				e.silentExec(fmt.Sprintf("ALTER TABLE `%s` DROP FOREIGN KEY `%s`", newName, cc.CONSTRAINT_NAME))
			}
		}
		for _, c := range td.Constraints {
			if findConstraintInCurrent(currentConstraints, c.Name, schema.Prefix, e.prefix) {
				continue
			}
			refTable := e.prefix + c.RefTableName
			e.exec(fmt.Sprintf("ALTER TABLE `%s` ADD CONSTRAINT `%s` FOREIGN KEY (`%s`) REFERENCES `%s` (`%s`) ON DELETE %s ON UPDATE %s",
				newName, e.prefix+c.Name, c.ColumnName, refTable, c.RefColumnName, c.DeleteRule, c.UpdateRule))
		}

		if cfg.Optimize && (td.Engine == "InnoDB" || td.Engine == "MyISAM") {
			e.silentExec("OPTIMIZE TABLE " + newName)
		}
	}

	return nil
}

// --- query helpers ---

type rawTable struct {
	Name, Engine, Collation, Comment string
}

func (e *Engine) getTables() ([]rawTable, error) {
	rows, err := e.conn.Query(fmt.Sprintf("SHOW TABLE STATUS LIKE '%s%%'", e.prefix))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []rawTable
	for rows.Next() {
		var rt rawTable
		var name, engine, collation, comment sql.NullString
		var version, rowFormat, rowsCount, avgRowLen, dataLen, maxDataLen, idxLen, dataFree sql.NullInt64
		var autoIncr, createTime, updateTime, checkTime, checksum, createOpts sql.NullString
		if err := rows.Scan(&name, &engine, &version, &rowFormat, &rowsCount, &avgRowLen,
			&dataLen, &maxDataLen, &idxLen, &dataFree, &autoIncr, &createTime,
			&updateTime, &checkTime, &collation, &checksum, &createOpts, &comment); err != nil {
			return nil, err
		}
		rt.Name = name.String
		rt.Engine = engine.String
		rt.Collation = collation.String
		rt.Comment = comment.String
		out = append(out, rt)
	}
	return out, nil
}

type rawField struct {
	ORDINAL_POSITION, COLUMN_COMMENT, COLUMN_NAME string
	COLUMN_DEFAULT                                 *string
	IS_NULLABLE, DATA_TYPE, COLUMN_TYPE            string
	EXTRA, COLUMN_KEY                              string
}

func (e *Engine) getFields(table string) ([]rawField, error) {
	rows, err := e.conn.Query(fmt.Sprintf(
		"SELECT ORDINAL_POSITION, COLUMN_COMMENT, `COLUMN_NAME`, COLUMN_DEFAULT, "+
			"IS_NULLABLE, DATA_TYPE, COLUMN_TYPE, EXTRA, COLUMN_KEY "+
			"FROM INFORMATION_SCHEMA.COLUMNS "+
			"WHERE TABLE_NAME = '%s%s' AND TABLE_SCHEMA = '%s'",
		e.prefix, table, e.schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []rawField
	for rows.Next() {
		var rf rawField
		if err := rows.Scan(&rf.ORDINAL_POSITION, &rf.COLUMN_COMMENT, &rf.COLUMN_NAME,
			&rf.COLUMN_DEFAULT, &rf.IS_NULLABLE, &rf.DATA_TYPE, &rf.COLUMN_TYPE,
			&rf.EXTRA, &rf.COLUMN_KEY); err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, nil
}

type rawIndex struct {
	Table, Non_unique, Key_name, Seq_in_index, Column_name string
	Collation, Cardinality, Sub_part, Packed               *string
	Null, Index_type, Comment, Index_comment               string
}

func (e *Engine) getIndexes(table string) ([]rawIndex, error) {
	rows, err := e.conn.Query(fmt.Sprintf("SHOW INDEXES FROM `%s%s`", e.prefix, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []rawIndex
	for rows.Next() {
		var ri rawIndex
		if err := rows.Scan(&ri.Table, &ri.Non_unique, &ri.Key_name, &ri.Seq_in_index,
			&ri.Column_name, &ri.Collation, &ri.Cardinality, &ri.Sub_part,
			&ri.Packed, &ri.Null, &ri.Index_type, &ri.Comment, &ri.Index_comment); err != nil {
			return nil, err
		}
		out = append(out, ri)
	}
	return out, nil
}

type rawConstraint struct {
	UPDATE_RULE, DELETE_RULE, CONSTRAINT_NAME, COLUMN_NAME            string
	REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME                     string
}

func (e *Engine) getConstraints(table string) ([]rawConstraint, error) {
	rows, err := e.conn.Query(fmt.Sprintf(
		"SELECT rc.UPDATE_RULE, rc.DELETE_RULE, kc.CONSTRAINT_NAME, "+
			"kc.COLUMN_NAME, kc.REFERENCED_TABLE_NAME, kc.REFERENCED_COLUMN_NAME "+
			"FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kc "+
			"LEFT JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc ON rc.CONSTRAINT_NAME = kc.CONSTRAINT_NAME "+
			"WHERE kc.TABLE_NAME = '%s%s' AND kc.CONSTRAINT_NAME <> 'PRIMARY' "+
			"AND rc.CONSTRAINT_SCHEMA = '%s' AND kc.TABLE_SCHEMA = '%s'",
		e.prefix, table, e.schema, e.schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []rawConstraint
	for rows.Next() {
		var rc rawConstraint
		if err := rows.Scan(&rc.UPDATE_RULE, &rc.DELETE_RULE, &rc.CONSTRAINT_NAME,
			&rc.COLUMN_NAME, &rc.REFERENCED_TABLE_NAME, &rc.REFERENCED_COLUMN_NAME); err != nil {
			return nil, err
		}
		out = append(out, rc)
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
	e.conn.Exec(sql) // ignore errors silently
}

// --- helper functions ---

type currentTable struct {
	Name    string
	Engine  string
	Coll    string
	Comment string
}

func findCurrentTable(tables []currentTable, name string) (currentTable, bool) {
	for _, t := range tables {
		if t.Name == name {
			return t, true
		}
	}
	return currentTable{}, false
}

func findTableInSchema(tables []s.TableDef, prefix, name string) bool {
	for _, t := range tables {
		if prefix+t.Name == name {
			return true
		}
	}
	return false
}

func findCurrentField(fields []rawField, name string) (rawField, bool) {
	for _, f := range fields {
		if f.COLUMN_NAME == name {
			return f, true
		}
	}
	return rawField{}, false
}

func findFieldInSchema(fields []s.FieldDef, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func findCurrentIndex(indexes []rawIndex, name string) (rawIndex, bool) {
	for _, i := range indexes {
		if i.Key_name == name {
			return i, true
		}
	}
	return rawIndex{}, false
}

func findIndexInSchema(indexes []s.IndexDef, name string) bool {
	for _, i := range indexes {
		if i.Name == name {
			return true
		}
	}
	return false
}

func findConstraintInSchema(constraints []s.ConstraintDef, name, oldPrefix, newPrefix string) bool {
	cname := strings.Replace(name, newPrefix, "", 1)
	for _, c := range constraints {
		iname := strings.Replace(c.Name, oldPrefix, "", 1)
		if cname == iname {
			return true
		}
	}
	return false
}

func findConstraintInCurrent(constraints []rawConstraint, name, oldPrefix, newPrefix string) bool {
	iname := strings.Replace(name, oldPrefix, "", 1)
	for _, c := range constraints {
		cname := strings.Replace(c.CONSTRAINT_NAME, newPrefix, "", 1)
		if cname == iname {
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

func defVal(fd s.FieldDef) string {
	if fd.DefaultValue == nil {
		return ""
	}
	return *fd.DefaultValue
}

func formatDefault(fd s.FieldDef) string {
	if fd.DefaultValue == nil {
		return ""
	}
	def := *fd.DefaultValue
	if def == "" {
		return ""
	}
	dt := strings.ToLower(fd.DataType)
	if dt == "text" || dt == "mediumtext" || dt == "longtext" || dt == "tinytext" ||
		dt == "blob" || dt == "tinyblob" || dt == "mediumblob" || dt == "longblob" {
		return ""
	}
	if dt == "int" && def == "" && !fd.IsNullable {
		return "DEFAULT '0'"
	}
	if dt != "datetime" && dt != "timestamp" {
		return "DEFAULT '" + escapeSQL(def) + "'"
	}
	return ""
}

func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}
