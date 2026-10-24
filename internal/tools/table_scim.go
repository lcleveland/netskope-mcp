package tools

// SCIM identity. Netskope's SCIM surface is the provisioning view of users and
// groups -- what an IdP pushes in -- which is not the same population as the
// users that appear in event data.

var scimResources = []Resource{
	{
		Name:       "netskope_scim_users",
		Group:      "scim",
		Title:      "SCIM users",
		Collection: "/api/v2/scim/Users",
		Actions:    crud,
		LimitParam: "count",
		Description: "Users provisioned into the tenant over SCIM. Endpoint: /api/v2/scim/Users.\n\n" +
			"This is SCIM, not Netskope's own API shape: filtering uses the SCIM syntax " +
			"(`query.filter` of `userName eq \"a@b.com\"`), paging uses `startIndex` and `count` " +
			"rather than offset and limit, and bodies follow the SCIM user schema " +
			"(`userName`, `name.givenName`, `name.familyName`, `active`, `emails`).\n\n" +
			"If an IdP is provisioning this tenant, it is the source of truth and will overwrite " +
			"changes made here on its next sync.",
	},
	{
		Name:       "netskope_scim_groups",
		Group:      "scim",
		Title:      "SCIM groups",
		Collection: "/api/v2/scim/Groups",
		Actions:    crud,
		LimitParam: "count",
		GetQuery:   map[string]string{"attributes": "members"},
		Description: "Groups provisioned over SCIM, which NPA and inline policy rules target. " +
			"Endpoint: /api/v2/scim/Groups.\n\n" +
			"Bodies follow the SCIM group schema (`displayName`, `members`). Netskope returns " +
			"`members` only when asked: get sends `attributes=members` by default, and list omits " +
			"members unless `query.attributes` is `members`. Membership changes " +
			"are normally expressed as a PATCH with add/remove operations rather than a whole-object " +
			"update; a full update replaces the member list outright, which will silently remove " +
			"everyone not named in it.",
	},
}
