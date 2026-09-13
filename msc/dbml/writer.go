package dbml

import (
	"fmt"
	"io"
	"strings"

	"github.com/serhioromano/dbsync/msc/schema"
)

// Write serializes a Schema to DBML format and writes it to w.
func Write(w io.Writer, schema *schema.Schema) error {
	// Header
	if _, err := fmt.Fprintf(w, "// Schema: %s\n", schema.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "// Prefix: %s\n", schema.Prefix); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}

	for _, table := range schema.Tables {
		if err := writeTable(w, table); err != nil {
			return err
		}
	}

	// Write References
	for _, table := range schema.Tables {
		for _, c := range table.Constraints {
			if err := writeRef(w, table.Name, c); err != nil {
				return err
			}
		}
	}

	return nil
}

func writeTable(w io.Writer, table schema.TableDef) error {
	escName := esc(table.Name)
	if _, err := fmt.Fprintf(w, "Table \"%s\" {\n", escName); err != nil {
		return err
	}

	// Fields
	for _, f := range table.Fields {
		settings := []string{}

		if f.IsPrimary {
			settings = append(settings, "pk")
			if f.IsAutoIncr {
				settings = append(settings, "increment")
			}
		}
		if f.IsNullable {
			settings = append(settings, "null")
		} else {
			settings = append(settings, "not null")
		}
		if f.IsUnique {
			settings = append(settings, "unique")
		}
		if f.DefaultValue != nil && *f.DefaultValue != "" {
			def := *f.DefaultValue
			if isNumericType(f.DataType) {
				settings = append(settings, fmt.Sprintf("default: %s", def))
			} else {
				settings = append(settings, fmt.Sprintf("default: `%s`", strings.ReplaceAll(def, "`", "\\`")))
			}
		}
		if f.Comment != "" {
			settings = append(settings, fmt.Sprintf("note: '%s'", strings.ReplaceAll(f.Comment, "'", "\\'")))
		}

		settingsStr := ""
		if len(settings) > 0 {
			settingsStr = " [" + strings.Join(settings, ", ") + "]"
		}

		if _, err := fmt.Fprintf(w, "  \"%s\" %s%s\n",
			esc(f.Name), f.ColumnType, settingsStr); err != nil {
			return err
		}
	}

	// Indexes block (non-PK, non-FK, non-single-column-unique)
	var indexEntries []string
	for _, idx := range table.Indexes {
		// Skip single-column unique already inline on field
		if idx.IsUnique && len(idx.Columns) == 1 {
			continue
		}
		idxSettings := []string{fmt.Sprintf("name: \"%s\"", idx.Name)}
		switch strings.ToUpper(idx.IndexType) {
		case "FULLTEXT":
			idxSettings = append(idxSettings, "type: fulltext")
		case "HASH":
			idxSettings = append(idxSettings, "type: hash")
		default:
			idxSettings = append(idxSettings, "type: btree")
		}
		if idx.IsUnique {
			idxSettings = append(idxSettings, "unique")
		}

		var cols []string
		for _, c := range idx.Columns {
			cols = append(cols, esc(c))
		}
		indexEntries = append(indexEntries, fmt.Sprintf("    (%s) [%s]",
			strings.Join(cols, ", "), strings.Join(idxSettings, ", ")))
	}

	if len(indexEntries) > 0 {
		if _, err := fmt.Fprint(w, "\n  Indexes {\n"); err != nil {
			return err
		}
		for _, e := range indexEntries {
			if _, err := fmt.Fprintln(w, e); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(w, "  }\n"); err != nil {
			return err
		}
	}

	// Note with table metadata
	noteParts := []string{}
	if table.Engine != "" {
		noteParts = append(noteParts, fmt.Sprintf("Engine: %s", table.Engine))
	}
	if table.Collation != "" {
		noteParts = append(noteParts, fmt.Sprintf("Collation: %s", table.Collation))
	}
	if table.Comment != "" {
		noteParts = append(noteParts, fmt.Sprintf("Comment: %s", table.Comment))
	}
	if len(noteParts) > 0 {
		if _, err := fmt.Fprintf(w, "\n  Note: '''%s'''\n", strings.Join(noteParts, " | ")); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprint(w, "}\n\n"); err != nil {
		return err
	}
	return nil
}

func writeRef(w io.Writer, fromTable string, c schema.ConstraintDef) error {
	refSettings := []string{}
	if c.DeleteRule != "" && strings.ToUpper(c.DeleteRule) != "NO ACTION" {
		refSettings = append(refSettings, fmt.Sprintf("delete: %s", strings.ToLower(c.DeleteRule)))
	}
	if c.UpdateRule != "" && strings.ToUpper(c.UpdateRule) != "NO ACTION" {
		refSettings = append(refSettings, fmt.Sprintf("update: %s", strings.ToLower(c.UpdateRule)))
	}
	refSettingsStr := ""
	if len(refSettings) > 0 {
		refSettingsStr = " [" + strings.Join(refSettings, ", ") + "]"
	}

	_, err := fmt.Fprintf(w, "Ref: \"%s\".\"%s\" > \"%s\".\"%s\"%s\n",
		esc(fromTable), esc(c.ColumnName),
		esc(c.RefTableName), esc(c.RefColumnName),
		refSettingsStr)
	return err
}

func esc(name string) string {
	return strings.ReplaceAll(name, "\"", "\\\"")
}

func isNumericType(dt string) bool {
	switch strings.ToLower(dt) {
	case "int", "bigint", "smallint", "tinyint", "mediumint",
		"float", "double", "decimal", "numeric", "real":
		return true
	}
	return false
}
