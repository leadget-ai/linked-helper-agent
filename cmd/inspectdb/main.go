// inspectdb prints the capability profile and resolved account owner for one
// or more lh.db files. It is the first tool to reach for when a customer's LH
// build ships a schema we have not seen: the output shows which tables and
// columns exist and which owner fields each strategy managed to fill, so the
// gap maps directly to the OwnerSource that needs adjusting.
//
//	go run ./cmd/inspectdb path/to/lh.db [more.db ...]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/leadget/lh-agent/internal/lh"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: inspectdb <lh.db> [more.db ...]")
		os.Exit(2)
	}

	ctx := context.Background()
	reader := lh.NewReader()
	defer reader.Close()

	exitCode := 0
	for _, path := range os.Args[1:] {
		if err := inspect(ctx, reader, path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			exitCode = 1
		}
	}
	os.Exit(exitCode)
}

func inspect(ctx context.Context, reader *lh.Reader, path string) error {
	fmt.Printf("=== %s ===\n", path)

	profile, err := reader.InspectProfile(ctx, path)
	if err != nil {
		return err
	}
	fmt.Printf("schema version: %d\n", profile.SchemaVersion)
	fmt.Printf("capabilities: %s\n", profile.Describe())

	owner, err := reader.ReadAccountOwner(ctx, path)
	if err != nil {
		return err
	}
	if owner == nil {
		fmt.Println("owner: <none resolved>")
		return nil
	}

	rendered, err := json.MarshalIndent(ownerView(owner), "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("owner: %s\n\n", rendered)
	return nil
}

func ownerView(o *lh.AccountOwner) map[string]any {
	view := map[string]any{}
	put := func(key string, s *string) {
		if s != nil {
			view[key] = *s
		}
	}
	put("fullName", o.FullName)
	put("email", o.Email)
	put("avatar", o.Avatar)
	put("profileUrl", o.ProfileURL)
	put("publicSlug", o.PublicSlug)
	put("lastLoginAt", o.LastLoginAt)
	if o.ExternalID != nil {
		view["memberId"] = *o.ExternalID
	}
	if o.SSI != nil {
		view["ssi"] = *o.SSI
	}
	return view
}
