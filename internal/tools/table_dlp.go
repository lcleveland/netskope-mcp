package tools

// Data loss prevention: the profiles policy rules reference, and the rules,
// file profiles and identifiers those profiles are built from.

// dlpGateNote is on every DLP tool: the 403 it explains reads like a role
// problem, and a model that has not been told otherwise will chase the role.
const dlpGateNote = "\n\nThe DLP API is still in development at Netskope and must be enabled per " +
	"tenant by the account team. Until it is, every call 403s with DLP_API_ERROR " +
	"\"Permission Error\" whatever the token's role; do not suggest widening the role."

var dlpResources = []Resource{
	{
		Name:       "netskope_dlp_profiles",
		Group:      "dlp",
		Title:      "DLP profiles",
		Collection: "/api/v2/services/dlp/profiles",
		Actions:    []Action{ActionList, ActionGet, ActionCreate, ActionUpdate, ActionDelete, ActionDeploy},
		Description: "DLP profiles: the named bundles of content rules and file profiles that " +
			"real-time and API protection policies match on. Endpoint: /api/v2/services/dlp/profiles.\n\n" +
			"Edits to profiles, rules, file profiles and data identifiers are pending until " +
			"`deploy`, which pushes all pending DLP changes at once. Every policy that names a " +
			"profile changes behaviour when it deploys, so list its rules before editing." + dlpGateNote,
	},
	{
		Name:       "netskope_dlp_rules",
		Group:      "dlp",
		Title:      "DLP content rules",
		Collection: "/api/v2/services/dlp/rules/content",
		Actions:    crud,
		Description: "DLP content rules: the match conditions (data identifiers, dictionaries, " +
			"thresholds, severity) that DLP profiles combine. Endpoint: /api/v2/services/dlp/rules/content.\n\n" +
			"Changes go live only after netskope_dlp_profiles `deploy`." + dlpGateNote,
	},
	{
		Name:       "netskope_dlp_file_profiles",
		Group:      "dlp",
		Title:      "DLP file profiles",
		Collection: "/api/v2/services/dlp/fileprofiles",
		Actions:    crud,
		Description: "File profiles: file type, size, name and hash conditions used by DLP profiles " +
			"and file-type policy. Endpoint: /api/v2/services/dlp/fileprofiles.\n\n" +
			"Changes go live only after netskope_dlp_profiles `deploy`." + dlpGateNote,
	},
	{
		Name:       "netskope_dlp_data_identifiers",
		Group:      "dlp",
		Title:      "DLP data identifiers",
		Collection: "/api/v2/services/dlp/entities/dataidentifiers",
		Actions:    crud,
		Description: "Custom data identifiers: the regex-based entities content rules match on. " +
			"Endpoint: /api/v2/services/dlp/entities/dataidentifiers.\n\n" +
			"Changes go live only after netskope_dlp_profiles `deploy`. A bad regex matches " +
			"everything or nothing; prove it on a profile no policy uses before deploying." + dlpGateNote,
	},
}
