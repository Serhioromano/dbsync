package dbml

import (
	"fmt"
	"strings"

	s "github.com/serhioromano/dbsync/msc/schema"
)

// Parse parses a DBML document string and returns a Schema.
// Handles both our canonical format and dbdiagram.io's full DBML dialect.
func Parse(input string) (*s.Schema, error) {
	schema := &s.Schema{}

	lines := strings.Split(input, "\n")

	type refEntry struct {
		refName    string
		fromTable  string
		fromCol    string
		toTable    string
		toCol      string
		deleteRule string
		updateRule string
	}
	var refs []refEntry

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Strip inline // comments
		if idx := findUnquoted(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		// Header comments
		if strings.HasPrefix(line, "//") {
			comment := strings.TrimSpace(line[2:])
			if strings.HasPrefix(comment, "Schema:") {
				schema.Name = strings.TrimSpace(comment[7:])
			} else if strings.HasPrefix(comment, "Prefix:") {
				schema.Prefix = strings.TrimSpace(comment[7:])
			}
			i++
			continue
		}

		if line == "" {
			i++
			continue
		}

		// Table definition
		if strings.HasPrefix(line, "Table ") {
			table, consumed, err := parseTable(lines, i)
			if err != nil {
				return nil, err
			}
			schema.Tables = append(schema.Tables, table)
			i = consumed
			continue
		}

		// Ref line — two formats:
		//   Ref: "table"."col" > "table"."col" [settings]
		//   Ref name: table.col > table.col  (dbdiagram.io)
		if strings.HasPrefix(line, "Ref:") || strings.HasPrefix(line, "Ref ") {
			ref, err := parseRefLine(line)
			if err != nil {
				return nil, fmt.Errorf("error parsing Ref at line %d: %v", i+1, err)
			}
			refs = append(refs, ref)
			i++
			continue
		}

		// Records block — skip entirely
		if strings.HasPrefix(line, "Records ") {
			i = skipBlock(lines, i)
			continue
		}

		// Enum block — skip
		if strings.HasPrefix(line, "Enum ") {
			i = skipBlock(lines, i)
			continue
		}

		// TableGroup / Project / Note at top level — skip
		if strings.HasPrefix(line, "TableGroup ") ||
			strings.HasPrefix(line, "Project ") ||
			strings.HasPrefix(line, "Note:") {
			if strings.Contains(line, "{") {
				i = skipBlock(lines, i)
			} else {
				i++
			}
			continue
		}

		i++
	}

	// Attach refs as constraints to their source tables
	for _, ref := range refs {
		for idx := range schema.Tables {
			if schema.Tables[idx].Name == ref.fromTable {
				constraintName := ref.refName
		if constraintName == "" {
			constraintName = fmt.Sprintf("fk_%s_%s", ref.fromTable, ref.fromCol)
		}
				schema.Tables[idx].Constraints = append(schema.Tables[idx].Constraints, s.ConstraintDef{
					Name:              constraintName,
					ColumnName:        ref.fromCol,
					RefTableName:      ref.toTable,
					RefColumnName:     ref.toCol,
					UpdateRule:        strings.ToUpper(ref.updateRule),
					DeleteRule:        strings.ToUpper(ref.deleteRule),
				})
			}
		}
	}

	return schema, nil
}

// skipBlock skips a brace-delimited block starting at start.
// Returns the line index after the closing brace.
func skipBlock(lines []string, start int) int {
	if strings.Contains(lines[start], "{") && strings.Contains(lines[start], "}") {
		return start + 1
	}
	depth := 0
	i := start
	for i < len(lines) {
		for _, ch := range lines[i] {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
			}
		}
		i++
		if depth == 0 {
			break
		}
	}
	return i
}

