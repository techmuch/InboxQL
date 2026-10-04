package store

// # A pack to start from
//
// Six of these were hardcoded in the rail component — Tickets, Proposed, Files,
// People, Systems, Unclassified — which is six saved queries somebody wrote in
// TSX instead of saving. They are here now, alongside a few more worth having,
// and they install like anything else.
//
// The same shape as the annotator starters, for the same reason: a candidate is
// worth adding only if it matches something in *this* mailbox, and the number
// is what makes that visible. A rail entry that finds nothing is a row that
// teaches you the feature does not work.

// RailDefault is one candidate rail entry.
type RailDefault struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Query string `json:"query"`
	Icon  string `json:"icon"`
	About string `json:"about"`
}

// RailDefaults are the entries offered.
//
// The first six are what the rail hardcoded. The rest are the queries this
// mailbox turned out to want — unread mail that is waiting on a reply, the
// things an annotator found, the week's arrivals.
var RailDefaults = []RailDefault{
	{
		Name: "tickets", Title: "Tickets", Icon: "ticket",
		Query: "in:tickets -status:done -status:rejected",
		About: "work that is still open",
	},
	{
		Name: "proposed", Title: "Proposed", Icon: "ticket",
		Query: "in:tickets status:proposed",
		About: "tickets an extractor suggested, waiting on a ruling",
	},
	{
		Name: "files", Title: "Files", Icon: "paperclip",
		Query: "in:attachments",
		About: "every distinct attachment, once each however many messages carried it",
	},
	{
		Name: "people", Title: "People", Icon: "users",
		Query: "in:contacts kind:person",
		About: "the humans you correspond with",
	},
	{
		Name: "systems", Title: "Systems", Icon: "bot",
		Query: "in:contacts kind:system",
		About: "the senders that are machines",
	},
	{
		Name: "unclassified", Title: "Unclassified", Icon: "search",
		Query: "in:contacts kind:unknown",
		About: "addresses nothing has worked out yet",
	},
	{
		Name: "awaiting-me", Title: "Waiting on me", Icon: "clock",
		Query: "in:contacts awaiting:me",
		About: "people whose last message you have not answered",
	},
	{
		Name: "this-week", Title: "This week", Icon: "clock",
		Query: "after:7d",
		About: "everything that arrived in the last seven days",
	},
	{
		Name: "unread", Title: "Unread", Icon: "inbox",
		Query: "is:unread -folder:spam -folder:trash",
		About: "unread mail, without the bins",
	},
	{
		Name: "big-files", Title: "Large files", Icon: "paperclip",
		Query: "in:attachments larger:1mb",
		About: "the attachments taking up the room",
	},
	{
		Name: "problems", Title: "Problems", Icon: "alert",
		Query: "in:logs level>warn",
		About: "what the application has had trouble with",
	},
}

// RailDefaultByName finds one.
func RailDefaultByName(name string) (RailDefault, bool) {
	for _, d := range RailDefaults {
		if d.Name == name {
			return d, true
		}
	}
	return RailDefault{}, false
}

// InstallRailDefaults saves the named entries, skipping any already there.
//
// Skipping rather than overwriting: a name that already exists may have been
// edited, and the edit is the user's. An empty list installs everything not
// already present, which is what somebody who has just found the pack means.
func InstallRailDefaults(only []string) (added, skipped []string, err error) {
	want := map[string]bool{}
	for _, n := range only {
		want[n] = true
	}

	for _, d := range RailDefaults {
		if len(want) > 0 && !want[d.Name] {
			continue
		}
		existing, err := GetSavedQuery(d.Name)
		if err != nil {
			return added, skipped, err
		}
		if existing != nil {
			skipped = append(skipped, d.Name)
			continue
		}
		if err := SaveQuery(&SavedQuery{
			Name: d.Name, Title: d.Title, Query: d.Query,
			Icon: d.Icon, Description: d.About,
		}); err != nil {
			return added, skipped, err
		}
		added = append(added, d.Name)
	}
	return added, skipped, nil
}

// RailDefaultReach is how many rows a candidate would match here.
//
// Zero is a real answer and the useful one: an entry that finds nothing is a
// row that teaches somebody the feature does not work.
func RailDefaultReach(d RailDefault) (int64, error) {
	return CountQuery(d.Query)
}
