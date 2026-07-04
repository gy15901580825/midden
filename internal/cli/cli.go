// Package cli wires cobra subcommands to the app layer. Commands never
// touch the filesystem directly.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"aism/internal/adapter/claude"
	"aism/internal/app"
	"aism/internal/format"
	"aism/internal/trash"
	"aism/internal/tui"
)

const version = "0.1.0-dev"

type flags struct {
	dryRun, yes, jsonOut bool
	tool                 string
}

func New() *cobra.Command {
	fl := &flags{}
	root := &cobra.Command{
		Use:           "aism",
		Short:         "Manage AI coding sessions (Claude Code) — browse, rename, clear, remove",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(fl)
			if err != nil {
				return err
			}
			return tui.Run(a)
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if fl.tool != "claude" {
				return fmt.Errorf("unsupported --tool %q (v1 supports: claude)", fl.tool)
			}
			return nil
		},
	}
	pf := root.PersistentFlags()
	pf.BoolVar(&fl.dryRun, "dry-run", false, "show what would happen, move nothing")
	pf.BoolVarP(&fl.yes, "yes", "y", false, "skip confirmation prompts")
	pf.BoolVar(&fl.jsonOut, "json", false, "machine-readable output")
	pf.StringVar(&fl.tool, "tool", "claude", "which AI tool's sessions to manage")

	root.AddCommand(projectsCmd(fl), sessionsCmd(fl), renameCmd(fl), removeCmd(fl), clearCmd(fl), trashCmd(fl))
	return root
}

func Execute() error { return New().Execute() }

func newApp(fl *flags) (*app.App, error) {
	croot, err := claude.DefaultRoot()
	if err != nil {
		return nil, err
	}
	troot, err := trash.DefaultRoot()
	if err != nil {
		return nil, err
	}
	return app.New(claude.New(croot), trash.New(troot), fl.dryRun), nil
}

func resolveProject(a *app.App, arg string) (string, error) {
	projects, err := a.Projects()
	if err != nil {
		return "", err
	}
	var byName []string
	for _, p := range projects {
		if p.ID == arg {
			return p.ID, nil
		}
		if p.Name == arg {
			byName = append(byName, p.ID)
		}
	}
	switch len(byName) {
	case 1:
		return byName[0], nil
	case 0:
		return "", fmt.Errorf("no project named %q (try 'aism projects') (project ids start with '-': pass them after --, e.g. aism sessions -- <id>)", arg)
	default:
		return "", fmt.Errorf("%q is ambiguous, use a full id (pass it after -- so it isn't parsed as flags): aism sessions -- <id>. Matches: %s", arg, strings.Join(byName, ", "))
	}
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func projectsCmd(fl *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "projects",
		Short: "List all projects with session counts and sizes",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(fl)
			if err != nil {
				return err
			}
			projects, err := a.Projects()
			if err != nil {
				return err
			}
			if fl.jsonOut {
				return printJSON(cmd.OutOrStdout(), projects)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSESSIONS\tSIZE\tLAST ACTIVE\tID")
			now := time.Now()
			for _, p := range projects {
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n",
					p.Name, p.Sessions, format.Size(p.SizeBytes), format.Ago(p.LastActive, now), p.ID)
			}
			return w.Flush()
		},
	}
}

func sessionsCmd(fl *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "sessions <project>",
		Short: "List sessions of a project (name or id)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(fl)
			if err != nil {
				return err
			}
			pid, err := resolveProject(a, args[0])
			if err != nil {
				return err
			}
			sessions, err := a.Sessions(pid)
			if err != nil {
				return err
			}
			if fl.jsonOut {
				return printJSON(cmd.OutOrStdout(), sessions)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tSIZE\tMSGS\tMODIFIED")
			now := time.Now()
			for _, s := range sessions {
				mark := ""
				if s.Active {
					mark = " ⚡"
				}
				fmt.Fprintf(w, "%s\t%s%s\t%s\t%d\t%s\n",
					s.ID[:8], s.Title, mark, format.Size(s.SizeBytes), s.Messages, format.Ago(s.Modified, now))
			}
			return w.Flush()
		},
	}
}

