package health

import (
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/gliner"
	"github.com/user/inboxql/internal/store"
)

// checkGLiNER reports on the span-extraction model.
//
// Silent until something asks for it. A model nobody uses is not a fault, and
// a mailbox with no span extractors in it should not grow a permanent amber
// row telling its owner to download 800 MB they never asked for.
//
// Once an annotator declares the engine it becomes a failure rather than a
// warning, because at that point there is an annotator that cannot run at all
// — which is a different situation from work merely being outstanding.
func checkGLiNER(rep *Report, dataDir string) {
	annotators, err := store.ListAnnotators()
	if err != nil {
		return
	}
	var wanting []string
	for _, a := range annotators {
		if a.Engine == store.EngineGLiNER {
			wanting = append(wanting, a.Name)
		}
	}
	if len(wanting) == 0 {
		return
	}

	if gliner.Installed(dataDir) {
		rep.add("span extraction model", StatusOK,
			fmt.Sprintf("installed; %s can run", countOf(len(wanting), "annotator", "annotators")))
		return
	}
	// "1 annotator needs it" / "2 annotators need it": the verb has to agree
	// with the count as well as the noun.
	needs := "need"
	if len(wanting) == 1 {
		needs = "needs"
	}
	rep.addJob("span extraction model", StatusFail,
		fmt.Sprintf("%s %s it and it is not installed: %s",
			countOf(len(wanting), "annotator", "annotators"), needs,
			strings.Join(wanting, ", ")),
		"iql gliner install", JobGLiNER)
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
