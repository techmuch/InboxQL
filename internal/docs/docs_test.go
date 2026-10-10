package docs

import (
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
			if strings.Contains(dest, "://") || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "mailto:") {
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
