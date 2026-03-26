// One-off tool to convert question images from JPEG to WebP.
//
// Requires ImageMagick (magick) on PATH.
//
// What it does:
//  1. Reads each .jpg from images/huge/ (highest quality source)
//  2. Converts to WebP at two sizes:
//     - Desktop (max 1920w) → images/<name>.webp
//     - Mobile  (max 780w)  → images/mobile/<name>.webp
//  3. Updates the SQLite database (alien.db) picture column: .jpg → .webp
//  4. Deletes all .jpg files from images/, images/mobile/, images/huge/
//  5. Removes the images/huge/ directory
//
// Run from project root:
//
//	go run ./cmd/convertimages/
package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	desktopWidth = 1920
	mobileWidth  = 780
	quality      = 82
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal("getwd: %v", err)
	}

	hugeDir := filepath.Join(root, "images", "huge")
	desktopDir := filepath.Join(root, "images")
	mobileDir := filepath.Join(root, "images", "mobile")
	dbPath := filepath.Join(root, "alien.db")

	// Verify magick is available.
	if _, err := exec.LookPath("magick"); err != nil {
		fatal("ImageMagick (magick) not found on PATH")
	}

	// Collect source JPEGs from huge/.
	entries, err := os.ReadDir(hugeDir)
	if err != nil {
		fatal("reading %s: %v", hugeDir, err)
	}

	var jpgs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".jpg") {
			jpgs = append(jpgs, e.Name())
		}
	}

	if len(jpgs) == 0 {
		fatal("no .jpg files found in %s", hugeDir)
	}

	fmt.Printf("Found %d JPEG files to convert\n", len(jpgs))

	// Convert each image.
	for i, name := range jpgs {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		src := filepath.Join(hugeDir, name)

		dstDesktop := filepath.Join(desktopDir, base+".webp")
		dstMobile := filepath.Join(mobileDir, base+".webp")

		fmt.Printf("[%d/%d] %s\n", i+1, len(jpgs), name)

		// Desktop: resize to max 1920w, preserve aspect ratio.
		if err := convert(src, dstDesktop, desktopWidth); err != nil {
			fatal("  desktop convert failed: %v", err)
		}

		// Mobile: resize to max 780w, preserve aspect ratio.
		if err := convert(src, dstMobile, mobileWidth); err != nil {
			fatal("  mobile convert failed: %v", err)
		}
	}

	// Update database.
	if _, err := os.Stat(dbPath); err == nil {
		fmt.Println("\nUpdating database...")
		if err := updateDB(dbPath); err != nil {
			fatal("database update failed: %v", err)
		}
		fmt.Println("Database updated: .jpg → .webp in picture column")
	} else {
		fmt.Println("\nNo alien.db found — skipping database update")
		fmt.Println("The picture column will need .jpg → .webp updates when the DB exists")
	}

	// Delete old JPEG files.
	fmt.Println("\nCleaning up old JPEG files...")
	deleted := 0
	for _, dir := range []string{desktopDir, mobileDir, hugeDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".jpg") {
				p := filepath.Join(dir, e.Name())
				if err := os.Remove(p); err != nil {
					fmt.Printf("  warning: could not delete %s: %v\n", p, err)
				} else {
					deleted++
				}
			}
		}
	}
	fmt.Printf("Deleted %d JPEG files\n", deleted)

	// Remove huge/ directory.
	if err := os.Remove(hugeDir); err != nil {
		fmt.Printf("warning: could not remove %s: %v\n", hugeDir, err)
	} else {
		fmt.Println("Removed images/huge/ directory")
	}

	// Print summary.
	fmt.Println("\nDone! Summary:")
	fmt.Printf("  Converted: %d images × 2 sizes = %d WebP files\n", len(jpgs), len(jpgs)*2)
	fmt.Println("  Deleted:   all old JPEGs + images/huge/")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Update templates to remove huge/ srcset entries")
	fmt.Println("  2. Update hardcoded .jpg references to .webp in intro.html and about.html")
}

func convert(src, dst string, maxWidth int) error {
	// magick <src> -resize <W>x -strip -quality <Q> <dst>
	// The "Wx" geometry means "resize to width W, height auto".
	cmd := exec.Command("magick", src,
		"-resize", fmt.Sprintf("%dx", maxWidth),
		"-strip",
		"-quality", fmt.Sprintf("%d", quality),
		dst,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func updateDB(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := db.Exec(`UPDATE question SET picture = REPLACE(picture, '.jpg', '.webp') WHERE picture LIKE '%.jpg'`)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	fmt.Printf("  Updated %d rows\n", n)
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
