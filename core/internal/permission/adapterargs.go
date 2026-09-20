package permission

import (
	"regexp"
	"strings"
)

// adapterArgsRe is the deny list for "skip approval / sandbox / permission"
// switches (adapter protocol §9). C2 ruling 2026-09-19 put this rule set with
// audit/permission: orchestration and automatic retries may never enable these,
// so the check is a hard gate, not a policy rung that a rule or grant could
// outvote. Observed in Phase 0: WorkBuddy --auth none, Codex
// --dangerously-bypass-approvals-and-sandbox, ZCode/WorkBuddy bypassPermissions.
//
// ponytail: substring/regex list from the flags Phase 0 actually found; add
// entries per adapter as the survey turns up new spellings rather than guessing
// a general parser.
var adapterArgsRe = regexp.MustCompile(`(?i)` +
	`bypass|` + // bypassPermissions, --dangerously-bypass-approvals-and-sandbox
	`dangerously|` +
	// --auth none / --auth=none / --auth-mode none / {"auth":"none"}
	`\bauth[\w-]*["']?\s*[=:]?\s*["']?none|` +
	`--?\s*(no|skip|disable)[-_]?sandbox|` +
	`--?\s*(auto|no)[-_]?approv|` +
	`sandbox["'=:_-]*\s*(off|disabled?|false)|` +
	`--?\s*yolo`)

// CheckAdapterArgs returns the arguments that carry a denied switch. Callers
// pass every launch argument AND the serialized adapter config, since a bypass
// key may arrive as JSON rather than a flag. A non-empty result means: refuse
// the launch and record audit_entries with OutcomeDenied (§9).
//
// Matching runs over the joined command line so a two-token switch
// ("--auth" "none") is caught, and each hit is attributed to the token the
// match actually starts in — naming the innocent token before it would put the
// wrong argument in the audit record.
func CheckAdapterArgs(args ...string) []string {
	joined := strings.Join(args, " ")
	locs := adapterArgsRe.FindAllStringIndex(joined, -1)
	if len(locs) == 0 {
		return nil
	}
	starts := make([]int, len(args))
	off := 0
	for i, a := range args {
		starts[i] = off
		off += len(a) + 1 // the join separator
	}
	var hits []string
	seen := map[int]bool{}
	for _, l := range locs {
		owner := 0
		for i, s := range starts {
			if s > l[0] {
				break
			}
			owner = i
		}
		if !seen[owner] {
			seen[owner] = true
			hits = append(hits, args[owner])
		}
	}
	return hits
}
