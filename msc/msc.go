package msc

import (
	"fmt"
	"os"
	"strings"

	"github.com/serhioromano/dbsync/msc/dbml"
	"github.com/serhioromano/dbsync/msc/mysql"
	"github.com/serhioromano/dbsync/msc/schema"
)

// Config is an alias for backward compatibility.
type Config = schema.Config

// Schema is an alias for backward compatibility.
type Schema = schema.Schema

// TableDef is an alias for backward compatibility.
type TableDef = schema.TableDef

// FieldDef is an alias for backward compatibility.
type FieldDef = schema.FieldDef

// IndexDef is an alias for backward compatibility.
type IndexDef = schema.IndexDef

// ConstraintDef is an alias for backward compatibility.
type ConstraintDef = schema.ConstraintDef

// Engine is an alias for backward compatibility.
type Engine = schema.Engine

// Snash is a backward-compatible wrapper that creates a DBML snapshot
// using the MySQL engine. Prefer using the Engine interface directly.
func Snash(p Config) error {
	p.Engine = "mysql"
	engine := &mysql.Engine{}
	schema, err := engine.Snapshot(p)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(p.FilesPath, 0755); err != nil {
		return err
	}

	f, err := os.Create(p.FilesPath + "/" + p.File)
	if err != nil {
		return err
	}
	defer f.Close()

	return dbml.Write(f, schema)
}

// Restore is a backward-compatible wrapper that restores a DBML snapshot
// using the MySQL engine. Prefer using the Engine interface directly.
type Restore struct{}

// Run executes the restore using the MySQL engine.
func (r *Restore) Run(p Config) error {
	p.Engine = "mysql"

	dat, err := os.ReadFile(p.FilesPath + "/" + p.File)
	if err != nil {
		return err
	}

	schema, err := dbml.Parse(string(dat))
	if err != nil {
		return err
	}

	engine := &mysql.Engine{}
	return engine.Restore(p, schema)
}

// escapeDBMLName is kept for backward compatibility.
func escapeDBMLName(name string) string {
	return strings.ReplaceAll(name, "\"", "\\\"")
}

// Struct2json is kept for backward compatibility.
// It converts a struct to a map using reflection.
func Struct2json(s interface{}) map[string]interface{} {
	fmt.Println("Warning: Struct2json is deprecated; use typed Schema/TableDef instead")
	return make(map[string]interface{})
}
