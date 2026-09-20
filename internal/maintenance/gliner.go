package maintenance

import (
	"context"
	"fmt"

	"github.com/user/inboxql/internal/gliner"
)

// runGLiNERInstall downloads and prepares the span-extraction model.
//
// A job rather than a handler for the same reason OCR is one: it moves most of
// a gigabyte and then rewrites it, which no browser will wait for. It differs
// from the other jobs in one way worth naming — this is the only maintenance
// job that talks to the internet, and the only one whose consent question is
// about what comes in rather than what goes out. No mail is sent anywhere,
// here or later, which is why it does not take AllowRemote.
func runGLiNERInstall(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	if gliner.Installed(dataDir) && !opts.Redo {
		return "the model is already installed", nil
	}

	repo := opts.Profile // reused as the repository name; the UI leaves it empty
	if repo == "" {
		repo = gliner.DefaultRepo
	}

	err := gliner.Install(ctx, dataDir, repo, func(file string, done, total int64) {
		// Bytes rather than files: two files of wildly different sizes make a
		// file count a progress bar that sits at 0% and then jumps to done.
		if total > 0 {
			run.progress(int(done>>20), int(total>>20), file+" (MB)")
		}
	})
	if err != nil {
		return "", err
	}

	digest, _ := gliner.Digest(dataDir, "model.onnx")
	if digest == "" {
		return fmt.Sprintf("installed %s", repo), nil
	}
	return fmt.Sprintf("installed %s (%s)", repo, digest[:16]), nil
}
