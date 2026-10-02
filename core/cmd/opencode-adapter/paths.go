package main

// paths.go is §16.5's version dispatch and REQ-OC-5's "一家一份映射，别在业务层判版本",
// kept as data on purpose.
//
// The profile measured two generations of naming coexisting in 1.18.32 itself:
// `/session/{id}/abort` and `/api/session/{id}/interrupt` (5 and 11 literal hits), plus a
// `/revert` family. So the dispatch is a table keyed by observed version, not an if.
//
// What this table must never become is a client. §16.7 第 1 条's range is "观察，除此之外
// 一条命令都不发", and core/test/read_only_tier_test.go's criterion ① fails this package if
// a write-chain method name shows up in a call position. Recording the route strings here
// is the "路由存在、行为未验证" fact §16.5 asks to keep - Verified stays false for every
// entry because the product answered 401 to every probe (profile §④), and a true anywhere
// would be a claim nobody measured.

// interruptRoute is one version's stop/interrupt naming, as observed.
type interruptRoute struct {
	// Legacy is the pre-`/api` generation's path; Current is the `/api/...` generation.
	// Both exist in the version the profile measured, which is the whole reason this is a
	// pair of fields rather than one string per version.
	Legacy  string
	Current string
	// RevertFamily is the third naming the profile recorded, kept because a future
	// "undo the last step" capability would otherwise be re-discovered by accident.
	RevertFamily string
	// Verified is permanently false under §16.7: no request was ever sent, so no route has
	// observed behaviour behind it. Nothing may read "the path exists" as "stop works".
	Verified bool
	// Why is the measured reason Verified is false. It travels with the entry so a later
	// reader cannot quietly flip the flag without saying where the evidence came from.
	Why string
}

// interruptRoutes is keyed by the observed product version. The two entries are the two
// versions the survey role actually read (1.18.14 in Phase 0, 1.18.32 in the integration
// profile); anything else is an unknown schema and gets the unknown answer.
var interruptRoutes = map[string]interruptRoute{
	"1.18.32": {
		Legacy:       "/session/{id}/abort",
		Current:      "/api/session/{id}/interrupt",
		RevertFamily: "/revert/{stage,commit,clear}",
		Verified:     false,
		Why:          "every probe answered 401 (profile §④); no request has ever been sent",
	},
	"1.18.14": {
		Legacy:       "/session/{id}/abort",
		Current:      "/api/session/{id}/interrupt",
		RevertFamily: "/revert",
		Verified:     false,
		Why:          "Phase 0 measured both generations present and neither invoked",
	},
}

// routeFor answers the dispatch question. A version that is not in the table returns
// ok=false rather than a default: §16.5's point in making this a table is that an
// unmeasured version must not inherit another version's routes and look supported.
func routeFor(version string) (interruptRoute, bool) {
	r, ok := interruptRoutes[version]
	return r, ok
}

// stopAnswer is what the write chain says when a caller asks about interruption. It names
// the route it did NOT call, so the refusal is traceable to §16.5's measurement instead
// of looking like a generic "not implemented". The route strings are read from the table
// here and never passed into anything that could send them.
func stopAnswer(version string) error {
	r, known := routeFor(version)
	if !known {
		return unavailableRefusal(methodSessionStop, "observed version "+version+
			" has no measured route table (§16.5 refuses to inherit another version's paths)")
	}
	return unavailableRefusal(methodSessionStop, "routes exist, behaviour is unverified for "+version+": "+
		r.Legacy+" and "+r.Current+" - "+r.Why)
}