func confirmPlan(cmd *cobra.Command, fl *flags, p *app.Plan) (bool, error) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s → trash (%s):\n", p.Label, format.Size(p.SizeBytes))
	max := len(p.Details)
	if max > 10 {
		max = 10
	}
	for _, d := range p.Details[:max] {
		fmt.Fprintln(out, "  ", d)
	}
	if len(p.Details) > 10 {
		fmt.Fprintf(out, "   … and %d more\n", len(p.Details)-10)
	}
	if fl.dryRun {
		fmt.Fprintln(out, "dry-run: nothing moved")
		return false, nil
	}
	if fl.yes {
		return true, nil
	}
	in := bufio.NewReader(cmd.InOrStdin())
	fmt.Fprintf(out, "Move %d item(s) (%s) to trash? [y/N] ", len(p.Details), format.Size(p.SizeBytes))
	line, _ := in.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		fmt.Fprintln(out, "aborted")
		return false, nil
	}
	if p.ActiveCount > 0 {
		fmt.Fprintf(out, "%d active session(s) (modified <10 min ago) — really move them? [y/N] ", p.ActiveCount)
		line, _ := in.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Fprintln(out, "aborted")
			return false, nil
		}
	}
	return true, nil
}

func reportResult(cmd *cobra.Command, res *app.Result) {
	fmt.Fprintf(cmd.OutOrStdout(), "moved %d item(s) (%s) to trash entry %s — restore with: aism trash restore %s\n",
		res.Count, format.Size(res.SizeBytes), res.EntryID, res.EntryID)
}

func renameCmd(fl *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <session-id> <new title>",
		Short: "Set a session's title (appends an ai-title record)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(fl)
			if err != nil {
				return err
			}
			sid := args[0]
			title := strings.Join(args[1:], " ")
			pid, err := a.Claude.FindSession(sid)
			if err != nil {
				return err
			}
			if fl.dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would rename %s to %q\n", sid[:8], title)
				return nil
			}
			if err := a.Rename(pid, sid, title); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed %s → %q\n", sid[:8], title)
			return nil
		},
	}
}

func removeCmd(fl *flags) *cobra.Command {
	var project string
	c := &cobra.Command{
		Use:   "remove [<session-id>...]",
		Short: "Move sessions (or a whole project with --project) to trash",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(fl)
			if err != nil {
				return err
			}
			var plan *app.Plan
			switch {
			case project != "":
				pid, err := resolveProject(a, project)
				if err != nil {
					return err
				}
				if plan, err = a.GatherProject(pid); err != nil {
					return err
				}
			case len(args) > 0:
				// group by project
				byProj := map[string][]string{}
				for _, sid := range args {
					pid, err := a.Claude.FindSession(sid)
					if err != nil {
						return err
					}
					byProj[pid] = append(byProj[pid], sid)
				}
				pids := make([]string, 0, len(byProj))
				for pid := range byProj {
					pids = append(pids, pid)
				}
				sort.Strings(pids)
				for _, pid := range pids {
					sids := byProj[pid]
					p, err := a.GatherSessions(pid, sids)
					if err != nil {
						return err
					}
					if plan == nil {
						plan = p
					} else {
						plan.Paths = append(plan.Paths, p.Paths...)
						plan.Details = append(plan.Details, p.Details...)
						plan.SizeBytes += p.SizeBytes
						plan.Label = fmt.Sprintf("%d sessions", len(plan.Details))
					}
				}
			default:
				return fmt.Errorf("give session ids or --project (see 'aism sessions')")
			}
			ok, err := confirmPlan(cmd, fl, plan)
			if err != nil || !ok {
				return err
			}
			res, err := a.Execute(plan)
			if err != nil {
				return err
			}
			reportResult(cmd, res)
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "remove this whole project (memory/ is kept)")
	return c
}

