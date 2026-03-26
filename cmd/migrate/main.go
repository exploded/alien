// Migration tool: reads from existing MySQL database and creates a new SQLite database.
// Usage: DATASOURCE="user:pass@tcp(host:3306)/alien" go run ./cmd/migrate/
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

func main() {
	dsn := os.Getenv("DATASOURCE")
	if dsn == "" {
		log.Fatal("DATASOURCE env var required (MySQL DSN)")
	}

	dbPath := "alien.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	// Connect to MySQL
	my, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("could not open mysql: %v", err)
	}
	defer my.Close()

	if err := my.Ping(); err != nil {
		log.Fatalf("could not connect to mysql: %v", err)
	}
	fmt.Println("connected to mysql")

	// Remove existing SQLite DB if present
	os.Remove(dbPath)

	// Create SQLite DB
	lite, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("could not open sqlite: %v", err)
	}
	defer lite.Close()

	lite.Exec("PRAGMA journal_mode=WAL")
	lite.Exec("PRAGMA foreign_keys=ON")

	_, err = lite.Exec(`
		CREATE TABLE IF NOT EXISTS question (
			id INTEGER PRIMARY KEY,
			category TEXT NOT NULL DEFAULT '',
			question TEXT NOT NULL DEFAULT '',
			picture TEXT NOT NULL DEFAULT '',
			yes INTEGER NOT NULL DEFAULT 0,
			no INTEGER NOT NULL DEFAULT 0,
			short TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS answer (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			question INTEGER NOT NULL DEFAULT 0,
			answer INTEGER NOT NULL DEFAULT 0,
			submitter TEXT NOT NULL DEFAULT '',
			submitdate TEXT NOT NULL DEFAULT (datetime('now')),
			submitteragent TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		log.Fatalf("could not create schema: %v", err)
	}

	// Migrate questions
	rows, err := my.Query("SELECT id, category, question, picture, yes, `no`, short FROM question ORDER BY id")
	if err != nil {
		log.Fatalf("could not query questions: %v", err)
	}

	tx, err := lite.Begin()
	if err != nil {
		log.Fatalf("could not begin transaction: %v", err)
	}

	stmt, err := tx.Prepare("INSERT INTO question (id, category, question, picture, yes, no, short) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		log.Fatalf("could not prepare question insert: %v", err)
	}

	questionCount := 0
	for rows.Next() {
		var id, yes, no int64
		var category, question, picture, short string
		if err := rows.Scan(&id, &category, &question, &picture, &yes, &no, &short); err != nil {
			log.Fatalf("could not scan question: %v", err)
		}
		if _, err := stmt.Exec(id, category, question, picture, yes, no, short); err != nil {
			log.Fatalf("could not insert question %d: %v", id, err)
		}
		questionCount++
	}
	rows.Close()
	stmt.Close()

	if err := tx.Commit(); err != nil {
		log.Fatalf("could not commit questions: %v", err)
	}
	fmt.Printf("migrated %d questions\n", questionCount)

	// Migrate answers
	rows, err = my.Query("SELECT id, question, answer, submitter, submitdate, submitteragent FROM answer ORDER BY id")
	if err != nil {
		log.Fatalf("could not query answers: %v", err)
	}

	tx, err = lite.Begin()
	if err != nil {
		log.Fatalf("could not begin transaction: %v", err)
	}

	stmt, err = tx.Prepare("INSERT INTO answer (id, question, answer, submitter, submitdate, submitteragent) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		log.Fatalf("could not prepare answer insert: %v", err)
	}

	answerCount := 0
	for rows.Next() {
		var id, question, answer int64
		var submitter, submitdate, submitteragent string
		if err := rows.Scan(&id, &question, &answer, &submitter, &submitdate, &submitteragent); err != nil {
			log.Fatalf("could not scan answer: %v", err)
		}
		if _, err := stmt.Exec(id, question, answer, submitter, submitdate, submitteragent); err != nil {
			log.Fatalf("could not insert answer %d: %v", id, err)
		}
		answerCount++
		if answerCount%10000 == 0 {
			fmt.Printf("  migrated %d answers...\n", answerCount)
		}
	}
	rows.Close()
	stmt.Close()

	if err := tx.Commit(); err != nil {
		log.Fatalf("could not commit answers: %v", err)
	}
	fmt.Printf("migrated %d answers\n", answerCount)
	fmt.Println("migration complete:", dbPath)
}
