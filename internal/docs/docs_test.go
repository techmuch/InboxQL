package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every guide in the table of contents exists, has a title and renders.
func TestEveryGuideRenders(t *testing.T) {
	for _, g := range Guides {
		p, err := Render(g.Slug, AppLinks)
		if err != nil {
			t.Errorf("%s: %v", g.Slug, err)
			continue
		}
		if p.Title == "" || p.Title == g.Slug {
			t.Errorf("%s has no title", g.Slug)
		}
		if strings.Contains(p.HTML, "<h1") {
			t.Errorf("%s renders its title twice", g.Slug)
		}
		if g.Summary == "" {
			t.Errorf("%s has no summary", g.Slug)
		}
	}
}

// A link from one guide to another is rewritten for whichever reader shows
// it, and every one of them names a guide that exists — a broken link in the
// docs is a broken page in Help and on the site.
func TestGuideLinksResolve(t *testing.T) {
	for _, g := range Guides {
		src, _ := source(g)
		for _, m := range mdLink.FindAllStringSubmatch(string(src), -1) {
			dest := m[1]
			if strings.Contains(dest, "://") || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "mailto:") ||
				shot.MatchString(dest) { // screenshots: TestScreenshots
				continue
			}
			lm := guideLink.FindStringSubmatch(dest)
			if lm == nil {
				t.Errorf("%s links to %q, which is not a guide", g.Slug, dest)
				continue
			}
			target := slugForFile(lm[1])
			if target == "" {
				t.Errorf("%s links to %q, which is not a guide", g.Slug, dest)
				continue
			}
			if lm[2] == "" {
				continue
			}
			p, err := Render(target, AppLinks)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, h := range p.Headings {
				found = found || h.ID == lm[2]
			}
			if !found {
				t.Errorf("%s links to %s#%s, and %s has no such heading", g.Slug, target, lm[2], target)
			}
		}
	}
	p, _ := Render("getting-started", SiteLinks)
	if !strings.Contains(p.HTML, `href="privacy.html"`) {
		t.Error("site links are not rewritten to .html")
	}
	p, _ = Render("getting-started", AppLinks)
	if !strings.Contains(p.HTML, `href="#doc/privacy"`) {
		t.Error("app links are not rewritten to #doc/")
	}
}

func TestIndexCutsIntoSections(t *testing.T) {
	idx := Index()
	var hit *Section
	for i := range idx {
		if idx[i].Slug == "settings" && strings.Contains(idx[i].Heading, "Machine settings") {
			hit = &idx[i]
		}
	}
	if hit == nil {
		t.Fatal("no Machine settings section in the settings guide")
	}
	if hit.ID == "" || !strings.Contains(hit.Text, "Heavy work on battery") || hit.Title == "" {
		t.Errorf("section = %+v", hit)
	}
	for _, s := range idx {
		if strings.Contains(s.Text, "<?claude") || strings.Contains(s.Text, "](") {
			t.Errorf("%s/%s carries markup into the index", s.Slug, s.ID)
		}
	}
}

// A link into the application opens it in Help; on the site, which cannot,
// it is bold text, so "Settings → General → Color theme" still reads.
func TestAppLinks(t *testing.T) {
	p, _ := Render("settings", AppLinks)
	if !strings.Contains(p.HTML, `href="#setting/theme" class="iql-app-link"`) {
		t.Error("an app link is not marked for the reader")
	}
	p, _ = Render("settings", SiteLinks)
	if strings.Contains(p.HTML, "#setting/") || strings.Contains(p.HTML, "#open/") {
		t.Error("the site links into an application it does not have")
	}
	if !strings.Contains(p.HTML, "<strong>colour theme</strong>") {
		t.Error("an app link's text is lost on the site")
	}
}

// helpTopic is one line of frontend/src/lib/helpTopics.ts.
var helpTopic = regexp.MustCompile(`\{ id: '([^']+)'.*?doc: '([^']+)'(?:, anchor: '([^']+)')?`)

// Every "?" in the application opens a guide section that exists. The list
// is in the frontend; the headings are only known to this renderer, so the
// check is here.
func TestHelpTopicsResolve(t *testing.T) {
	src, err := os.ReadFile("../../frontend/src/lib/helpTopics.ts")
	if err != nil {
		t.Fatal(err)
	}
	topics := helpTopic.FindAllStringSubmatch(string(src), -1)
	if len(topics) < 10 {
		t.Fatalf("read %d topics; has the format of helpTopics.ts changed?", len(topics))
	}
	for _, m := range topics {
		p, err := Render(m[2], AppLinks)
		if err != nil {
			t.Errorf("topic %s names guide %q, which does not exist", m[1], m[2])
			continue
		}
		if m[3] == "" {
			continue
		}
		found := false
		for _, h := range p.Headings {
			found = found || h.ID == m[3]
		}
		if !found {
			t.Errorf("topic %s names %s#%s, and %s has no such heading", m[1], m[2], m[3], m[2])
		}
	}
}

// Every screenshot a guide shows exists in every theme, and every file in
// docs/images is shown somewhere — the binary carries them all, so one that
// no guide uses is weight for nothing.
func TestScreenshots(t *testing.T) {
	have := map[string]bool{}
	for _, name := range Images() {
		have[name] = true
	}
	used := map[string]bool{}
	img := regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)
	for _, g := range Guides {
		src, _ := source(g)
		for _, m := range img.FindAllStringSubmatch(string(src), -1) {
			s := shot.FindStringSubmatch(m[1])
			if s == nil {
				t.Errorf("%s shows %q, which is not a screenshot in images/", g.Slug, m[1])
				continue
			}
			for _, theme := range ShotThemes {
				name := s[1] + "." + theme + ".webp"
				used[name] = true
				if !have[name] {
					t.Errorf("%s shows %s, and there is no %s — run make screenshots", g.Slug, m[1], name)
				}
			}
		}
	}
	for name := range have {
		if !used[name] {
			t.Errorf("docs/images/%s is in no guide", name)
		}
	}

	p, _ := Render("desk", AppLinks)
	for _, want := range []string{
		`src="/api/docs/images/desk.light.webp" alt="The Desk`,
		`class="iql-shot iql-shot-light"`,
		`src="/api/docs/images/desk.dark.webp" alt="The Desk`,
	} {
		if !strings.Contains(p.HTML, want) {
			t.Errorf("the app's page lacks %s", want)
		}
	}
	p, _ = Render("desk", SiteLinks)
	if !strings.Contains(p.HTML, `src="images/desk.dark.webp"`) {
		t.Error("the site's page does not show the screenshot from beside it")
	}
	if _, err := Image("../desk.md"); err == nil {
		t.Error("Image reads outside docs/images")
	}
}
