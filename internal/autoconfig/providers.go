package autoconfig

import "strings"

var (
	gmail = Settings{
		Provider: "Gmail", IMAP: Server{"imap.gmail.com", 993, true}, SMTP: Server{"smtp.gmail.com", 465, true},
		Google: true, PasswordURL: "https://myaccount.google.com/apppasswords",
		Note: "Gmail does not accept your Google password here. Sign in with Google, or make an app password " +
			"(it needs 2-Step Verification on).",
	}
	icloud = Settings{
		Provider: "iCloud Mail", IMAP: Server{"imap.mail.me.com", 993, true}, SMTP: Server{"smtp.mail.me.com", 587, false},
		PasswordURL: "https://account.apple.com/account/manage",
		Note:        "iCloud needs an app-specific password, made under Sign-In and Security at account.apple.com.",
	}
	microsoft = Settings{
		Provider: "Outlook.com / Microsoft 365", IMAP: Server{"outlook.office365.com", 993, true},
		SMTP: Server{"smtp.office365.com", 587, false},
		Unsupported: "Microsoft has switched off password sign-in for IMAP and SMTP on Outlook.com and Microsoft 365, " +
			"and InboxQL cannot yet sign in with a Microsoft account. Until it can, mail from this account can be " +
			"brought in with Import (from Apple Mail or .eml files).",
	}
	yahoo = Settings{
		Provider: "Yahoo Mail", IMAP: Server{"imap.mail.yahoo.com", 993, true}, SMTP: Server{"smtp.mail.yahoo.com", 465, true},
		PasswordURL: "https://login.yahoo.com/account/security",
		Note:        "Yahoo needs an app password, made under Account Security.",
	}
	aol = Settings{
		Provider: "AOL Mail", IMAP: Server{"imap.aol.com", 993, true}, SMTP: Server{"smtp.aol.com", 465, true},
		PasswordURL: "https://login.aol.com/account/security",
		Note:        "AOL needs an app password, made under Account Security.",
	}
	fastmail = Settings{
		Provider: "Fastmail", IMAP: Server{"imap.fastmail.com", 993, true}, SMTP: Server{"smtp.fastmail.com", 465, true},
		PasswordURL: "https://app.fastmail.com/settings/security/apps",
		Note:        "Fastmail needs an app password, made under Settings → Privacy & Security.",
	}
	zoho = Settings{
		Provider: "Zoho Mail", IMAP: Server{"imap.zoho.com", 993, true}, SMTP: Server{"smtp.zoho.com", 465, true},
		Note: "IMAP access has to be switched on in Zoho Mail's settings first.",
	}
	gmx = Settings{
		Provider: "GMX", IMAP: Server{"imap.gmx.com", 993, true}, SMTP: Server{"mail.gmx.com", 587, false},
		Note: "IMAP access has to be switched on in GMX's settings first.",
	}
	proton = Settings{
		Provider: "Proton Mail",
		Unsupported: "Proton Mail is end-to-end encrypted and reachable over IMAP only through Proton Mail Bridge, " +
			"which runs on this computer. Start Bridge, then enter the address and password it shows under " +
			"\"Enter settings manually\".",
	}
)

var domains = map[string]*Settings{
	"gmail.com": &gmail, "googlemail.com": &gmail,
	"icloud.com": &icloud, "me.com": &icloud, "mac.com": &icloud,
	"outlook.com": &microsoft, "hotmail.com": &microsoft, "live.com": &microsoft, "msn.com": &microsoft,
	"hotmail.co.uk": &microsoft, "outlook.co.uk": &microsoft, "live.co.uk": &microsoft,
	"yahoo.com": &yahoo, "ymail.com": &yahoo, "rocketmail.com": &yahoo, "yahoo.co.uk": &yahoo,
	"aol.com":      &aol,
	"fastmail.com": &fastmail, "fastmail.fm": &fastmail,
	"zoho.com": &zoho, "zohomail.com": &zoho,
	"gmx.com": &gmx, "gmx.net": &gmx, "gmx.de": &gmx,
	"proton.me": &proton, "protonmail.com": &proton, "pm.me": &proton,
}

func byDomain(domain string) *Settings { return domains[domain] }

// byMX recognises a custom domain whose mail a large provider handles.
func byMX(hosts []string) *Settings {
	for _, h := range hosts {
		switch {
		case strings.HasSuffix(h, ".google.com"), strings.HasSuffix(h, ".googlemail.com"), h == "smtp.google.com":
			s := gmail
			s.Provider = "Google Workspace"
			s.PasswordURL = "https://myaccount.google.com/apppasswords"
			s.Note = "This domain's mail is on Google Workspace. Sign in with Google, or use an app password " +
				"if your administrator allows them."
			return &s
		case strings.HasSuffix(h, ".mail.protection.outlook.com"):
			return &microsoft
		case strings.HasSuffix(h, ".messagingengine.com"):
			return &fastmail
		case strings.HasSuffix(h, ".mail.icloud.com"):
			return &icloud
		case strings.HasSuffix(h, ".zoho.com"), strings.HasSuffix(h, ".zoho.eu"):
			return &zoho
		case strings.HasSuffix(h, ".protonmail.ch"):
			return &proton
		}
	}
	return nil
}
