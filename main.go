// Package cmd CLI tool package
// Copyright © 2020 Sergey Romanov aka Serhioromano <serg4172@mail.ru>
package main

import (
	"github.com/serhioromano/mysqlsync/cmd"
	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

func main() {
	cmd.Execute()
}
