package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// readPassword asks twice on a terminal (nothing echoed), or reads one line from standard input with --password-stdin,
// so a password never has to appear in the shell's history or the process list.
func readPassword(cmd *cobra.Command, fromStdin bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("no terminal to ask on: pipe the password in with --password-stdin")
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Again: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("the two passwords differ")
	}
	return string(a), nil
}

func userCmd() *cobra.Command {
	c := &cobra.Command{Use: "user", Short: "Manage the people who can sign in (accounts mode)"}
	var role string
	var stdin bool
	add := &cobra.Command{
		Use: "add NAME", Short: "Add a person; asks for the password", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if store.RoleRank(role) == 0 {
				return fmt.Errorf("--role must be viewer, editor or owner")
			}
			pw, err := readPassword(cmd, stdin)
			if err != nil {
				return err
			}
			hash, err := auth.HashPassword(pw)
			if err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			if err := s.CreateUser(context.Background(), store.User{Name: args[0], Role: role, PasswordHash: hash, Created: time.Now().UTC()}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s (%s)\n", args[0], role)
			return nil
		},
	}
	add.Flags().StringVar(&role, "role", store.RoleViewer, "viewer, editor or owner")
	add.Flags().BoolVar(&stdin, "password-stdin", false, "read the password from standard input")
	c.AddCommand(add)

	c.AddCommand(&cobra.Command{
		Use: "list", Short: "Everyone, their role and whether they can sign in",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			all, err := s.Users(context.Background())
			if err != nil {
				return err
			}
			for _, u := range all {
				state := "active"
				if u.Disabled {
					state = "disabled"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-24s %-7s %-8s since %s\n", u.Name, u.Role, state, u.Created.Local().Format("2006-01-02"))
			}
			return nil
		},
	})

	var pstdin bool
	passwd := &cobra.Command{
		Use: "passwd NAME", Short: "Set a new password", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			u, err := s.User(context.Background(), args[0])
			if err != nil {
				return err
			}
			pw, err := readPassword(cmd, pstdin)
			if err != nil {
				return err
			}
			if u.PasswordHash, err = auth.HashPassword(pw); err != nil {
				return err
			}
			if err := s.UpdateUser(context.Background(), u); err != nil {
				return err
			}
			if err := s.DeleteSessions(context.Background(), u.Name, ""); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "password changed for %s; signed out everywhere\n", u.Name)
			return nil
		},
	}
	passwd.Flags().BoolVar(&pstdin, "password-stdin", false, "read the password from standard input")
	c.AddCommand(passwd)

	c.AddCommand(&cobra.Command{
		Use: "remove NAME", Short: "Remove a person, their API tokens and sessions (never the last owner)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			ctx := context.Background()
			u, err := s.User(ctx, args[0])
			if err != nil {
				return err
			}
			if u.Role == store.RoleOwner && !u.Disabled {
				all, _ := s.Users(ctx)
				owners := 0
				for _, x := range all {
					if x.Role == store.RoleOwner && !x.Disabled {
						owners++
					}
				}
				if owners <= 1 {
					return errors.New("this is the last owner who can sign in; add another owner first")
				}
			}
			if err := s.DeleteUser(ctx, u.Name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", u.Name)
			return nil
		},
	})
	return c
}

func tokenCmd() *cobra.Command {
	c := &cobra.Command{Use: "token", Short: "Manage API tokens"}
	var user, name string
	create := &cobra.Command{
		Use: "create", Short: "Make an API token for a person; it is printed once",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			ctx := context.Background()
			if _, err := s.User(ctx, user); err != nil {
				return fmt.Errorf("user %q: %w", user, err)
			}
			secret, err := auth.NewSecret("brofo_")
			if err != nil {
				return err
			}
			id, err := auth.NewSecret("tk_")
			if err != nil {
				return err
			}
			id = id[:3+16]
			if err := s.CreateToken(ctx, store.Token{ID: id, User: user, Name: name, Hash: auth.HashSecret(secret), Created: time.Now().UTC()}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "token %s for %s (%s). Copy it now: it is not shown again.\n", id, user, name)
			fmt.Fprintln(cmd.OutOrStdout(), secret)
			return nil
		},
	}
	create.Flags().StringVar(&user, "user", "", "whose token it is")
	create.Flags().StringVar(&name, "name", "cli", "what uses it")
	_ = create.MarkFlagRequired("user")
	c.AddCommand(create)
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "Every token (never the secret)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			all, err := s.Tokens(context.Background(), "")
			if err != nil {
				return err
			}
			for _, t := range all {
				used := "never used"
				if !t.LastUsed.IsZero() {
					used = "last used " + t.LastUsed.Local().Format("2006-01-02 15:04")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-20s %-20s %s\n", t.ID, t.User, t.Name, used)
			}
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "revoke ID", Short: "Stop a token working", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			if err := s.RevokeToken(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "revoked %s\n", args[0])
			return nil
		},
	})
	return c
}