func parseTable(lines []string, start int) (s.TableDef, int, error) {
	table := s.TableDef{
		Engine:    "InnoDB",
		Collation: "utf8_general_ci",
	}

	headerLine := strings.TrimSpace(lines[start])
	rest := strings.TrimPrefix(headerLine, "Table ")
	rest = strings.TrimSpace(rest)
	// Remove trailing {
	rest = strings.TrimSuffix(rest, " {")
	rest = strings.TrimSuffix(rest, "{")
	rest = strings.TrimSpace(rest)

	// Table name: either "name" or bare name
	table.Name = strings.Trim(rest, "\"")

	i := start + 1
	inIndexes := false
	inNote := false
	noteLines := []string{}

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Strip inline // comments
		if idx := findUnquoted(trimmed, "//"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}

		// Handle Note block (multi-line)
		if inNote {
			if strings.Contains(trimmed, "'''") {
				endIdx := strings.Index(trimmed, "'''")
				if endIdx > 0 {
					noteLines = append(noteLines, trimmed[:endIdx])
				}
				parseTableNote(&table, strings.Join(noteLines, "\n"))
				inNote = false
				noteLines = nil
				i++
				continue
			}
			noteLines = append(noteLines, trimmed)
			i++
			continue
		}

		if trimmed == "}" {
			i++
			break
		}

		if strings.HasPrefix(trimmed, "Indexes {") || trimmed == "Indexes {" {
			inIndexes = true
			i++
			continue
		}

		if inIndexes && trimmed == "}" {
			inIndexes = false
			i++
			continue
		}

		if inIndexes {
			idxDef, err := parseIndexEntry(trimmed)
			if err == nil {
				table.Indexes = append(table.Indexes, idxDef)
			}
			i++
			continue
		}

		// Note block start
		if strings.HasPrefix(trimmed, "Note:") && strings.Contains(trimmed, "'''") {
			inNote = true
			afterNote := trimmed[strings.Index(trimmed, "'''")+3:]
			if strings.Contains(afterNote, "'''") {
				endIdx := strings.Index(afterNote, "'''")
				parseTableNote(&table, strings.TrimSpace(afterNote[:endIdx]))
				inNote = false
				i++
				continue
			}
			if afterNote != "" {
				noteLines = append(noteLines, afterNote)
			}
			i++
			continue
		}

		// Skip empty lines
		if trimmed == "" {
			i++
			continue
		}

		// Column definition — either "name" type [...] or name type [...]
		fd, err := parseColumnDef(trimmed)
		if err == nil {
			if fd.IsPrimary {
				table.PrimaryKey = fd.Name
				table.Indexes = append(table.Indexes, s.IndexDef{
					Name:      "PRIMARY",
					Columns:   []string{fd.Name},
					IsUnique:  true,
					IndexType: "BTREE",
				})
			}
			if fd.IsUnique && !fd.IsPrimary {
				table.Indexes = append(table.Indexes, s.IndexDef{
					Name:      fd.Name,
					Columns:   []string{fd.Name},
					IsUnique:  true,
					IndexType: "BTREE",
				})
			}
			table.Fields = append(table.Fields, fd)
			i++
			continue
		}

		i++
	}

	return table, i, nil
}

