package tools

// Netskope Private Access: the publishers that terminate tunnels, the private
// apps they front, and the policy that decides who reaches what.
//
// Paths follow the REST API v2 layout where publisher infrastructure lives under
// /api/v2/infrastructure and the apps themselves under /api/v2/steering. Verify
// against your own tenant's Swagger (https://<tenant>/apidocs/?include_beta_routes=1)
// before trusting any one of them: Netskope moves routes between releases and the
// beta-routes flag changes which exist.

var crud = []Action{ActionList, ActionGet, ActionCreate, ActionUpdate, ActionDelete}

var npaResources = []Resource{
	{
		Name:       "netskope_publishers",
		Group:      "npa",
		Title:      "NPA publishers",
		Collection: "/api/v2/infrastructure/publishers",
		Actions:    crud,
		Description: "Netskope Private Access publishers: the software appliances deployed inside a " +
			"network that terminate NPA tunnels and reach private applications on behalf of users. " +
			"Endpoint: /api/v2/infrastructure/publishers.\n\n" +
			"A publisher's `status` and `stitcher_id` tell you whether it is actually connected to " +
			"the tenant; a publisher that exists but is not registered steers no traffic. " +
			"Create/update bodies take `name`, `lbrokerconnect`, `publisher_upgrade_profiles_id` and " +
			"`tags`. Deleting a publisher breaks every private app that lists it.",
	},
	{
		Name:       "netskope_publisher_upgrade_profiles",
		Group:      "npa",
		Title:      "NPA publisher upgrade profiles",
		Collection: "/api/v2/infrastructure/publisherupgradeprofiles",
		Actions:    crud,
		Description: "Maintenance windows that control when publishers take new releases. " +
			"Endpoint: /api/v2/infrastructure/publisherupgradeprofiles.\n\n" +
			"Bodies take `name`, `enabled`, `docker_tag`, `frequency` (a cron expression) and " +
			"`timezone`. A publisher with no profile upgrades on Netskope's own schedule, which is " +
			"usually not what a change-controlled environment wants.",
	},
	{
		Name:       "netskope_local_brokers",
		Group:      "npa",
		Title:      "NPA local brokers",
		Collection: "/api/v2/infrastructure/lbrokers",
		Actions:    crud,
		Description: "Local brokers, which keep NPA traffic inside a site instead of hairpinning it " +
			"through the Netskope cloud. Endpoint: /api/v2/infrastructure/lbrokers.\n\n" +
			"Bodies take `name` and the broker's registration details. Most tenants have none; an " +
			"empty list here is normal rather than a sign of a failed query.",
	},
	{
		Name:       "netskope_private_apps",
		Group:      "npa",
		Title:      "NPA private applications",
		Collection: "/api/v2/steering/apps/private",
		Actions:    crud,
		MaxItems:   100,
		Description: "Private applications reachable through NPA: the host/port definitions that " +
			"policy rules grant access to. Endpoint: /api/v2/steering/apps/private.\n\n" +
			"Bodies take `app_name`, `host` (a comma-separated list of FQDNs, CIDRs or wildcards), " +
			"`protocols` (each `{type: tcp|udp, port: \"443\"}`), `publishers` (the publishers that " +
			"serve it), `clientless_access`, `trust_self_signed_certs` and `tags`.\n\n" +
			"This is the largest collection in most tenants; filter with " +
			"`query.query` or `query.filter` rather than listing everything.",
	},
	{
		Name:       "netskope_private_app_tags",
		Group:      "npa",
		Title:      "NPA private app tags",
		Collection: "/api/v2/steering/apps/private/tags",
		Actions:    []Action{ActionList, ActionCreate, ActionUpdate, ActionDelete},
		Description: "Tags used to group private applications, which policy rules can then target " +
			"collectively. Endpoint: /api/v2/steering/apps/private/tags.\n\n" +
			"Tagging is how a rule stays correct as apps are added: a rule written against a tag " +
			"picks up new apps automatically, where one written against app ids does not.\n\n" +
			"`create` assigns tags to existing private apps rather than making a free-standing " +
			"tag, so the body needs both: `{\"ids\": [\"<private_app_id>\"], \"tags\": " +
			"[{\"tag_name\": \"<name>\"}]}`. A body carrying only a name is rejected with " +
			"422 `unfound parameters ids or tags`.",
	},
	{
		Name:       "netskope_npa_policy_rules",
		Group:      "npa",
		Title:      "NPA policy rules",
		Collection: "/api/v2/policy/npa/rules",
		Actions:    crud,
		// PUT has no gateway route here; only PATCH does.
		UpdateMethod: "PATCH",
		Description: "Access rules deciding which users and devices reach which private apps. " +
			"Endpoint: /api/v2/policy/npa/rules.\n\n" +
			"Bodies take `rule_name`, `description`, `enabled`, `action` (allow or block), " +
			"`group` (the policy group id), `rule_order`, and a `rule_data` object holding the " +
			"match criteria: `users`, `user_groups`, `organization_units`, `privateApps`, " +
			"`privateAppTags`, `classification`, `device_classification_id`.\n\n" +
			"Rule order is significant and the first match wins, so inserting a rule changes the " +
			"meaning of the ones below it. Read the current ordering before writing.\n\n" +
			"These endpoints are gated behind the tenant flag `npa_api_policy_enabled`, which is " +
			"off by default; if every call here 403s, that flag is why and Netskope support has to " +
			"turn it on.",
	},
	{
		Name:         "netskope_npa_policy_groups",
		Group:        "npa",
		Title:        "NPA policy groups",
		Collection:   "/api/v2/policy/npa/policygroups",
		Actions:      crud,
		UpdateMethod: "PATCH",
		Description: "Policy groups: the ordered containers that NPA rules live in. " +
			"Endpoint: /api/v2/policy/npa/policygroups.\n\n" +
			"Group order determines rule evaluation order across groups, so this is the coarse " +
			"knob to read first when a rule is not taking effect.",
	},
}
