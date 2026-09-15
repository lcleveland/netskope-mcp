package tools

// Reporting. Read-only by construction: these endpoints describe and run saved
// reports, and there is no case for a model authoring one.

var reportingResources = []Resource{
	{
		Name:       "netskope_reports",
		Group:      "reporting",
		Title:      "Saved reports",
		Collection: "/api/v2/reporting/aa/reports",
		Actions:    []Action{ActionList},
		Description: "Saved Advanced Analytics reports defined in the tenant, and their metadata. " +
			"Endpoint: /api/v2/reporting/aa/reports.\n\n" +
			"Use this to discover what reporting already exists before assembling the same numbers " +
			"by hand out of event searches. Running a report is asynchronous and returns a job " +
			"reference rather than data.\n\n" +
			"List-only: the tenant registers no item-level route, so a `get` on a report_id 404s. " +
			"The list already carries every field the item view would (report_id, report_name, " +
			"folder); open the report itself in Advanced Analytics.",
	},
}
