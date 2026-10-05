package tools

// IPS: tenant-wide intrusion prevention switches. Opt-in, because each update
// changes threat blocking for every steered user at once.

var ipsResources = []Resource{
	{
		Name:         "netskope_ips_status",
		Group:        "ips",
		Title:        "IPS status",
		Collection:   "/api/v2/ips/status",
		Singleton:    true,
		Actions:      []Action{ActionGet, ActionUpdate},
		UpdateMethod: "PATCH",
		Description: "Whether IPS is enabled for the tenant. Endpoint: /api/v2/ips/status.\n\n" +
			"Update body: any of `web`, `nonweb`, `npa`, each a boolean. " +
			"Turning it off stops intrusion prevention for every user immediately.",
	},
	{
		Name:         "netskope_ips_allowlist",
		Group:        "ips",
		Title:        "IPS allow list",
		Collection:   "/api/v2/ips/allowlist",
		Singleton:    true,
		Actions:      []Action{ActionGet, ActionUpdate},
		UpdateMethod: "PATCH",
		Description: "Destinations IPS never inspects. Endpoint: /api/v2/ips/allowlist.\n\n" +
			"Update body: any of `src_ids`, `domain`, `dst_ids`. Read the list before updating: " +
			"an update may replace a field rather than add to it.",
	},
	{
		Name:       "netskope_ips_signature_overrides",
		Group:      "ips",
		Title:      "IPS signature overrides",
		Collection: "/api/v2/ips/signatureoverrides",
		Singleton:  true,
		Actions:    []Action{ActionGet, ActionUpdate},
		Description: "Per-signature action overrides (e.g. alert instead of block). " +
			"Endpoint: /api/v2/ips/signatureoverrides.\n\n" +
			"Update body: `{\"sig_id\": [...], \"override\": \"disabled\"|\"alert\"|\"reject\"}`. " +
			"Removing an override is POST /api/v2/ips/deletesignatureoverrides, which this tool " +
			"does not send. Look signatures up with netskope_api POST /api/v2/ips/getsignaturelist.",
	},
}
