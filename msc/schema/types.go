package schema

// Config holds all configuration for snapshot and restore operations.
type Config struct {
	Engine      string // "mysql" or "sqlite"
	User        string
	Pass        string
	Host        string
	Port        string
	DB          string // For MySQL: schema name; for SQLite: file path
	FilesPath   string
	File        string
	Prefix      string
	DTable      bool
	DColumn     bool
	DIndex      bool
	DConstraint bool
	Optimize    bool
}

// Schema is a portable database schema definition used for snapshot and restore.
type Schema struct {
	Name   string
	Prefix string
	Tables []TableDef
}

// TableDef is a complete table definition.
type TableDef struct {
	Name        string
	Engine      string
	Collation   string
	Comment     string
	PrimaryKey  string
	Fields      []FieldDef
	Indexes     []IndexDef
	Constraints []ConstraintDef
}

// FieldDef describes a single column.
type FieldDef struct {
	Name         string
	ColumnType   string  // e.g. "varchar(255)", "int(11)"
	DataType     string  // e.g. "varchar", "int"
	IsNullable   bool
	DefaultValue *string // nil means no default
	Comment      string
	IsPrimary    bool
	IsAutoIncr   bool
	IsUnique     bool
}

// IndexDef describes an index (including unique indexes, excluding PK and FK).
type IndexDef struct {
	Name      string
	Columns   []string
	IsUnique  bool
	IndexType string // "BTREE", "FULLTEXT", "HASH"
}

// ConstraintDef describes a foreign key constraint.
type ConstraintDef struct {
	Name              string
	ColumnName        string
	RefTableName      string
	RefColumnName     string
	UpdateRule        string // e.g. "CASCADE", "NO ACTION"
	DeleteRule        string // e.g. "CASCADE", "NO ACTION"
}

// Engine is the interface that every database adapter must implement.
type Engine interface {
	// Snapshot connects to the database and returns its full Schema.
	Snapshot(cfg Config) (*Schema, error)

	// Restore connects to the database and applies the given Schema,
	// generating and executing DDL to make the DB match.
	Restore(cfg Config, schema *Schema) error
}
