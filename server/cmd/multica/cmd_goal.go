package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// The goal layer's CLI surface.
//
// It is deliberately read-heavy. An agent's job around a goal is to understand
// what the work is for and, when it finishes, to say what it believes changed —
// with evidence, for a person to decide. It has no command to accept a
// milestone, edit a goal or move a date, because those are judgements the
// server refuses from an agent anyway; offering them here would only produce a
// 403 an agent has to learn by hitting.

var goalCmd = &cobra.Command{
	Use:   "goal",
	Short: "Work with goals: the planning tier above issues",
	Long: `Goals answer what a stretch of work is for.

They are never assignable: an agent works on the issues linked to a cycle
goal, not on the goal itself. Use these commands to see what the work you are
doing serves, and 'multica milestone propose' to report what changed.`,
}

var goalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List goals in the workspace",
	RunE:  runGoalList,
}

var goalShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a goal with its alignment and milestones",
	Args:  exactArgs(1),
	RunE:  runGoalShow,
}

var goalIssuesCmd = &cobra.Command{
	Use:   "issues <id>",
	Short: "List the issues delivering a goal",
	Args:  exactArgs(1),
	RunE:  runGoalIssues,
}

var goalForIssueCmd = &cobra.Command{
	Use:   "for-issue <issue>",
	Short: "Show which goals an issue serves",
	Long: `Show which goals an issue serves.

This is the one to reach for first when you pick up an issue: it says what the
work is ultimately for, which is context no issue body reliably carries.`,
	Args: exactArgs(1),
	RunE: runGoalForIssue,
}

func init() {
	goalListCmd.Flags().Int("level", 0, "Only this tier: 1 direction, 2 product goal, 3 cycle goal")
	goalListCmd.Flags().String("status", "", "Filter by status")
	goalListCmd.Flags().String("project", "", "Filter by project id")
	goalListCmd.Flags().Bool("orphan-only", false, "Only goals that align to nothing")
	// Registered on every command: an agent reading this output is parsing it,
	// and a table it has to scrape is a contract nobody wrote down.
	for _, c := range []*cobra.Command{goalListCmd, goalShowCmd, goalIssuesCmd, goalForIssueCmd} {
		c.Flags().String("output", "table", "Output format: table or json")
	}
	goalCmd.AddCommand(goalListCmd, goalShowCmd, goalIssuesCmd, goalForIssueCmd)
}

