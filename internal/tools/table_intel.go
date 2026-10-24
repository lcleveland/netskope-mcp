package tools

// URL and cloud-app intelligence: the "why is this blocked / is this app risky"
// lookups. netskope_url_lookup is a custom tool (intel.go) because its lookup is
// a POST whose name does not start with `get`.

var intelResources = []Resource{
	{
		Name:       "netskope_url_recategorizations",
		Group:      "intel",
		Title:      "URL recategorization requests",
		Collection: "/api/v2/nsiq/url/recategorizations",
		Actions:    []Action{ActionList, ActionGet, ActionCreate},
		// The route rejects a page larger than 10.
		MaxItems: 10,
		Description: "Requests asking Netskope to recategorize a URL it has filed under the wrong " +
			"category. Endpoint: /api/v2/nsiq/url/recategorizations.\n\n" +
			"`create` submits a request for Netskope's review; it does not change policy or " +
			"categories on this tenant, and cannot be withdrawn here. Body: `{\"email\": " +
			"\"requester@company.com\", \"recat_requests\": [{\"url\": \"https://example.com\", " +
			"\"suggested_categories\": [\"Technology\"]}], \"justification\": \"why\"}` (up to 5 " +
			"categories per URL, justification up to 2000 characters). Check the current category " +
			"with netskope_url_lookup first.\n\n" +
			"`list` covers the last 90 days, 10 per page: filter with `query.status` (received, " +
			"inprogress, completed, rejected), `query.starttime`/`query.endtime` (epoch seconds) and " +
			"page with `query.offset`. `get` takes the `task_id`.",
	},
	{
		Name:       "netskope_cci_apps",
		Group:      "intel",
		Title:      "Cloud app risk (CCI)",
		Collection: "/api/v2/services/cci/app",
		Singleton:  true,
		Actions:    []Action{ActionGet},
		Description: "Cloud Confidence Index lookup: Netskope's risk rating for a cloud app. " +
			"Endpoint: /api/v2/services/cci/app.\n\n" +
			"`query.apps` is required and must be the exact app name as Netskope indexes it " +
			"(e.g. `Dropbox`, `Box`, `Slack`); there is no listing or partial match, so try the " +
			"vendor's product name if a lookup comes back empty. Returns `cci` (0-100), `ccl` " +
			"(excellent, high, medium, low, poor), `category_name` and `organisation`. Optional " +
			"filters: `query.category`, `query.ccl`, `query.tag`, `query.discovered` (true for " +
			"shadow-IT apps seen on this tenant).",
	},
}
