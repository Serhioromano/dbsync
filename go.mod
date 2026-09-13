module github.com/serhioromano/mysqlsync

go 1.16

require (
	github.com/go-sql-driver/mysql v1.5.0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/viper v1.7.1
	modernc.org/sqlite v1.17.0
)

replace github.com/serhioromano/mysqlsync/cmd => ../cmd
