package store

import (
	"fmt"
	"sync"

	"github.com/user/inboxql/internal/query"
)

// Facets is how what you are looking at breaks down: for each row of the
// rail, how many messages clicking it would leave you with.
//
// # Why every row previews the click
//
// A label in the rail adds to the frame, and a folder swaps the frame and
// keeps what was added. So the honest number beside a label is the frame with
// that label, and beside a folder it is the additions inside that folder. A
// global total beside a row that adds to a frame answers a question nobody
// clicking it asked: "banking 312" on Starred, where the click shows twelve.
type Facets struct {
	// Total and Unread are the frame itself.
	Total  int `json:"total"`
	Unread int `json:"unread"`
	// Labels is every mail label, zeros included, so a rail that dims a
	// zero does not have to guess which names exist.
	Labels []FacetCount `json:"labels"`
	// Annotators is each annotator's rail term counted within the frame.
	Annotators []FacetCount `json:"annotators"`
	// Folders is the additions inside each folder. Absent when nothing was
	// added: then the folder's own counts already answer, and are cheaper.
	Folders []FacetCount `json:"folders,omitempty"`
}

// FacetCount is one row's count.
type FacetCount struct {
	Name   string `json:"name"`
	Term   string `json:"term,omitempty"`
	Total  int    `json:"total"`
	Unread int    `json:"unread"`
}

const unreadSum = `COALESCE(SUM(CASE WHEN m.flags NOT LIKE '%\Seen%' THEN 1 ELSE 0 END), 0)`

// MessageFacets counts the rail inside a query about mail.
//
// src is the whole query — folder and additions — and rest is the additions
// alone, which is what a folder row keeps when it swaps the folder. rest
// empty means nothing was added, and the folders are not counted here.
//
// The labels are one GROUP BY over the frame's matches. The annotators and
// the folders are each their own count, run together: each is an indexed
// seek, which FolderCounts measured as faster than one grouping scan.
func MessageFacets(src, rest string) (*Facets, error) {
	frame, err := mailFilter(src)
	if err != nil {
		return nil, err
	}
	where, args, err := query.CompileFilter(frame, queryOptions(0, 0))
	if err != nil {
		return nil, err
	}

	out := &Facets{Labels: []FacetCount{}, Annotators: []FacetCount{}}
	if err := db.QueryRow("SELECT COUNT(*), "+unreadSum+" FROM messages m WHERE "+where, args...).
		Scan(&out.Total, &out.Unread); err != nil {
		return nil, err
	}

	byLabel := map[string]FacetCount{}
	rows, err := db.Query(`SELECT ml.label, COUNT(*), `+unreadSum+`
		FROM messages m JOIN message_labels ml ON ml.message_id = m.id
		WHERE `+where+` GROUP BY ml.label`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c FacetCount
		if err := rows.Scan(&c.Name, &c.Total, &c.Unread); err != nil {
			rows.Close()
			return nil, err
		}
		byLabel[c.Name] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	names, err := LabelCounts("")
	if err != nil {
		return nil, err
	}
	for _, l := range names {
		c := byLabel[l.Name]
		c.Name = l.Name
		out.Labels = append(out.Labels, c)
	}

	// The rest are counted together, each into its own slot.
	type job struct {
		into *FacetCount
		node query.Node
	}
	var jobs []job

	anns, err := ListAnnotators()
	if err != nil {
		return nil, err
	}
	out.Annotators = make([]FacetCount, len(anns))
	for i, a := range anns {
		term := "label:" + a.Name
		if a.Kind == KindExtract {
			term = "extract:" + a.Name
		}
		out.Annotators[i] = FacetCount{Name: a.Name, Term: term}
		t, err := query.Parse(term)
		if err != nil {
			continue
		}
		jobs = append(jobs, job{&out.Annotators[i], &query.And{Nodes: []query.Node{frame, t.Filter}}})
	}

	if rest != "" {
		added, err := mailFilter(rest)
		if err != nil {
			return nil, err
		}
		for _, f := range Folders {
			if IsDraftFolder(f) {
				// A draft is not a message; the additions may not even mean
				// anything to one. Drafts keep their own count.
				continue
			}
			out.Folders = append(out.Folders, FacetCount{Name: f, Term: "folder:" + f})
		}
		for i := range out.Folders {
			t, err := query.Parse(out.Folders[i].Term)
			if err != nil {
				return nil, err
			}
			jobs = append(jobs, job{&out.Folders[i], &query.And{Nodes: []query.Node{t.Filter, added}}})
		}
	}

	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			w, a, err := query.CompileFilter(j.node, queryOptions(0, 0))
			if err != nil {
				errs[i] = err
				return
			}
			errs[i] = db.QueryRow("SELECT COUNT(*), "+unreadSum+" FROM messages m WHERE "+w, a...).
				Scan(&j.into.Total, &j.into.Unread)
		}(i, j)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// mailFilter is a query's filter, refusing one about anything but mail.
// Display stages are fine — they change how mail is shown, not which.
func mailFilter(src string) (query.Node, error) {
	q, err := query.Parse(src)
	if err != nil {
		return nil, err
	}
	if q.Entity() != query.EntityMessage {
		return nil, fmt.Errorf("counts by label are about mail, and this query is about %s", q.Entity())
	}
	for _, st := range q.Stages {
		if !displayStages[st.Kind] {
			return nil, fmt.Errorf("| %s turns mail into something else", st.Kind)
		}
	}
	return q.Filter, nil
}
