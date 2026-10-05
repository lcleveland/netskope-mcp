package tools

// Secure Web Gateway steering and inline policy: the real-time protection rules,
// and the URL lists, categories and profiles those rules match against.

// deployable is a profile collection whose edits sit pending until deployed,
// and whose objects can each have their pending edits reverted.
var deployable = []Action{ActionList, ActionGet, ActionCreate, ActionUpdate, ActionDelete, ActionDeploy, ActionRevert}

// pendingNote is the caveat every deployable collection shares.
const pendingNote = "\n\nEdits sit pending until `deploy`, which pushes every pending change in this " +
	"collection live; `revert` discards one object's pending edits. An edit that is never " +
	"deployed appears to have done nothing."

// liveNote replaces pendingNote where the tenant was seen to apply API writes at
// once: create, update and delete all came back `applied`, revert 409d and
// deploy pushed nothing. Deploy still matters for edits staged in the UI.
const liveNote = "\n\nCreate, update and delete through this API take effect IMMEDIATELY: there is no " +
	"pending stage to review or back out of. `deploy` only pushes edits someone staged in " +
	"the admin console, and needs a body naming them: `{\"ids\": [<ids>]}`. `revert` " +
	"discards one object's staged console edit; it 409s on anything not pending."

var policyResources = []Resource{
	{
		Name:       "netskope_url_lists",
		Group:      "policy",
		Title:      "URL lists",
		Collection: "/api/v2/policy/urllist",
		Actions:    []Action{ActionList, ActionGet, ActionCreate, ActionUpdate, ActionDelete, ActionDeploy},
		Description: "Named lists of URLs that real-time protection rules allow or block. " +
			"Endpoint: /api/v2/policy/urllist.\n\n" +
			"Bodies take `name` and a `data` object with `urls` (the entries) and `type` " +
			"(`exact` or `regex`).\n\n" +
			"Editing a list does NOT change live traffic on its own: changes sit pending until " +
			"`deploy`, which pushes every pending URL list change live. A `delete` is pending too: " +
			"the list stays, marked deleted, until the next deploy. Netskope is migrating URL " +
			"lists to destination profiles (netskope_destination_profiles) for new policy.",
	},
	{
		Name:       "netskope_custom_categories",
		Group:      "policy",
		Title:      "Custom URL categories",
		Collection: "/api/v2/profiles/customcategories",
		Actions:    deployable,
		// PATCH only: there is no item-level PUT route.
		UpdateMethod: "PATCH",
		Description: "Custom URL categories, which group URL lists and destination profiles into " +
			"something policy can match by name alongside Netskope's built-in categories. " +
			"Endpoint: /api/v2/profiles/customcategories.\n\n" +
			"Bodies take `name` and the included/excluded list or profile ids. A URL list that is " +
			"still pending create cannot be included (400) until it is deployed." + liveNote,
	},
	{
		Name:         "netskope_destination_profiles",
		Group:        "policy",
		Title:        "Destination profiles",
		Collection:   "/api/v2/profiles/destinations",
		Actions:      deployable,
		UpdateMethod: "PATCH",
		// The tenant 400s a limit above 100.
		MaxItems: 100,
		Description: "Destination profiles: named sets of domains, URLs and IPs that policy and " +
			"custom categories match on; the successor to URL lists. " +
			"Endpoint: /api/v2/profiles/destinations.\n\n" +
			"Create bodies need `name` and `type`.\n\n" +
			"Adding or removing single values without rewriting the list goes through " +
			"PATCH {id}/values, which this tool does not send. `update` cannot change `values` at " +
			"all (400); it edits name, description and labels. " +
			"Check what a URL matches with netskope_api POST " +
			"/api/v2/profiles/destinations/getevaluation." + liveNote,
	},
	{
		Name:         "netskope_network_profiles",
		Group:        "policy",
		Title:        "Network profiles",
		Collection:   "/api/v2/profiles/networks",
		Actions:      deployable,
		UpdateMethod: "PATCH",
		Description: "Network profiles: named sets of source or destination IPs, ranges and CIDRs " +
			"for policy to match on. Endpoint: /api/v2/profiles/networks." + pendingNote,
	},
	{
		Name:         "netskope_service_profiles",
		Group:        "policy",
		Title:        "Service profiles",
		Collection:   "/api/v2/profiles/serviceobjects",
		Actions:      deployable,
		UpdateMethod: "PATCH",
		// The tenant 400s a limit above 150.
		MaxItems: 150,
		Description: "Service profiles: named protocol/port sets for firewall policy. " +
			"Endpoint: /api/v2/profiles/serviceobjects.\n\n" +
			"Create bodies need `name`, `description` and `protocols`. A protocol with no ports " +
			"means any port: omit the port key rather than sending an empty list." + liveNote +
			" Service profile deploy also requires `change_note`.",
	},
	{
		Name:         "netskope_dns_profiles",
		Group:        "policy",
		Title:        "DNS profiles",
		Collection:   "/api/v2/profiles/dns",
		Actions:      []Action{ActionList, ActionGet, ActionCreate, ActionUpdate, ActionDelete, ActionDeploy},
		UpdateMethod: "PATCH",
		// The tenant 400s a limit above 150.
		MaxItems: 150,
		Description: "DNS security profiles: domain category, record type and tunnelling " +
			"controls applied to DNS traffic. Endpoint: /api/v2/profiles/dns.\n\n" +
			"Create bodies need `name`. Edits sit pending until `deploy`, whose body names what " +
			"to push: `{\"ids\": [<profile ids>]}` or `{\"all\": true}`, plus an optional " +
			"`change_note`. " +
			"Category, record-type and tunnel ids come from netskope_api GET " +
			"/api/v2/profiles/dns/domaincategories, /recordtypes and /tunnels.",
	},
	{
		Name:         "netskope_remote_proxies",
		Group:        "policy",
		Title:        "Remote proxies",
		Collection:   "/api/v2/profiles/remoteproxies",
		Actions:      deployable,
		UpdateMethod: "PATCH",
		Description: "Remote (upstream) proxies that Netskope forwards selected traffic through. " +
			"Endpoint: /api/v2/profiles/remoteproxies.\n\n" +
			"The API is in Beta at Netskope and has 403d under a full-access role, so a 403 " +
			"here is likely the tenant, not the role. Filter with `query.jql`: attributes id, name, description, host, " +
			"status, label_ids; operators =, IN (\"a\",\"b\"), ~ (contains); up to 5 joined with AND, " +
			"e.g. `name ~ \"proxy\" AND status = \"applied\"`." + pendingNote,
	},
	{
		Name:         "netskope_domain_frontings",
		Group:        "policy",
		Title:        "Domain fronting exceptions",
		Collection:   "/api/v2/policy/domainfrontings",
		Actions:      crud,
		UpdateMethod: "PATCH",
		Description: "Exceptions to domain-fronting detection: SNI/Host mismatches Netskope should " +
			"allow. Endpoint: /api/v2/policy/domainfrontings.\n\n" +
			"Each exception is a hole in fronting detection; keep them as narrow as possible.",
	},
	{
		Name:  "netskope_realtime_policy_rules",
		Group: "internetaccess",
		Title: "Real-time protection policy rules",
		// The UI's "Real-time Protection" is "internet access" in REST API v2;
		// there is no /policy/realtime or /policy/inline route.
		Collection: "/api/v2/policy/internetaccess/rules",
		Actions:    []Action{ActionList, ActionGet},
		Description: "Inline policy rules for web and cloud app traffic -- what the UI calls " +
			"Real-time Protection. Endpoint: /api/v2/policy/internetaccess/rules.\n\n" +
			"Rule order is significant and the first match wins, within the ordered groups that " +
			"netskope_realtime_policy_groups lists. This returns the current configuration, " +
			"which can include rules edited but not yet deployed; /rules/applied is what is live " +
			"on the data plane.\n\n" +
			"Filter with `query.jql`, not `filter`: attributes id, group_id, name, enabled, status; " +
			"operators = (exact), IN (\"a\",\"b\") and ~ (contains), joined with AND, e.g. " +
			"`name ~ \"eng\" AND enabled = true`. `limit` defaults to 10, max 1000.\n\n" +
			"For private-access policy use netskope_npa_policy_rules instead: these two are " +
			"different rulebooks and a private app will not appear here.\n\n" +
			"Exposed read-only: a malformed rule can block all egress for every steered user. " +
			"Make these changes in the Netskope UI, where the rule builder validates them.",
	},
	{
		Name:       "netskope_realtime_policy_groups",
		Group:      "internetaccess",
		Title:      "Real-time protection policy groups",
		Collection: "/api/v2/policy/internetaccess/groups",
		Actions:    []Action{ActionList, ActionGet},
		Description: "The ordered groups that real-time protection rules live in. " +
			"Endpoint: /api/v2/policy/internetaccess/groups.\n\n" +
			"Group order determines rule evaluation order across groups, so read this first when " +
			"a rule is not taking effect. Read-only for the same reason as the rules themselves.",
	},
}
