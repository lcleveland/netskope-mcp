package tools

// AI Gateway: the providers, MCP servers and limits the gateway brokers. Opt-in.
// AIG tokens are deliberately absent: they are credentials.

var aigResources = []Resource{
	{
		Name: "netskope_aig_ai_providers", Group: "aig", Title: "AI Gateway providers",
		Collection: "/api/v2/aig/aiproviders", Actions: crud, UpdateMethod: "PATCH",
		Description: "LLM providers the AI Gateway routes to. Endpoint: /api/v2/aig/aiproviders.\n\n" +
			"Predefined providers are read-only; create, update and delete apply to custom ones.",
	},
	{
		Name: "netskope_aig_mcp_servers", Group: "aig", Title: "AI Gateway MCP servers",
		Collection: "/api/v2/aig/mcpservers", Actions: crud, UpdateMethod: "PATCH",
		Description: "MCP servers the AI Gateway brokers. Endpoint: /api/v2/aig/mcpservers.\n\n" +
			"Predefined servers are read-only; create, update and delete apply to custom ones.",
	},
	{
		Name: "netskope_aig_rate_limits", Group: "aig", Title: "AI Gateway rate limits",
		Collection: "/api/v2/aig/ratelimits", Actions: crud, UpdateMethod: "PATCH",
		Description: "Rate limit rules on AI Gateway traffic. Endpoint: /api/v2/aig/ratelimits.",
	},
	{
		Name: "netskope_aig_token_groups", Group: "aig", Title: "AI Gateway token groups",
		Collection: "/api/v2/aig/tokengroups", Actions: crud, UpdateMethod: "PATCH",
		Description: "Groups that AI Gateway tokens belong to, which rate limits and policy target. " +
			"Endpoint: /api/v2/aig/tokengroups.\n\n" +
			"The tokens themselves are credentials and are not exposed by this server.",
	},
}
