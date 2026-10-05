package tools

// Device classification: the rules that label a device managed or unmanaged,
// which policy then matches on. Opt-in.

var deviceResources = []Resource{
	{
		Name:        "netskope_device_classification_rules",
		Group:       "devices",
		Title:       "Device classification rules",
		Collection:  "/api/v2/deviceclassification/rules",
		Actions:     crud,
		CreateArray: true,
		Description: "Rules that classify devices (e.g. managed vs unmanaged) from posture checks. " +
			"Endpoint: /api/v2/deviceclassification/rules.\n\n" +
			"Create bodies take `name`, `label`, `os` and `conditions`; send one rule as an " +
			"object, the tool wraps it in the array the route wants. `conditions` must nest each " +
			"check three levels deep, `{\"$and\": [{\"$or\": [{\"$and\": [{\"domain_check\": ...}]}]}]}`, " +
			"even though older rules read back with two; shallower is a 400. Create returns no object: " +
			"list with `query.label` to find the new rule. Policy rules match on the resulting " +
			"classification, so a rule change can move every device in or out of a policy at once.",
	},
	{
		Name:        "netskope_device_classification_tags",
		Group:       "devices",
		Title:       "Device classification labels",
		Collection:  "/api/v2/deviceclassification/tags",
		Actions:     crud,
		CreateArray: true,
		Description: "The labels device classification rules assign. " +
			"Endpoint: /api/v2/deviceclassification/tags.\n\n" +
			"Create bodies take `name`; send one label as an object, the tool wraps it in the " +
			"array the route wants. Label priority is changed by a separate PATCH " +
			"this tool does not send; use the UI to reorder.",
	},
}