func parseColumnDef(line string) (s.FieldDef, error) {
	fd := s.FieldDef{IsNullable: true}
	trimmed := strings.TrimSpace(line)

	// Extract name and rest. Name is either quoted or bare.
	name := ""
	rest := ""

	if strings.HasPrefix(trimmed, "\"") {
		qEnd := strings.Index(trimmed[1:], "\"")
		if qEnd == -1 {
			return fd, fmt.Errorf("unterminated field name in: %s", trimmed)
		}
		name = trimmed[1 : 1+qEnd]
		rest = trimmed[1+qEnd+1:]
	} else {
		// Bare name: first whitespace-delimited token
		space := strings.Index(trimmed, " ")
		if space == -1 {
			return fd, fmt.Errorf("expected column definition: %s", trimmed)
		}
		name = trimmed[:space]
		rest = trimmed[space:]
	}
	fd.Name = name
	rest = strings.TrimSpace(rest)

	// Extract column type and settings
	colType := ""
	var settings []string

	bracketIdx := strings.Index(rest, "[")
	if bracketIdx >= 0 && strings.HasSuffix(strings.TrimSpace(rest), "]") {
		colType = strings.TrimSpace(rest[:bracketIdx])
		settings = parseSettings(rest[bracketIdx+1 : len(rest)-1])
	} else {
		colType = strings.TrimSpace(rest)
	}

	fd.ColumnType = colType
	fd.DataType = extractDataType(colType)

	for _, setting := range settings {
		switch {
		case setting == "pk" || setting == "primary key":
			fd.IsPrimary = true
		case setting == "increment" || setting == "auto increment":
			fd.IsAutoIncr = true
		case setting == "not null":
			fd.IsNullable = false
		case setting == "null":
			fd.IsNullable = true
		case setting == "unique":
			fd.IsUnique = true
		case strings.HasPrefix(setting, "default:"):
			defVal := strings.TrimSpace(setting[8:])
			if len(defVal) >= 2 && (defVal[0] == '`' || defVal[0] == '\'') &&
				defVal[len(defVal)-1] == defVal[0] {
				defVal = defVal[1 : len(defVal)-1]
			}
			if defVal != "" {
				fd.DefaultValue = &defVal
			}
		case strings.HasPrefix(setting, "note:"):
			comment := strings.TrimSpace(setting[5:])
			if len(comment) >= 2 && comment[0] == '\'' && comment[len(comment)-1] == '\'' {
				comment = comment[1 : len(comment)-1]
				comment = strings.ReplaceAll(comment, "\\'", "'")
			}
			fd.Comment = comment
		}
	}

	return fd, nil
}

func parseIndexEntry(line string) (s.IndexDef, error) {
	idx := s.IndexDef{IndexType: "BTREE"}
	trimmed := strings.TrimSpace(line)

	parenStart := strings.Index(trimmed, "(")
	parenEnd := strings.Index(trimmed, ")")
	if parenStart == -1 || parenEnd == -1 {
		return idx, fmt.Errorf("invalid index entry: %s", trimmed)
	}

	colsStr := trimmed[parenStart+1 : parenEnd]
	for _, col := range strings.Split(colsStr, ",") {
		col = strings.TrimSpace(col)
		col = strings.Trim(col, "\"")
		if col != "" {
			idx.Columns = append(idx.Columns, col)
		}
	}

	settingsStr := ""
	bracketStart := strings.Index(trimmed[parenEnd:], "[")
	if bracketStart >= 0 {
		bracketEnd := strings.LastIndex(trimmed, "]")
		if bracketEnd > parenEnd {
			settingsStr = trimmed[parenEnd+bracketStart+1 : bracketEnd]
		}
	}

	for _, setting := range parseSettings(settingsStr) {
		setting = strings.TrimSpace(setting)
		switch {
		case strings.HasPrefix(setting, "name:"):
			idx.Name = strings.Trim(strings.TrimSpace(setting[5:]), "\"")
		case strings.HasPrefix(setting, "type:"):
			t := strings.ToUpper(strings.TrimSpace(setting[5:]))
			if t == "FULLTEXT" || t == "HASH" {
				idx.IndexType = t
			}
		case setting == "unique":
			idx.IsUnique = true
		}
	}

	if idx.Name == "" && len(idx.Columns) > 0 {
		idx.Name = idx.Columns[0]
	}

	return idx, nil
}

