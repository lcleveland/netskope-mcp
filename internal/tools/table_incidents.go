package tools

// Incidents and user behaviour: watchlists that raise a user's visibility, and
// the triage state (status, assignee, severity) of DLP and UBA incidents.

var incidentResources = []Resource{
	{
		Name:         "netskope_watchlists",
		Group:        "incidents",
		Title:        "UBA watchlists",
		Collection:   "/api/v2/incidents/watchlists",
		Actions:      crud,
		UpdateMethod: "PATCH",
		Description: "Behaviour analytics watchlists: named sets of users whose activity is scored " +
			"and alerted on more closely. Endpoint: /api/v2/incidents/watchlists.\n\n" +
			"User confidence scores and anomalies are reads, through netskope_api: POST " +
			"/api/v2/incidents/uba/getuci and /api/v2/incidents/anomalies/getanomalies.",
	},
	{
		Name:         "netskope_incident_update",
		Group:        "incidents",
		Title:        "Update incident triage",
		Collection:   "/api/v2/incidents/update",
		Singleton:    true,
		Actions:      []Action{ActionUpdate},
		UpdateMethod: "PATCH",
		Description: "Change the triage fields (status, assignee, severity) of DLP incidents. " +
			"Endpoint: PATCH /api/v2/incidents/update, which takes no id: the incidents to change " +
			"are named inside `body`, as `{\"payload\": [{\"incident_id\": <integer>, " +
			"\"field\": \"status\", \"new_value\": \"...\", \"user\": \"<admin email>\"}]}`.\n\n" +
			"`incident_id` must be a JSON number; a quoted id is rejected. An unknown id comes back " +
			"as a 500 rather than a 404, and an unrecognised status value is stored as-is, so " +
			"copy values from an existing incident.\n\n" +
			"Find incidents first with netskope_event_search type=incident.",
	},
}
