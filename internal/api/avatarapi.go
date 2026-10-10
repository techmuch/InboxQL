package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/user/inboxql/internal/avatar"
)

// Pictures of people: fetched from Gravatar only when someone presses the
// button for that one address, then served from the mailbox. See package
// avatar for why it is that way round.
func registerAvatarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/avatars", handleAvatarMeta)
	mux.HandleFunc("GET /api/avatars/image", handleAvatarImage)
	mux.HandleFunc("GET /api/avatars/stored", func(w http.ResponseWriter, r *http.Request) {
		stored, err := avatars().Stored()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "%v", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"stored": stored})
	})
	mux.HandleFunc("POST /api/avatars/gravatar", handleAvatarFetch)
	mux.HandleFunc("DELETE /api/avatars", handleAvatarRemove)
}

func avatars() *avatar.Store { return avatar.New(importDataDir) }

func avatarError(w http.ResponseWriter, err error) {
	if errors.Is(err, avatar.ErrAddress) {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeError(w, http.StatusBadGateway, "%v", err)
}

func handleAvatarMeta(w http.ResponseWriter, r *http.Request) {
	m, err := avatars().Meta(r.URL.Query().Get("address"))
	if err != nil {
		avatarError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// The content types a stored picture may be served as. Fixed, so that what
// Gravatar said is never trusted further than "one of these".
var avatarTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func handleAvatarImage(w http.ResponseWriter, r *http.Request) {
	b, m, err := avatars().Image(r.URL.Query().Get("address"))
	if errors.Is(err, avatar.ErrAddress) {
		avatarError(w, err)
		return
	}
	if err != nil {
		// No picture is the common case, not a fault: the page draws
		// initials when this is a 404.
		http.NotFound(w, r)
		return
	}
	ct := strings.TrimSpace(strings.Split(m.ContentType, ";")[0])
	if !avatarTypes[ct] {
		ct = "application/octet-stream"
	}
	etag := fmt.Sprintf(`"%d"`, m.FetchedAt.UnixNano())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Write(b)
}

func handleAvatarFetch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address string `json:"address"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	m, err := avatars().FetchGravatar(r.Context(), body.Address)
	if err != nil {
		avatarError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func handleAvatarRemove(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("address")
	if err := avatars().Remove(address); err != nil && !errors.Is(err, os.ErrNotExist) {
		avatarError(w, err)
		return
	}
	m, _ := avatars().Meta(address)
	writeJSON(w, http.StatusOK, m)
}
