package elements

import "github.com/user/inboxql/internal/store"

// Catalog is what is worth knowing, in the order a reader is asked.
//
// The instructions are written for a capable model reading the whole
// conversation, which is the point of moving off the small decision model:
// they say what counts and what does not, the cases that are easy to get
// wrong, and what to write when there is nothing.
var Catalog = []Element{
	{
		Name:  "receipts",
		About: "what was bought, from whom, for how much — each item with its brand",
		Unit:  UnitMessage, Shape: ShapeRecords,
		Instructions: "Record each purchase this message is the receipt, order confirmation, invoice or " +
			"payment record for: one record per order or receipt. List every item with its brand. " +
			"Receipts often abbreviate or omit the brand (\"885911470902 FLEXVOLT 20-\" is a DeWalt " +
			"FLEXVOLT battery; \"RYB\" is Ryobi): name the brand when you can tell it from the product " +
			"code, the product line or your own knowledge, and leave it out rather than guess. Use the " +
			"amounts as written. Not a purchase: shipping updates without prices, adverts, statements " +
			"that only show a balance, price-drop alerts — answer none for those.",
		Fields: []Field{
			{Name: "merchant", Type: TypeText, Required: true, Describe: "the shop or company sold by, e.g. Home Depot"},
			{Name: "date", Type: TypeDate, Describe: "when it was bought"},
			{Name: "order", Type: TypeText, Describe: "the order, receipt or invoice number, as written"},
			{Name: "items", Type: TypeList, Describe: "what was bought", Of: []Field{
				{Name: "name", Type: TypeText, Required: true, Describe: "the item, written out in full words"},
				{Name: "brand", Type: TypeText, Describe: "the maker, e.g. DeWalt"},
				{Name: "sku", Type: TypeText, Describe: "a product code or UPC, as written"},
				{Name: "quantity", Type: TypeNumber, Describe: "how many"},
				{Name: "price", Type: TypeNumber, Describe: "the line's price"},
			}},
			{Name: "total", Type: TypeNumber, Describe: "the amount paid, tax included"},
			{Name: "currency", Type: TypeText, Describe: "ISO code, e.g. USD"},
			{Name: "payment", Type: TypeText, Describe: "how it was paid, e.g. Visa ending 4821"},
		},
	},
	{
		Name:  "travel",
		About: "flights, trains, hotels, cars — booked, changed or cancelled",
		Unit:  UnitMessage, Shape: ShapeRecords,
		Instructions: "Record each travel reservation this message confirms, changes or cancels: one " +
			"record per flight leg, train, hotel stay, car rental, cruise or ride. Include the " +
			"confirmation code exactly as written. Use local times as written for start and end, with " +
			"the date. Not travel: marketing, loyalty-point statements, price alerts, and receipts for " +
			"things bought while travelling (those are receipts).",
		Fields: []Field{
			{Name: "kind", Type: TypeText, Required: true, Describe: "what was booked",
				Enum: []string{"flight", "train", "hotel", "car", "cruise", "ride", "other"}},
			{Name: "status", Type: TypeText, Describe: "what this message does to it",
				Enum: []string{"booked", "changed", "cancelled", "reminder"}},
			{Name: "provider", Type: TypeText, Required: true, Describe: "the airline, hotel, rail or rental company"},
			{Name: "confirmation", Type: TypeText, Describe: "the booking or confirmation code, as written"},
			{Name: "number", Type: TypeText, Describe: "a flight or train number, e.g. DL 1432"},
			{Name: "traveler", Type: TypeText, Describe: "who is travelling"},
			{Name: "from", Type: TypeText, Describe: "where it starts: city, airport or station"},
			{Name: "to", Type: TypeText, Describe: "where it ends; for a hotel, where it is"},
			{Name: "start", Type: TypeDateTime, Describe: "departure or check-in"},
			{Name: "end", Type: TypeDateTime, Describe: "arrival or check-out"},
			{Name: "total", Type: TypeNumber, Describe: "the price, if given"},
		},
	},
	{
		Name:  "events",
		About: "things to put in the calendar — proposed, for you to approve",
		Unit:  UnitMessage, Shape: ShapeRecords, Target: TargetCalendar,
		Instructions: "Record each specific event this message arranges or confirms that the user would " +
			"want in their calendar: a meeting, appointment, call, reservation, class, or deadline with " +
			"a time. It must have a date. Not events: vague plans (\"let's meet sometime\"), sales that " +
			"end on a date, webinars the user did not sign up for, and calendar invitations already " +
			"sent as invitations (those reach the calendar on their own). Travel goes in travel, not " +
			"here. Each event becomes a proposal the user approves.",
		Fields: []Field{
			{Name: "title", Type: TypeText, Required: true, Describe: "a short title, e.g. Dentist — cleaning"},
			{Name: "start", Type: TypeDateTime, Required: true, Describe: "when it starts, local time as written; a date alone for an all-day event"},
			{Name: "end", Type: TypeDateTime, Describe: "when it ends, if said"},
			{Name: "all_day", Type: TypeBool, Describe: "true for an all-day event"},
			{Name: "location", Type: TypeText, Describe: "where, or the call link"},
			{Name: "notes", Type: TypeText, Describe: "anything worth having in the event"},
		},
	},
	{
		Name:  "tasks",
		About: "what someone has to do because of this conversation — proposed as tickets",
		Unit:  UnitThread, Shape: ShapeRecords, Target: TargetTickets,
		Instructions: "Record each concrete thing someone has committed to or been asked to do in this " +
			"conversation that is still open as of its newest message. Say who owes it: me (the user), " +
			"them (someone else), or both. A request the user already answered or a promise already " +
			"kept is done — leave it out. Not tasks: newsletters' calls to action, \"let me know if you " +
			"have questions\", automated reminders to review or rate something. Each task becomes a " +
			"proposed ticket the user approves.",
		Fields: []Field{
			{Name: "what", Type: TypeText, Required: true, Describe: "the task, as an instruction, e.g. Send the signed contract to Alice"},
			{Name: "owner", Type: TypeText, Required: true, Describe: "who has to do it", Enum: []string{"me", "them", "both"}},
			{Name: "due", Type: TypeDate, Describe: "when it is due, if said or clearly implied"},
		},
	},
	{
		Name:  "waiting",
		About: "who, if anyone, is waiting for a reply",
		Unit:  UnitThread, Shape: ShapeLevels,
		Instructions: "As of the newest message, is someone waiting for a reply? Judge what was said, " +
			"not who wrote last: \"Thanks, sounds good!\" closes a conversation; \"I'll get back to you " +
			"Monday\" from the user means the user owes the next message even though they wrote last. " +
			"Automated mail waits on no one unless it asks the user to answer a person.",
		Levels: []store.Level{
			{Label: "on-me", Describe: "the user owes a reply or an action someone is waiting for"},
			{Label: "on-them", Describe: "the user is waiting for someone else to reply or act"},
			{Label: "no-one", Describe: "nobody is waiting: closed, informational, or automated"},
		},
	},
	{
		Name:  "importance",
		About: "how much a conversation needs the user",
		Unit:  UnitThread, Shape: ShapeLevels,
		Instructions: "How much does this conversation need the user's attention now?",
		Levels: []store.Level{
			{Label: "important", Describe: "a person needs something from the user, or money, health, legal or travel is at stake"},
			{Label: "normal", Describe: "worth reading, nothing pressing"},
			{Label: "low", Describe: "informational, can wait or be skimmed"},
			{Label: "ignorable", Describe: "promotional or automated noise"},
		},
	},
	{
		Name:  "sender",
		About: "whether a sender is a person, an organization, or a machine",
		Unit:  UnitSender, Shape: ShapeLevels, Target: TargetContacts,
		Instructions: "What kind of sender is this address, judged from how they write and what they " +
			"send? A shared mailbox people answer (support@, sales@) is an organization; a no-reply " +
			"address, a newsletter or a notification system is a system.",
		Levels: []store.Level{
			{Label: store.KindPerson, Describe: "an individual writing for themselves"},
			{Label: store.KindOrganization, Describe: "a company or team, written by people"},
			{Label: store.KindSystem, Describe: "automated: notifications, newsletters, receipts, no-reply"},
		},
	},
	{
		Name:  "topics",
		About: "what a conversation is about, in a few tags",
		Unit:  UnitThread, Shape: ShapeTags, Target: TargetTopics,
		Instructions: "Give one to five short lowercase tags for what this conversation is about — the " +
			"project, product, place, organization or subject a person would search for later. Prefer " +
			"a topic already in use (listed with each item) over a new near-synonym. Not tags: the " +
			"sender's name, \"email\", \"update\", \"newsletter\".",
	},
}