var ageRe = regexp.MustCompile(`^(\d+)([dh])$`)

func parseAge(s string) (time.Duration, error) {
	m := ageRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("bad duration %q (use e.g. 30d or 12h)", s)
	}
	n, _ := strconv.Atoi(m[1])
	if m[2] == "d" {
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.Duration(n) * time.Hour, nil
}

func clearCmd(fl *flags) *cobra.Command {
	var olderThan string
	var empty, orphans bool
	c := &cobra.Command{
		Use:   "clear <project>",
		Short: "Bulk-clean a project's sessions (no flags = all non-active sessions)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(fl)
			if err != nil {
				return err
			}
			pid, err := resolveProject(a, args[0])
			if err != nil {
				return err
			}
			rules := claude.ClearRules{Empty: empty, Orphans: orphans}
			if olderThan != "" {
				if rules.OlderThan, err = parseAge(olderThan); err != nil {
					return err
				}
			}
			if !empty && !orphans && olderThan == "" {
				rules.All = true
			}
			plan, err := a.GatherClear(pid, rules)
			if err != nil {
				return err
			}
			ok, err := confirmPlan(cmd, fl, plan)
			if err != nil || !ok {
				return err
			}
			res, err := a.Execute(plan)
			if err != nil {
				return err
			}
			reportResult(cmd, res)
			return nil
		},
	}
	c.Flags().StringVar(&olderThan, "older-than", "", "only sessions older than e.g. 30d / 12h")
	c.Flags().BoolVar(&empty, "empty", false, "only trivial sessions (no assistant reply or <5 records)")
	c.Flags().BoolVar(&orphans, "orphans", false, "only orphan sidecar dirs")
	return c
}

func trashCmd(fl *flags) *cobra.Command {
	c := &cobra.Command{Use: "trash", Short: "List, restore, or empty the aism trash"}
	c.AddCommand(
		&cobra.Command{
			Use: "list", Short: "List trash entries",
			RunE: func(cmd *cobra.Command, args []string) error {
				troot, err := trash.DefaultRoot()
				if err != nil {
					return err
				}
				entries, err := trash.New(troot).List()
				if err != nil {
					return err
				}
				if fl.jsonOut {
					return printJSON(cmd.OutOrStdout(), entries)
				}
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
				fmt.Fprintln(w, "ENTRY\tTOOL\tLABEL\tITEMS")
				for _, e := range entries {
					fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", e.ID, e.Tool, e.Label, len(e.Items))
				}
				return w.Flush()
			},
		},
		&cobra.Command{
			Use: "restore <entry-id>", Short: "Move an entry's files back to their original paths",
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if fl.dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would restore %s\n", args[0])
					return nil
				}
				troot, err := trash.DefaultRoot()
				if err != nil {
					return err
				}
				if err := trash.New(troot).Restore(args[0]); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "restored", args[0])
				return nil
			},
		},
		&cobra.Command{
			Use: "empty", Short: "Permanently delete everything in the trash",
			RunE: func(cmd *cobra.Command, args []string) error {
				troot, err := trash.DefaultRoot()
				if err != nil {
					return err
				}
				if fl.dryRun {
					entries, err := trash.New(troot).List()
					if err != nil {
						return err
					}
					noun := "entries"
					if len(entries) == 1 {
						noun = "entry"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would permanently delete %d trash %s\n", len(entries), noun)
					return nil
				}
				if !fl.yes {
					fmt.Fprint(cmd.OutOrStdout(), "Permanently delete ALL trash entries? [y/N] ")
					line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
					if s := strings.ToLower(strings.TrimSpace(line)); s != "y" && s != "yes" {
						fmt.Fprintln(cmd.OutOrStdout(), "aborted")
						return nil
					}
				}
				if err := trash.New(troot).Empty(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "trash emptied")
				return nil
			},
		},
	)
	return c
}
