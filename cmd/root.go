// Package cmd CLI tool package
// Copyright © 2020 Sergey Romanov aka Serhioromano <serg4172@mail.ru>
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string
var cnfLoaded bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dbsync",
	Short: "Synchronize MySQL DB Model from one DB to another",
	Long: `Tool to migrate DB from development to production without a pain.	
It works though generating DB model snapshot DBML files (Database Markup Language).
DBML files can be visualized at https://dbdiagram.io/home`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $PWD/.dbsync.json)")
	rootCmd.PersistentFlags().StringP("profile", "p", "", "Name of connection profile in configuration file. By adding profile you do not need to add any other flag.")
	rootCmd.PersistentFlags().String("db", "", "DB scheme name")
	rootCmd.PersistentFlags().String("user", "", "DB user name")
	rootCmd.PersistentFlags().String("pass", "", "DB password")
	rootCmd.PersistentFlags().String("path", "", "Path where snapshot files are stored")
	rootCmd.PersistentFlags().String("host", "", "DB host")
	rootCmd.PersistentFlags().String("port", "", "DB port")
	rootCmd.PersistentFlags().String("prefix", "", "DB table prefix. Will be deleted on snapshot and added on restore, thus you can have dev tables with one prefix or without and prod tables with other prefix.")
	rootCmd.PersistentFlags().String("engine", "mysql", "Database engine: mysql or sqlite")
	rootCmd.PersistentFlags().StringP("file", "f", "", "File to save snapshot or to restore from (default to DB scheme name)")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	viper.SetConfigName(".dbsync.json")
	viper.SetConfigType("json")
	viper.AddConfigPath("$HOME/.dbsync")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
	viper.BindPFlag("profile", rootCmd.Flags().Lookup("profile"))

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		} else {
			panic(fmt.Errorf("fatal error config file: %s", err))
		}
	}

	cnfLoaded = false
	if err := viper.ReadInConfig(); err == nil {
		cnfLoaded = true
		fmt.Println("Using config file:", viper.ConfigFileUsed())
		viper.Set("path", viper.Get("files_path"))
		profile := viper.GetString("profile")
		if profile != "" {
			viper.Set("user", viper.Get("profiles."+profile+".user"))
			viper.Set("db", viper.Get("profiles."+profile+".dbname"))
			viper.Set("port", viper.Get("profiles."+profile+".port"))
			viper.Set("pass", viper.Get("profiles."+profile+".pass"))
			viper.Set("host", viper.Get("profiles."+profile+".host"))
			viper.Set("prefix", viper.Get("profiles."+profile+".prefix"))
			viper.Set("file", viper.Get("profiles."+profile+".file_name"))
			viper.Set("engine", viper.Get("profiles."+profile+".engine"))
		}
	}

	user, _ := rootCmd.Flags().GetString("user")
	if user != "" {
		viper.Set("user", user)
	}
	db, _ := rootCmd.Flags().GetString("db")
	if db != "" {
		viper.Set("db", db)
	}
	host, _ := rootCmd.Flags().GetString("host")
	if host != "" {
		viper.Set("host", host)
	}
	pass, _ := rootCmd.Flags().GetString("pass")
	if pass != "" {
		viper.Set("pass", pass)
	}
	path, _ := rootCmd.Flags().GetString("path")
	if path != "" {
		viper.Set("path", path)
	}
	port, _ := rootCmd.Flags().GetString("port")
	if port != "" {
		viper.Set("port", port)
	}
	prefix, _ := rootCmd.Flags().GetString("prefix")
	if prefix != "" {
		viper.Set("prefix", prefix)
	}
	file, _ := rootCmd.Flags().GetString("file")
	if file != "" {
		viper.Set("file", file)
	}
	engine, _ := rootCmd.Flags().GetString("engine")
	if engine != "" {
		viper.Set("engine", engine)
	}
	f := viper.GetString("file")
	if f == "" {
		viper.Set("file", viper.GetString("db")+".dbml")
	}
}
