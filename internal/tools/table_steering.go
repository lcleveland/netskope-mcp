package tools

// Steering: the tunnels that bring branch-office traffic into Netskope without a
// client. Deleting or misconfiguring one drops a whole site's egress.

var steeringResources = []Resource{
	{
		Name:         "netskope_ipsec_tunnels",
		Group:        "steering",
		Title:        "IPsec tunnels",
		Collection:   "/api/v2/steering/ipsec/tunnels",
		Actions:      crud,
		UpdateMethod: "PATCH",
		Description: "IPsec tunnels steering site traffic into Netskope. " +
			"Endpoint: /api/v2/steering/ipsec/tunnels.\n\n" +
			"Create bodies need `site`, `srcipidentity`, `enable`, `bandwidth` and `pops`; list " +
			"filters include `query.site`, `query.pop` and `query.status`. " +
			"A tunnel terminates on two Netskope POPs (primary and failover); list the candidates " +
			"with netskope_api GET /api/v2/steering/ipsec/pops. Every user behind the site loses " +
			"egress if its tunnels go down, so read the current tunnel before updating it.",
	},
	{
		Name:         "netskope_gre_tunnels",
		Group:        "steering",
		Title:        "GRE tunnels",
		Collection:   "/api/v2/steering/gre/tunnels",
		Actions:      crud,
		UpdateMethod: "PATCH",
		Description: "GRE tunnels steering site traffic into Netskope. " +
			"Endpoint: /api/v2/steering/gre/tunnels.\n\n" +
			"Create bodies need the same fields as IPsec: `site`, `srcipidentity`, `enable`, " +
			"`bandwidth`, `pops`. POPs are listed by netskope_api GET /api/v2/steering/gre/pops. Same blast radius as " +
			"IPsec: every user behind the site loses egress if its tunnels go down.",
	},
}
