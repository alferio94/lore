package admin

// embed.go — Embeds the admin SPA dist directory and serves static files with
// an index.html fallback for Angular/SPA client-side routing.

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

//go:embed admin_dist
var adminDistFS embed.FS

// spaHandler returns an http.HandlerFunc that serves embedded static files
// from the admin_dist directory.  Any path that does not correspond to a real
// embedded file is answered with index.html so that the Angular router can
// handle it (SPA fallback).
//
// The handler strips the leading "/admin/" prefix before looking up files
// inside the embedded filesystem root "admin_dist/".
func spaHandler() http.HandlerFunc {
	// Build a sub-filesystem rooted at admin_dist so we can use the
	// standard http.FileServer helpers without the extra path prefix.
	sub, err := fs.Sub(adminDistFS, "admin_dist")
	if err != nil {
		// Should never happen at runtime — fail loudly during startup.
		panic("admin: failed to create sub-FS for admin_dist: " + err.Error())
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Strip the /admin/ prefix.
		p := strings.TrimPrefix(r.URL.Path, "/admin")
		p = strings.TrimPrefix(p, "/")
		if p == "" {
			p = "index.html"
		}

		// Try to open the file from the embedded FS.
		f, err := sub.Open(p)
		if err != nil {
			// File not found → SPA fallback: serve index.html.
			serveIndexHTML(w, sub)
			return
		}
		defer f.Close()

		// Stat to detect directories; serve index.html for those too.
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			serveIndexHTML(w, sub)
			return
		}

		// Detect and set Content-Type from extension.
		ext := filepath.Ext(p)
		ct := mime.TypeByExtension(ext)
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(http.StatusOK)
		io.Copy(w, f) //nolint:errcheck
	}
}

// serveIndexHTML reads and writes index.html from the given sub-FS.
func serveIndexHTML(w http.ResponseWriter, sub fs.FS) {
	idx, err := sub.Open("index.html")
	if err != nil {
		http.Error(w, "admin: index.html not found", http.StatusInternalServerError)
		return
	}
	defer idx.Close()

	ct := mime.TypeByExtension(path.Ext("index.html"))
	if ct == "" {
		ct = "text/html; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	io.Copy(w, idx) //nolint:errcheck
}
