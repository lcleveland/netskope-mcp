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
		Collection: "/api/v2/policy/customcategory",
		Actions:    crud,
		Description: "Custom URL categories, which group URL lists into something policy can match " +
			"by name alongside Netskope's built-in categories. " +
			"Endpoint: /api/v2/policy/customcategory.\n\n" +
			"Bodies take `name` and the included/excluded URL list ids. Like URL lists, changes " +
			"need deploying before they affect traffic.",
	},
	{
		Name:  "netskope_realtime_policy_rules",
		Group: "policy",
		Title: "Real-time protection policy rules",
		// TODO: unverified, and identical to netskope_npa_policy_rules' collection --
		// so this tool currently returns NPA private-access rules, not inline ones.
		// Check the real inline-policy route against a tenant's own Swagger
		// (https://<tenant>/apidocs/?include_beta_routes=1) and correct it, or drop
		// the resource. The description below warns the model until that happens.
		Collection: "/api/v2/policy/npa/rules",
		Actions:    []Action{ActionList, ActionGet},
		Description: "Inline (real-time protection) policy rules for web and cloud app traffic.\n\n" +
			"WARNING: the endpoint behind this tool is unverified and may return NPA " +
			"private-access rules rather than inline ones. Confirm against the tenant's own " +
			"Swagger before relying on the result, and prefer netskope_npa_policy_rules when " +
			"you actually want private-access policy.\n\n" +
			"Exposed read-only: the write path for inline policy differs materially between " +
			"tenants and a malformed rule can block all egress for every steered user. Make these " +
			"changes in the Netskope UI, where the rule builder validates them.",
	},
}
