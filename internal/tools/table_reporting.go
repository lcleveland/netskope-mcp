package tools

// Reporting. Read-only by construction: these endpoints describe and run saved
// reports, and there is no case for a model authoring one.

var reportingResources = []Resource{
	{
		Name:       "netskope_reports",
		Group:      "reporting",
		Title:      "Saved reports",
		Collection: "/api/v2/reporting/reports",
		Actions:    []Action{ActionList, ActionGet},
		Description: "Saved reports defined in the tenant, and their metadata. " +
			"Endpoint: /api/v2/reporting/reports.\n\n" +
			"Use this to discover what reporting already exists before assembling the same numbers " +
			"by hand out of event searches. Running a report is asynchronous and returns a job " +
			"reference rather than data.",
	},
}
