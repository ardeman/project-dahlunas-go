// Command mint makes signing keys and unlock codes by hand, for orders
// before the payment webhook exists.
//
//	mint new-key -project 1 -key 1
//	mint code -project 1 -product 1 -key 1
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/unlock"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mint:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: mint new-key|code [flags]")
	}
	fs := flag.NewFlagSet("mint "+args[0], flag.ContinueOnError)
	project := fs.Uint("project", 0, "project ID (1–65535)")
	keyID := fs.Uint("key", 0, "key ID within the project (1–255)")
	product := fs.Uint("product", 0, "product ID within the project (1–65535), for code")
	out := fs.String("out", "", "file for the new key's seed, for new-key (default dahlunas-<project>-<key>.key)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *project < 1 || *project > math.MaxUint16 || *keyID < 1 || *keyID > math.MaxUint8 {
		return errors.New("-project (1–65535) and -key (1–255) are required")
	}
	switch args[0] {
	case "new-key":
		path := *out
		if path == "" {
			path = fmt.Sprintf("dahlunas-%d-%d.key", *project, *keyID)
		}
		return newKey(path, uint16(*project), uint8(*keyID))
	case "code":
		if *product < 1 || *product > math.MaxUint16 {
			return errors.New("-product (1–65535) is required")
		}
		key, err := unlock.KeyFromEnv(uint16(*project), uint8(*keyID))
		if err != nil {
			return err
		}
		code, err := unlock.New(key, uint8(*keyID), uint16(*project), uint16(*product), time.Now())
		if err != nil {
			return err
		}
		fmt.Println(code)
		fmt.Println(unlock.Grouped(code))
		return nil
	default:
		return fmt.Errorf("unknown command %q; use new-key or code", args[0])
	}
}

// newKey writes a new seed to path (never printed) and prints the public key.
func newKey(path string, project uint16, keyID uint8) error {
	seed, public, err := unlock.NewSeed()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, seed); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("Seed written to %s. Store it as the secret %s, then delete the file.\n", path, unlock.KeyEnv(project, keyID))
	fmt.Printf("Public key %d for project %d (bundle it in the app):\n%s\n", keyID, project, public)
	return nil
}
