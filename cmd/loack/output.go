package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"loack/provider"
)

// This file renders loack's output in the style of the Terraform CLI:
// streaming per-resource progress lines ("Creating...", "Creation complete
// after 2s [id=...]") and a "Resources: N added, M changed, K destroyed."
// summary, with green/yellow/red coloring when stdout is a terminal.

// --- color ---------------------------------------------------------------

var useColor = colorEnabled()

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func green(s string) string  { return paint("32", s) }
func yellow(s string) string { return paint("33", s) }
func red(s string) string    { return paint("31", s) }
func bold(s string) string   { return paint("1", s) }

// --- generic lines -------------------------------------------------------

func outf(format string, a ...any) { fmt.Fprintf(os.Stdout, format+"\n", a...) }
func blank()                       { fmt.Fprintln(os.Stdout) }

func errorf(format string, a ...any) {
	fmt.Fprintln(os.Stderr, red("Error: ")+fmt.Sprintf(format, a...))
}

func idSuffix(id string) string {
	if id == "" {
		return ""
	}
	return fmt.Sprintf(" [id=%s]", id)
}

// roundDur formats an elapsed duration the way Terraform does: whole seconds.
func roundDur(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Round(time.Second).String()
}

// --- streaming progress observer ----------------------------------------

// observer turns provisioner Events into Terraform-style progress lines,
// tracking phase start times so it can report "complete after Ns".
type observer struct {
	started map[string]time.Time
}

func newObserver() *observer { return &observer{started: map[string]time.Time{}} }

func (o *observer) hook() provider.Hook { return o.handle }

func (o *observer) handle(e provider.Event) {
	switch e.Kind {
	case provider.EventRefreshing:
		outf("%s: Refreshing state...%s", e.Address, idSuffix(e.ID))
	case provider.EventCreating:
		o.started[e.Address] = time.Now()
		outf("%s: Creating...", e.Address)
	case provider.EventCreated:
		outf("%s: Creation complete after %s%s", e.Address, o.elapsed(e.Address), idSuffix(e.ID))
	case provider.EventModifying:
		o.started[e.Address] = time.Now()
		outf("%s: Modifying...%s", e.Address, idSuffix(e.ID))
	case provider.EventModified:
		outf("%s: Modifications complete after %s%s", e.Address, o.elapsed(e.Address), idSuffix(e.ID))
	case provider.EventDestroying:
		o.started[e.Address] = time.Now()
		outf("%s: Destroying...%s", e.Address, idSuffix(e.ID))
	case provider.EventDestroyed:
		outf("%s: Destruction complete after %s", e.Address, o.elapsed(e.Address))
	}
}

func (o *observer) elapsed(addr string) string {
	if t, ok := o.started[addr]; ok {
		return roundDur(time.Since(t))
	}
	return "0s"
}

// --- summaries -----------------------------------------------------------

func applySummary(added, changed, destroyed int) {
	blank()
	outf("%s Resources: %s added, %s changed, %s destroyed.",
		bold("Apply complete!"),
		green(fmt.Sprintf("%d", added)),
		yellow(fmt.Sprintf("%d", changed)),
		red(fmt.Sprintf("%d", destroyed)),
	)
}

func destroySummary(destroyed int) {
	blank()
	outf("%s Resources: %s destroyed.",
		bold("Destroy complete!"),
		red(fmt.Sprintf("%d", destroyed)),
	)
}

func noChanges(msg string) {
	blank()
	outf("%s %s", bold("No changes."), msg)
}

// --- plan ----------------------------------------------------------------

func planSummary(add, change, destroy int) {
	blank()
	outf("%s %s to add, %s to change, %s to destroy.",
		bold("Plan:"),
		green(fmt.Sprintf("%d", add)),
		yellow(fmt.Sprintf("%d", change)),
		red(fmt.Sprintf("%d", destroy)),
	)
}

// showPlan renders a change set in Terraform style and returns the counts. With
// no changes it prints "No changes." and returns zeros.
func showPlan(changes []change) (add, chg, del int) {
	add, chg, del = planCounts(changes)
	if add == 0 && chg == 0 && del == 0 {
		noChanges("Your infrastructure matches the configuration.")
		return 0, 0, 0
	}
	blank()
	outf("loack will perform the following actions:")
	blank()
	for _, c := range changes {
		renderChange(c)
	}
	planSummary(add, chg, del)
	return add, chg, del
}

func renderChange(c change) {
	switch c.Action {
	case aCreate:
		outf("  %s %s will be created", green("+"), c.display())
		for _, line := range yamlLines(c.desiredYAML) {
			outf("      %s %s", green("+"), line)
		}
	case aUpdate:
		outf("  %s %s will be updated in-place", yellow("~"), c.display())
		for _, fc := range c.fieldChanges {
			outf("      %s %s = %s %s %s", yellow("~"), fc.Path, fc.Old, yellow("->"), fc.New)
		}
	case aDestroy:
		outf("  %s %s will be destroyed", red("-"), c.display())
	case aSecretCreate:
		outf("  %s %s will be stored in loack state", green("+"), c.display())
		for _, k := range c.secretKeys {
			outf("      %s %s = %s", green("+"), k, "(sensitive value hidden)")
		}
	case aSecretUpdate:
		outf("  %s %s will be updated in loack state", yellow("~"), c.display())
		for _, k := range c.secretKeys {
			outf("      %s %s = %s", yellow("~"), k, "(sensitive value hidden)")
		}
	case aSecretDelete:
		outf("  %s %s will be removed from loack state", red("-"), c.display())
	}
}

// confirm prompts for the literal "yes", Terraform-style.
func confirm(prompt string) bool {
	blank()
	outf("%s", prompt)
	outf("  Only %q will be accepted to confirm.", "yes")
	blank()
	fmt.Fprint(os.Stdout, "  Enter a value: ")
	var answer string
	fmt.Fscanln(os.Stdin, &answer)
	return strings.TrimSpace(answer) == "yes"
}

// savedPlanHint mirrors Terraform's "saved the plan" footer.
func savedPlanHint(path string) {
	blank()
	outf("Saved the plan to: %s", path)
	blank()
	outf("To perform exactly these actions, run the following command to apply:")
	outf("    loack apply %s", path)
}

func yamlLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// indentYAML prints a YAML document indented as a Terraform-style "show" block.
func showYAML(address string, data []byte) {
	if len(data) == 0 {
		return
	}
	blank()
	outf("# %s:", address)
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		outf("  %s", line)
	}
}
