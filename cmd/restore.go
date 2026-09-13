// Package cmd CLI tool package
// Copyright © 2020 Sergey Romanov aka Serhioromano <serg4172@mail.ru>
package cmd

import (
	"fmt"
	"io/ioutil"

	"github.com/serhioromano/dbsync/msc/schema"
	"github.com/serhioromano/dbsync/msc/dbml"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// restoreCmd represents the restore command
var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore DB model from snapshot",
	Long:  `It takes a .dbml file with DB model snapshot created by the snash command and conforms the target DB to that snapshot.`,
	Run: func(cmd *cobra.Command, args []string) {

		profile := viper.GetString("profile")
		if profile != "" {
			if viper.IsSet("profiles." + profile + ".delete_table") {
				viper.Set("delete_table", viper.Get("profiles."+profile+".delete_table"))
			}
			if viper.IsSet("profiles." + profile + ".delete_index") {
				viper.Set("delete_index", viper.Get("profiles."+profile+".delete_index"))
			}
			if viper.IsSet("profiles." + profile + ".delete_column") {
				viper.Set("delete_column", viper.Get("profiles."+profile+".delete_column"))
			}
			if viper.IsSet("profiles." + profile + ".delete_constraint") {
				viper.Set("delete_constraint", viper.Get("profiles."+profile+".delete_constraint"))
			}
		}
		if t, _ := cmd.Flags().GetBool("d-table"); t == true {
			viper.Set("delete_table", t)
		}
		if c, _ := cmd.Flags().GetBool("d-column"); c == true {
			viper.Set("delete_column", c)
		}
		if i, _ := cmd.Flags().GetBool("d-index"); i == true {
			viper.Set("delete_index", i)
		}
		if k, _ := cmd.Flags().GetBool("d-constraint"); k == true {
			viper.Set("delete_constraint", k)
		}
		if o, _ := cmd.Flags().GetBool("optimize"); o == true {
			viper.Set("optimize", o)
		}

		options := schema.Config{
			Engine:      viper.GetString("engine"),
			User:        viper.GetString("user"),
			Pass:        viper.GetString("pass"),
			Host:        viper.GetString("host"),
			Port:        viper.GetString("port"),
			DB:          viper.GetString("db"),
			FilesPath:   viper.GetString("path"),
			File:        viper.GetString("file"),
			Prefix:      viper.GetString("prefix"),
			DTable:      viper.GetBool("delete_table"),
			DColumn:     viper.GetBool("delete_column"),
			DIndex:      viper.GetBool("delete_index"),
			DConstraint: viper.GetBool("delete_constraint"),
			Optimize:    viper.GetBool("optimize"),
		}

		filePath := options.FilesPath + "/" + options.File
		dat, err := ioutil.ReadFile(filePath)
		if err != nil {
			panic(err.Error())
		}

		schema, err := dbml.Parse(string(dat))
		if err != nil {
			panic(err.Error())
		}

		engine, err := getEngine(options.Engine)
		if err != nil {
			panic(err.Error())
		}

		if err := engine.Restore(options, schema); err != nil {
			panic(err.Error())
		}

		fmt.Println("Snapshot restored: " + filePath)
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)

	restoreCmd.Flags().BoolP("optimize", "o", true, "Run optimize query on table after finish")
	restoreCmd.Flags().BoolP("d-table", "t", true, "Delete tables that are not in the import")
	restoreCmd.Flags().BoolP("d-index", "i", true, "Delete indexes that are not in the import")
	restoreCmd.Flags().BoolP("d-column", "c", true, "Delete table columns that are not in the import")
	restoreCmd.Flags().BoolP("d-constraint", "k", true, "Delete constraints that are not in the import")
}
