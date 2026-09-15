package tools

// Secure Web Gateway steering and inline policy: the real-time protection rules,
// and the URL lists and custom categories those rules match against.

var policyResources = []Resource{
	{
		Name:       "netskope_url_lists",
		Group:      "policy",
		Title:      "URL lists",
		Collection: "/api/v2/policy/urllist",
		Actions:    crud,
		Description: "Named lists of URLs that real-time protection rules allow or block. " +
			"Endpoint: /api/v2/policy/urllist.\n\n" +
			"Bodies take `name` and a `data` object with `urls` (the entries) and `type` " +
			"(`exact` or `regex`).\n\n" +
			"Editing a list does NOT change live traffic on its own: changes sit in a pending " +
			"state until they are deployed. Use netskope_url_list_deploy afterwards, or the edit " +
			"will appear to have done nothing.",
	},
	{
		Name:       "netskope_custom_categories",
		Group:      "policy",
		Title:      "Custom URL categories",
		Collection: "/api/v2/profiles/customcategories",
		Actions:    crud,
		Description: "Custom URL categories, which group URL lists into something policy can match " +
			"by name alongside Netskope's built-in categories. " +
			"Endpoint: /api/v2/profiles/customcategories.\n\n" +
			"Bodies take `name` and the included/excluded URL list ids. Like URL lists, changes " +
			"need deploying before they affect traffic.",
	},
	{
		Name:  "netskope_realtime_policy_rules",
		Group: "policy",
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
			"For private-access policy use netskope_npa_policy_rules instead: these two are " +
			"different rulebooks and a private app will not appear here.\n\n" +
			"Exposed read-only: a malformed rule can block all egress for every steered user. " +
			"Make these changes in the Netskope UI, where the rule builder validates them.",
	},
	{
		Name:       "netskope_realtime_policy_groups",
		Group:      "policy",
		Title:      "Real-time protection policy groups",
		Collection: "/api/v2/policy/internetaccess/groups",
		Actions:    []Action{ActionList, ActionGet},
		Description: "The ordered groups that real-time protection rules live in. " +
			"Endpoint: /api/v2/policy/internetaccess/groups.\n\n" +
			"Group order determines rule evaluation order across groups, so read this first when " +
			"a rule is not taking effect. Read-only for the same reason as the rules themselves.",
	},
}
