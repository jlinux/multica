package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var milestoneCmd = &cobra.Command{
	Use:   "milestone",
	Short: "Work with milestones: launch, first real use, continued use",
	Long: `Milestones track a goal past delivery.

Launch means the thing is available. First use means someone ran a real
end-to-end pass with it. Continued use means they are still running it. The
distance between the first two is what the goal layer exists to measure.`,
}

var milestoneShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a milestone",
	Args:  exactArgs(1),
	RunE:  runMilestoneShow,
}

var milestoneProposeCmd = &cobra.Command{
	Use:   "propose <milestone-id>",
	Short: "Report that a milestone has been reached, with evidence",
	Long: `Report that a milestone has been reached, with evidence.

This does not change the milestone. It records what you believe and why, and
hands the decision to a person — you cannot accept your own proposal, and
neither can any other agent. That is deliberate: a milestone is a claim that
something is genuinely done or genuinely used, and a system where the party
doing the work also certifies the work has stopped measuring anything.

Attach the run and the issue you were working on. They are what let a reviewer
open the execution log, the diff and the token cost behind your one-line
claim, instead of taking it on trust. Evidence with nothing to check is the
kind a reviewer learns to click past.`,
	Example: `  multica milestone propose 3f7a… \
    --status pending_accept \
    --evidence "Three tenants completed an end-to-end batch import; see access log query below." \
    --issue MUL-826`,
	Args: exactArgs(1),
	RunE: runMilestonePropose,
}

var milestoneProposalsCmd = &cobra.Command{
	Use:   "proposals <milestone-id>",
	Short: "List proposals on a milestone, decided ones included",
	Args:  exactArgs(1),
	RunE:  runMilestoneProposals,
}

func init() {
	milestoneProposeCmd.Flags().String("status", "pending_accept",
		"What you are proposing: pending_accept (ready for a decision) or achieved")
	milestoneProposeCmd.Flags().String("evidence", "", "What this is based on (required)")
	milestoneProposeCmd.Flags().String("date", "", "The date it happened, YYYY-MM-DD (required for achieved)")
	milestoneProposeCmd.Flags().String("issue", "", "The issue your run was working on")
	milestoneProposeCmd.Flags().String("task", "", "The task id of your run")
	milestoneProposeCmd.Flags().StringArray("ref", nil,
		"A structured pointer backing the claim, repeatable: --ref pr=4471 --ref query=log://…")
	for _, c := range []*cobra.Command{milestoneShowCmd, milestoneProposalsCmd} {
		c.Flags().String("output", "table", "Output format: table or json")
	}
	milestoneProposeCmd.Flags().String("output", "text", "Output format: text or json")
	milestoneCmd.AddCommand(milestoneShowCmd, milestoneProposeCmd, milestoneProposalsCmd)
}

func runMilestoneShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var m milestoneSummary
	if err := client.GetJSON(ctx, "/api/milestones/"+url.PathEscape(args[0]), &m); err != nil {
		return fmt.Errorf("get milestone: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, m)
	}

	fmt.Printf("%s  %s\n", m.Type, m.Title)
	fmt.Printf("  id       %s\n", m.ID)
	fmt.Printf("  goal     %s\n", m.GoalID)
	fmt.Printf("  status   %s\n", m.Status)
	fmt.Printf("  planned  %s\n", valueOr(m.PlannedDate, "—"))
	if m.ActualDate != nil {
		fmt.Printf("  landed   %s\n", *m.ActualDate)
	}
	if m.IsDelayed {
		fmt.Printf("  overdue  yes\n")
	}
	if m.RequiresAcceptance {
		// Said plainly, because it changes what an agent can usefully do next.
		fmt.Printf("  note     a person decides this one; propose, do not set it\n")
		if m.AdoptionCheck != nil {
			fmt.Printf("  decided  by %s\n", *m.AdoptionCheck)
		}
	}
	return nil
}

func runMilestonePropose(cmd *cobra.Command, args []string) error {
	evidence, _ := cmd.Flags().GetString("evidence")
	if strings.TrimSpace(evidence) == "" {
		// Refused here rather than at the server so the message can say what
		// evidence is for. A proposal without it is a guess wearing a claim's
		// clothes, and the reviewer has no way to tell the difference.
		return fmt.Errorf("--evidence is required: say what this is based on, so a reviewer can check it rather than trust it")
	}
	status, _ := cmd.Flags().GetString("status")
	date, _ := cmd.Flags().GetString("date")
	if status == "achieved" && date == "" {
		return fmt.Errorf("--date is required when proposing achieved: say when it happened")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"proposed_status": status,
		"evidence":        strings.TrimSpace(evidence),
	}
	if date != "" {
		body["proposed_actual_date"] = date
	}
	if v, _ := cmd.Flags().GetString("issue"); v != "" {
		body["source_issue_id"] = v
	}
	if v, _ := cmd.Flags().GetString("task"); v != "" {
		body["source_task_id"] = v
	}
	if refs, _ := cmd.Flags().GetStringArray("ref"); len(refs) > 0 {
		parsed := make([]map[string]string, 0, len(refs))
		for _, ref := range refs {
			kind, value, found := strings.Cut(ref, "=")
			if !found {
				return fmt.Errorf("--ref %q must look like kind=value, e.g. pr=4471", ref)
			}
			parsed = append(parsed, map[string]string{"kind": kind, "value": value})
		}
		body["evidence_refs"] = parsed
	}

	var created map[string]any
	if err := client.PostJSON(ctx,
		"/api/milestones/"+url.PathEscape(args[0])+"/proposals", body, &created); err != nil {
		return fmt.Errorf("propose milestone: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, created)
	}
	fmt.Printf("proposed %s on milestone %s\n", status, args[0])
	fmt.Println("waiting on a person; you cannot accept this yourself")
	return nil
}

func runMilestoneProposals(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result struct {
		Proposals []struct {
			ID             string          `json:"id"`
			ProposedStatus string          `json:"proposed_status"`
			State          string          `json:"state"`
			Evidence       string          `json:"evidence"`
			ProposedByType string          `json:"proposed_by_type"`
			DecideNote     string          `json:"decide_note"`
			EvidenceRefs   json.RawMessage `json:"evidence_refs"`
		} `json:"proposals"`
	}
	if err := client.GetJSON(ctx,
		"/api/milestones/"+url.PathEscape(args[0])+"/proposals", &result); err != nil {
		return fmt.Errorf("list proposals: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result.Proposals)
	}
	if len(result.Proposals) == 0 {
		fmt.Println("no proposals on this milestone")
		return nil
	}
	rows := make([][]string, 0, len(result.Proposals))
	for _, p := range result.Proposals {
		rows = append(rows, []string{
			shortID(p.ID), p.ProposedByType, p.ProposedStatus, p.State, truncate(p.Evidence, 46),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "BY", "PROPOSED", "STATE", "EVIDENCE"}, rows)
	return nil
}
