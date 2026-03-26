// One-off tool to convert question images from JPEG to WebP.
// Run LOCALLY (requires ImageMagick).
//
// What it does:
//  1. Reads each .jpg from images/huge/ (highest quality source)
//  2. Converts to WebP at two sizes:
//     - Desktop (max 1920w) → images/<name>.webp
//     - Mobile  (max 780w)  → images/mobile/<name>.webp
//  3. Deletes all .jpg files from images/, images/mobile/, images/huge/
//  4. Removes the images/huge/ directory
//
// Run from project root:
//
//	go run ./cmd/convertimages/
//
// Then separately run cmd/updatepictures on the remote server to update the DB.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	fmt.Println("\nDone! Summary:")
	fmt.Printf("  Converted: %d images × 2 sizes = %d WebP files\n", len(jpgs), len(jpgs)*2)
	fmt.Println("  Deleted:   all old JPEGs + images/huge/")
	fmt.Println("\nNext: deploy, then run updatepictures on the server to update the DB")
}

func convert(src, dst string, maxWidth int) error {
	cmd := exec.Command("magick", src,
		"-resize", fmt.Sprintf("%dx", maxWidth),
		"-strip",
		"-quality", fmt.Sprintf("%d", quality),
		dst,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
