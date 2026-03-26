package main

import (
	"database/sql"
	"log/slog"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

func InitDB(datasourcename string) {
	var err error
	db, err = sql.Open("mysql", datasourcename)
	if err != nil {
		slog.Error("could not open database", "err", err)
		os.Exit(1)
	}

	if err = db.Ping(); err != nil {
		slog.Error("could not connect to database", "err", err)
		os.Exit(1)
	}
	slog.Info("database connected")
}