func runGoalList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	if v, _ := cmd.Flags().GetInt("level"); v != 0 {
		params.Set("level", strconv.Itoa(v))
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		params.Set("project_id", v)
	}
	if v, _ := cmd.Flags().GetBool("orphan-only"); v {
		params.Set("orphan_only", "true")
	}

	var result struct {
		Goals []goalSummary `json:"goals"`
	}
	if err := client.GetJSON(ctx, "/api/goals?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("list goals: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result.Goals)
	}

	headers := []string{"ID", "TIER", "TITLE", "STATUS", "ALIGNS TO"}
	rows := make([][]string, 0, len(result.Goals))
	for _, g := range result.Goals {
		aligns := "—"
		if g.ParentGoalID != nil {
			aligns = shortID(*g.ParentGoalID)
		} else if g.Level > 1 {
			aligns = "(stands alone)"
		}
		rows = append(rows, []string{
			shortID(g.ID), tierName(g.Level), truncate(g.Title, 44), g.Status, aligns,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runGoalShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var goal goalSummary
	if err := client.GetJSON(ctx, "/api/goals/"+url.PathEscape(args[0]), &goal); err != nil {
		return fmt.Errorf("get goal: %w", err)
	}

	var milestones struct {
		Milestones []milestoneSummary `json:"milestones"`
	}
	if err := client.GetJSON(ctx, "/api/goals/"+url.PathEscape(args[0])+"/milestones", &milestones); err != nil {
		return fmt.Errorf("list milestones: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"goal": goal, "milestones": milestones.Milestones,
		})
	}

	fmt.Printf("%s  %s\n", tierName(goal.Level), goal.Title)
	fmt.Printf("  id       %s\n", goal.ID)
	fmt.Printf("  status   %s\n", goal.Status)
	if goal.Cycle != "" {
		fmt.Printf("  cycle    %s\n", goal.Cycle)
	}
	if goal.ParentGoalID != nil {
		fmt.Printf("  aligns   %s\n", *goal.ParentGoalID)
	} else if goal.Level > 1 && goal.OrphanReason != "" {
		fmt.Printf("  alone    %s\n", goal.OrphanReason)
	}
	// Stated on every goal, because it is the thing most likely to be assumed
	// wrong: this is not something you can be assigned.
	fmt.Printf("  note     a goal is never assignable; work on the issues under it\n")

	if len(milestones.Milestones) == 0 {
		fmt.Println("\nno milestones yet")
		return nil
	}
	fmt.Println("\nMILESTONES")
	rows := make([][]string, 0, len(milestones.Milestones))
	for _, m := range milestones.Milestones {
		when := valueOr(m.PlannedDate, "—")
		if m.ActualDate != nil {
			when = *m.ActualDate + " (landed)"
		}
		flags := []string{}
		if m.IsDelayed {
			flags = append(flags, "overdue")
		}
		if m.RequiresAcceptance {
			flags = append(flags, "needs acceptance")
		}
		rows = append(rows, []string{
			shortID(m.ID), m.Type, truncate(m.Title, 34), m.Status, when, strings.Join(flags, ", "),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "STAGE", "TITLE", "STATUS", "DATE", ""}, rows)
	return nil
}

func runGoalIssues(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result struct {
		Issues []struct {
			ID           string  `json:"id"`
			Number       int     `json:"number"`
			Title        string  `json:"title"`
			Status       string  `json:"status"`
			AssigneeType *string `json:"assignee_type"`
		} `json:"issues"`
	}
	if err := client.GetJSON(ctx, "/api/goals/"+url.PathEscape(args[0])+"/issues", &result); err != nil {
		return fmt.Errorf("list goal issues: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result.Issues)
	}
	rows := make([][]string, 0, len(result.Issues))
	for _, i := range result.Issues {
		rows = append(rows, []string{
			"#" + strconv.Itoa(i.Number), truncate(i.Title, 48), i.Status, valueOr(i.AssigneeType, "—"),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ISSUE", "TITLE", "STATUS", "ASSIGNEE"}, rows)
	return nil
}

func runGoalForIssue(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result struct {
		Goals []goalSummary `json:"goals"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(args[0])+"/goals", &result); err != nil {
		return fmt.Errorf("list goals for issue: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result.Goals)
	}
	if len(result.Goals) == 0 {
		fmt.Println("this issue is not linked to a goal")
		return nil
	}
	rows := make([][]string, 0, len(result.Goals))
	for _, g := range result.Goals {
		rows = append(rows, []string{shortID(g.ID), tierName(g.Level), truncate(g.Title, 48), g.Status})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "TIER", "TITLE", "STATUS"}, rows)
	return nil
}

type goalSummary struct {
	ID           string  `json:"id"`
	Level        int     `json:"level"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	Cycle        string  `json:"cycle"`
	ParentGoalID *string `json:"parent_goal_id"`
	OrphanReason string  `json:"orphan_reason"`
	Assignable   bool    `json:"assignable"`
}

type milestoneSummary struct {
	ID                 string  `json:"id"`
	GoalID             string  `json:"goal_id"`
	Type               string  `json:"type"`
	Title              string  `json:"title"`
	Status             string  `json:"status"`
	PlannedDate        *string `json:"planned_date"`
	ActualDate         *string `json:"actual_date"`
	IsDelayed          bool    `json:"is_delayed"`
	RequiresAcceptance bool    `json:"requires_acceptance"`
	AdoptionCheck      *string `json:"adoption_check"`
}

func tierName(level int) string {
	switch level {
	case 1:
		return "direction"
	case 2:
		return "product"
	case 3:
		return "cycle"
	}
	return "level" + strconv.Itoa(level)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func valueOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
