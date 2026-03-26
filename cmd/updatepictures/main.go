// One-off tool to update the question.picture column from .jpg to .webp.
// Run on the REMOTE SERVER after deploying the new WebP images.
//
// Build:
//   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o updatepictures ./cmd/updatepictures/
//
// Then SCP to server and run:
//   ./updatepictures /var/www/alien/alien.db
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <path-to-alien.db>\n", os.Args[0])
		os.Exit(1)
	}
	dbPath := os.Args[1]

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	result, err := db.Exec(`UPDATE question SET picture = REPLACE(picture, '.jpg', '.webp') WHERE picture LIKE '%.jpg'`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: update failed: %v\n", err)
		os.Exit(1)
	}

	n, _ := result.RowsAffected()
	fmt.Printf("Updated %d rows: .jpg → .webp\n", n)
}
