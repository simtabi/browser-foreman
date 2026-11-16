package cli

import (
	"errors"

	notifykit "github.com/simtabi/go-notifykit/src"
	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/notify"
)

// errNotifyFailed is a notify verb that failed after go-notifykit said why: nothing more to print.
var errNotifyFailed = errors.New("notify failed")

// notifyCmd is the send, ping and doctor verbs every Simtabi Go program shares through go-notifykit.
func notifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Desktop notifications: send one, hand one to the running server, or check they work here",
	}
	for _, v := range []struct{ verb, short string }{
		{"send", "Show a desktop notification from this process"},
		{"ping", "Hand a notification to the running server (browser-foreman serve), which shows it"},
		{"doctor", "Say whether desktop notifications work here, and why not"},
	} {
		verb := v.verb
		cmd.AddCommand(&cobra.Command{
			Use:                verb + " [-title T] [-subtitle S] [-body B] [-group G] [-level info|warn|alert] [-sound] [-json]",
			Short:              v.short,
			DisableFlagParsing: true,
			RunE: func(c *cobra.Command, args []string) error {
				_, cfg, err := load()
				if err != nil {
					return err
				}
				n, err := notify.New(cfg)
				if err != nil {
					return err
				}
				if notifykit.RunCLI(c.Context(), n, append([]string{verb}, args...), c.InOrStdin(), c.OutOrStdout(), c.ErrOrStderr()) != 0 {
					return errNotifyFailed
				}
				return nil
			},
		})
	}
	return cmd
}
