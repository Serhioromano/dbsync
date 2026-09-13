// Package cmd CLI tool package
// Copyright © 2020 Sergey Romanov aka Serhioromano <serg4172@mail.ru>
package cmd

import (
	"fmt"
	"os"

	"github.com/serhioromano/dbsync/msc/schema"
	"github.com/serhioromano/dbsync/msc/dbml"
	"github.com/serhioromano/dbsync/msc/mysql"
	"github.com/serhioromano/dbsync/msc/sqlite"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// snashCmd represents the snash command
var snashCmd = &cobra.Command{
	Use:   "snash",
	Short: "Create DB model snapshot",
	Long: `Creates DB model snapshot and stores it as a DBML file (Database Markup Language).
You can use this file to conform another DB to this model with the restore command.
DBML files can be visualized at https://dbdiagram.io/home`,
	Run: func(cmd *cobra.Command, args []string) {
		options := schema.Config{
			Engine:    viper.GetString("engine"),
			User:      viper.GetString("user"),
			Pass:      viper.GetString("pass"),
			Host:      viper.GetString("host"),
			Port:      viper.GetString("port"),
			DB:        viper.GetString("db"),
			FilesPath: viper.GetString("path"),
			File:      viper.GetString("file"),
			Prefix:    viper.GetString("prefix"),
		}

		engine, err := getEngine(options.Engine)
		if err != nil {
			panic(err.Error())
		}

		schema, err := engine.Snapshot(options)
		if err != nil {
			panic(err.Error())
		}

		filePath := options.FilesPath + "/" + options.File
		if err := os.MkdirAll(options.FilesPath, 0755); err != nil {
			panic(err.Error())
		}

		f, err := os.Create(filePath)
		if err != nil {
			panic(err.Error())
		}
		defer f.Close()

		if err := dbml.Write(f, schema); err != nil {
			panic(err.Error())
		}

		fmt.Println("Snapshot created successfully: " + filePath)
	},
}

func init() {
	rootCmd.AddCommand(snashCmd)
}

func getEngine(name string) (schema.Engine, error) {
	switch name {
	case "mysql":
		return &mysql.Engine{}, nil
	case "sqlite":
		return &sqlite.Engine{}, nil
	default:
		return nil, fmt.Errorf("unsupported engine: %s (use 'mysql' or 'sqlite')", name)
	}
}
