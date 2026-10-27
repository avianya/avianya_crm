// Command admin manages logins from the command line, for the one job the web
// app cannot do: creating the first admin, before anyone can log in.
//
//	go run ./cmd/admin create-user -email you@company.com -name "Your Name" -role admin
//	go run ./cmd/admin set-password -email you@company.com
//
// The password is read from the CRM_PASSWORD environment variable if set,
// otherwise typed at the prompt. It is never taken as a flag, which would leave
// it in the shell's history. It uses DATABASE_URL from backend/.env like the
// server.
//
// create-user makes a new team member with that role and password, or gives an
// existing member (same email) that role and password, keeping their leads.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"crm-lead-management/backend/internal/auth"
	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() error {
	return fmt.Errorf(`usage:
  go run ./cmd/admin create-user -email EMAIL -name "NAME" [-role admin|manager|sales]
  go run ./cmd/admin set-password -email EMAIL
The password comes from CRM_PASSWORD, or is asked for.`)
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	email := fs.String("email", "", "the member's email (their login)")
	name := fs.String("name", "", "their name (create-user)")
	role := fs.String("role", "admin", "admin, manager or sales (create-user)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	*email = strings.ToLower(strings.TrimSpace(*email))
	if *email == "" || !strings.Contains(*email, "@") {
		return fmt.Errorf("-email is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := store.New(ctx, cfg.DatabaseURL, cfg.Timezone)
	if err != nil {
		return err
	}
	defer db.Close()

	switch cmd {
	case "create-user":
		r := model.Role(*role)
		if !r.Valid() {
			return fmt.Errorf("-role must be admin, manager or sales")
		}
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("-name is required")
		}
		hash, err := readPassword()
		if err != nil {
			return err
		}
		m, created, err := db.UpsertAccount(ctx, strings.TrimSpace(*name), *email, r, hash)
		if err != nil {
			return err
		}
		verb := "Gave existing team member"
		if created {
			verb = "Created"
		}
		fmt.Printf("%s %s <%s> (id %d) the role %s and a password. They can log in now.\n", verb, m.Name, m.Email, m.ID, m.Role)
	case "set-password":
		rec, err := db.LoginRecordByEmail(ctx, *email)
		if err != nil {
			return fmt.Errorf("no team member with email %s", *email)
		}
		hash, err := readPassword()
		if err != nil {
			return err
		}
		if err := db.SetPasswordHash(ctx, rec.Viewer.MemberID, hash, nil); err != nil {
			return err
		}
		fmt.Printf("Password set for %s; their other sessions have ended.\n", *email)
	default:
		return usage()
	}
	return nil
}

// readPassword takes the password from CRM_PASSWORD or the prompt, checks it,
// and returns its hash.
func readPassword() (string, error) {
	password := os.Getenv("CRM_PASSWORD")
	if password == "" {
		fmt.Printf("Password (at least %d characters; it is shown as you type, so make sure no one is watching): ", model.MinPasswordLength)
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("no password given")
		}
		password = strings.TrimRight(line, "\r\n")
	}
	if msg := model.ValidatePassword(password); msg != "" {
		return "", fmt.Errorf("the password %s", msg)
	}
	return auth.HashPassword(password)
}
