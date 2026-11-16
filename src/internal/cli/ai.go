package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/ai"
	"github.com/simtabi/browser-foreman/src/internal/config"
)

// aiCmd makes AI jobs in the running server and reports them: tags to suggest, duplicates to judge. Who answers is
// ai.provider (Anthropic's API or a local Ollama), or with none an MCP client (next_ai_jobs, the process-queue prompt).
func aiCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ai",
		Short: "Suggest tags and judge duplicates with a model (ai.provider: anthropic, ollama, or an MCP client)",
	}
	var folder string
	var apply, retag bool
	var limit int
	tag := &cobra.Command{
		Use:   "tag WORKSPACE",
		Short: "Suggest tags for a workspace's (or a folder's) untagged bookmarks; --apply adds them",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAI(cmd, map[string]any{"kind": ai.KindTag, "workspace": args[0], "folder": folder, "apply": apply, "limit": limit, "retag": retag})
		},
	}
	tag.Flags().StringVar(&folder, "folder", "", "only this folder and what is inside it, a path like \"Bookmarks Bar/Dev\"")
	tag.Flags().BoolVar(&apply, "apply", false, "add the suggested tags (one history entry per job; undo takes it back); without it they are only shown")
	tag.Flags().IntVar(&limit, "limit", 200, "bookmarks at most (up to 1000)")
	tag.Flags().BoolVar(&retag, "retag", false, "also bookmarks that already have tags")
	var dapply bool
	dedupe := &cobra.Command{
		Use:   "dedupe WORKSPACE",
		Short: "Judge look-alike bookmarks that may be the same page; --apply moves the copies judged the same to the Trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAI(cmd, map[string]any{"kind": ai.KindDedupe, "workspace": args[0], "apply": dapply})
		},
	}
	dedupe.Flags().BoolVar(&dapply, "apply", false, "move the copies judged the same page to the Trash (one history entry per job); without it they are only shown")
	jobs := &cobra.Command{
		Use:   "jobs",
		Short: "List the AI jobs in the running server, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			call, _, err := serverCaller(cmd.Context())
			if err != nil {
				return err
			}
			var out struct{ Jobs []aiJob }
			if err := call(http.MethodGet, "ai/jobs", nil, &out); err != nil {
				return err
			}
			if len(out.Jobs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No AI jobs.")
			}
			for _, j := range out.Jobs {
				printJob(cmd.OutOrStdout(), j)
			}
			return nil
		},
	}
	c.AddCommand(tag, dedupe, jobs)
	return c
}

type aiJob struct {
	ID, Kind, Workspace, Folder, State, By, Error string
	Apply                                         bool
	Size                                          int
	Results                                       *ai.Results
}

func printJob(out io.Writer, j aiJob) {
	what := fmt.Sprintf("%d bookmarks", j.Size)
	if j.Kind == ai.KindDedupe {
		what = fmt.Sprintf("%d groups", j.Size)
	}
	line := fmt.Sprintf("%s  %-6s %-8s %s in %s", j.ID, j.Kind, j.State, what, j.Workspace)
	if j.Folder != "" {
		line += "/" + j.Folder
	}
	if j.By != "" {
		line += " · by " + j.By
	}
	if j.Error != "" {
		line += " · " + j.Error
	}
	fmt.Fprintln(out, line)
}

func printResults(out io.Writer, j aiJob) {
	if j.Results == nil {
		return
	}
	for _, t := range j.Results.Tags {
		fmt.Fprintf(out, "  %s: %s\n", t.ID, strings.Join(t.Tags, ", "))
	}
	for _, v := range j.Results.Verdicts {
		if len(v.Same) == 0 {
			fmt.Fprintf(out, "  keep %s: none are the same page (%s)\n", v.Keep, v.Reason)
			continue
		}
		fmt.Fprintf(out, "  keep %s, same page: %s (%s)\n", v.Keep, strings.Join(v.Same, ", "), v.Reason)
	}
}

// runAI makes the jobs and, when the server answers them itself, waits for the answers (up to ten minutes).
func runAI(cmd *cobra.Command, body map[string]any) error {
	call, c, err := serverCaller(cmd.Context())
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	switch c.AI.Provider {
	case "anthropic":
		fmt.Fprintf(out, "Sending these bookmarks' titles, addresses and page descriptions to Anthropic (%s).\n", modelOr(c.AI.Model, ai.DefaultAnthropicModel))
	case "openai":
		fmt.Fprintf(out, "Sending these bookmarks' titles, addresses and page descriptions to %s (%s).\n", c.AI.OpenAIURL, c.AI.Model)
	}
	var made struct {
		Jobs       []aiJob `json:"jobs"`
		AnsweredBy string  `json:"answered_by"`
	}
	if err := call(http.MethodPost, "ai/jobs", body, &made); err != nil {
		return err
	}
	fmt.Fprintf(out, "%d job(s) made; answered by %s.\n", len(made.Jobs), made.AnsweredBy)
	if c.AI.Provider == "none" {
		fmt.Fprintln(out, "`browser-foreman ai jobs` shows where they stand.")
		return nil
	}
	want := map[string]bool{}
	for _, j := range made.Jobs {
		want[j.ID] = true
	}
	deadline := time.Now().Add(10 * time.Minute)
	failed := 0
	for len(want) > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		var list struct{ Jobs []aiJob }
		if err := call(http.MethodGet, "ai/jobs", nil, &list); err != nil {
			return err
		}
		for _, j := range list.Jobs {
			if !want[j.ID] || (j.State != ai.Done && j.State != ai.Failed) {
				continue
			}
			delete(want, j.ID)
			printJob(out, j)
			printResults(out, j)
			if j.State == ai.Failed {
				failed++
			}
		}
	}
	switch {
	case len(want) > 0:
		return fmt.Errorf("%d job(s) still running after ten minutes: `browser-foreman ai jobs` shows them", len(want))
	case failed > 0:
		return fmt.Errorf("%d of %d job(s) failed", failed, len(made.Jobs))
	}
	if body["apply"] == false {
		fmt.Fprintln(out, "Nothing was changed: add --apply to write these.")
	}
	return nil
}

func modelOr(m, def string) string {
	if m == "" {
		return def
	}
	return m
}

// serverCaller calls the running server's API with the owner's token, in local mode.
func serverCaller(ctx context.Context) (func(method, path string, body, out any) error, config.Config, error) {
	_, c, err := load()
	if err != nil {
		return nil, c, err
	}
	if c.Server.Mode != config.ModeLocal || c.APIToken == "" {
		return nil, c, errors.New("in accounts mode, make AI jobs through the API with an owner's token (POST /api/v1/ai/jobs)")
	}
	addr := serverAddr(ctx, c)
	return func(method, path string, body, out any) error {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, "http://"+addr+"/api/v1/"+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return fmt.Errorf("Browser Foreman is not answering on %s: start it with `browser-foreman serve` (AI jobs live in the server)", addr)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if resp.StatusCode >= 300 {
			var e struct{ Error string }
			_ = json.Unmarshal(raw, &e)
			return fmt.Errorf("%s (HTTP %d)", e.Error, resp.StatusCode)
		}
		return json.Unmarshal(raw, out)
	}, c, nil
}