func parseRefLine(line string) (struct {
	refName    string
	fromTable  string
	fromCol    string
	toTable    string
	toCol      string
	deleteRule string
	updateRule string
}, error) {
	var ref struct {
		refName    string
		fromTable  string
		fromCol    string
		toTable    string
		toCol      string
		deleteRule string
		updateRule string
	}
	ref.deleteRule = "NO ACTION"
	ref.updateRule = "NO ACTION"

	trimmed := strings.TrimSpace(line)

	// Remove "Ref:" or "Ref name:" prefix
	withoutPrefix := trimmed
	if strings.HasPrefix(trimmed, "Ref ") {
		// Format: "Ref name: table.col > table.col"
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx >= 0 {
			ref.refName = strings.TrimSpace(trimmed[4:colonIdx])
			withoutPrefix = strings.TrimSpace(trimmed[colonIdx+1:])
		} else {
			withoutPrefix = strings.TrimSpace(trimmed[4:])
		}
	} else {
		// Format: "Ref: table.col > table.col"
		withoutPrefix = strings.TrimSpace(strings.TrimPrefix(trimmed, "Ref:"))
	}

	// Determine direction: ">" or "<"
	var left, right string
	if strings.Contains(withoutPrefix, " > ") {
		parts := strings.SplitN(withoutPrefix, " > ", 2)
		left = parts[0]
		right = parts[1]
	} else if strings.Contains(withoutPrefix, " < ") {
		// Reverse: right side is the source (from), left is target (to)
		parts := strings.SplitN(withoutPrefix, " < ", 2)
		right = parts[0]
		left = parts[1]
	} else {
		return ref, fmt.Errorf("invalid Ref format (missing > or <): %s", line)
	}

	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)

	// Parse left side: table.col or "table"."col"
	ref.fromTable, ref.fromCol = parseRefSide(left)

	// Parse right side with optional settings
	refSettings := ""
	bracketIdx := strings.Index(right, "[")
	if bracketIdx >= 0 && strings.HasSuffix(strings.TrimSpace(right), "]") {
		ref.toTable, ref.toCol = parseRefSide(strings.TrimSpace(right[:bracketIdx]))
		refSettings = right[bracketIdx+1 : len(right)-1]
	} else {
		ref.toTable, ref.toCol = parseRefSide(right)
	}

	for _, setting := range parseSettings(refSettings) {
		setting = strings.TrimSpace(setting)
		if strings.HasPrefix(setting, "delete:") {
			ref.deleteRule = strings.TrimSpace(setting[7:])
		} else if strings.HasPrefix(setting, "update:") {
			ref.updateRule = strings.TrimSpace(setting[7:])
		}
	}

	return ref, nil
}

// parseRefSide parses "table.col" or "\"table\".\"col\"".
func parseRefSide(side string) (table, col string) {
	side = strings.TrimSpace(side)
	parts := strings.Split(side, ".")
	if len(parts) >= 2 {
		table = strings.Trim(parts[0], "\"")
		col = strings.Trim(parts[1], "\"")
	}
	return
}

func parseTableNote(table *s.TableDef, noteText string) {
	for _, part := range strings.Split(noteText, "|") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "Engine:"):
			table.Engine = strings.TrimSpace(part[7:])
		case strings.HasPrefix(part, "Collation:"):
			table.Collation = strings.TrimSpace(part[10:])
		case strings.HasPrefix(part, "Comment:"):
			table.Comment = strings.TrimSpace(part[8:])
		}
	}
}

// parseSettings splits comma-separated settings, respecting quotes.
func parseSettings(s string) []string {
	var result []string
	var current strings.Builder
	inBacktick := false
	inSingleQuote := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '`' && !inSingleQuote {
			inBacktick = !inBacktick
			current.WriteByte(ch)
		} else if ch == '\'' && !inBacktick {
			inSingleQuote = !inSingleQuote
			current.WriteByte(ch)
		} else if ch == ',' && !inBacktick && !inSingleQuote {
			result = append(result, strings.TrimSpace(current.String()))
			current.Reset()
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, strings.TrimSpace(current.String()))
	}
	return result
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

// findUnquoted finds the position of needle in s, ignoring occurrences inside
// double-quoted or single-quoted strings.
func findUnquoted(s string, needle string) int {
	inDQ := false
	inSQ := false
	for i := 0; i <= len(s)-len(needle); i++ {
		if s[i] == '"' && !inSQ {
			inDQ = !inDQ
			continue
		}
		if s[i] == '\'' && !inDQ {
			inSQ = !inSQ
			continue
		}
		if !inDQ && !inSQ && s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
