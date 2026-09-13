// Package web serves the dashboard UI, the JSON API, and the event stream.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"

	"listtt/internal/dashboard"
)

//go:embed static
var staticFiles embed.FS

const maxBodyBytes = 64 << 10

// Backend is implemented by *dashboard.Dashboard.
type Backend interface {
	Subscribe() (<-chan dashboard.Snapshot, func())
	CreateGroup(name string) (dashboard.GroupView, error)
	RenameGroup(id, name string) (dashboard.GroupView, error)
	DeleteGroup(id string) error
	UpdateSession(id string, groupID, note *string) (dashboard.SessionView, error)
	DeleteSession(id string) error
	Focus(id string) error
}

// NewHandler builds the router behind the Host/Origin guard. port is the port
// the server listens on.
func NewHandler(b Backend, port string) http.Handler {
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) { serveEvents(w, r, b) })

	mux.HandleFunc("POST /api/groups", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		g, err := b.CreateGroup(body.Name)
		respond(w, http.StatusCreated, g, err)
	})
	mux.HandleFunc("PATCH /api/groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		g, err := b.RenameGroup(r.PathValue("id"), body.Name)
		respond(w, http.StatusOK, g, err)
	})
	mux.HandleFunc("DELETE /api/groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusNoContent, nil, b.DeleteGroup(r.PathValue("id")))
	})
	mux.HandleFunc("PATCH /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			GroupID *string `json:"groupId"`
			Note    *string `json:"note"`
		}
		if !decode(w, r, &body) {
			return
		}
		v, err := b.UpdateSession(r.PathValue("id"), body.GroupID, body.Note)
		respond(w, http.StatusOK, v, err)
	})
	mux.HandleFunc("DELETE /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusNoContent, nil, b.DeleteSession(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/sessions/{id}/focus", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusNoContent, nil, b.Focus(r.PathValue("id")))
	})
	return guard(port, mux)
}

// guard blocks DNS rebinding (Host) and cross-site writes (Origin, Content-Type).
func guard(port string, next http.Handler) http.Handler {
	allowedHost := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost[r.Host] {
			writeError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("Origin") != "http://"+r.Host {
				writeError(w, http.StatusForbidden, "forbidden origin")
				return
			}
			if r.ContentLength != 0 {
				mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || mediaType != "application/json" {
					writeError(w, http.StatusForbidden, "Content-Type must be application/json")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// respond writes err as a mapped error, or v with status (no body for 204).
func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		code := http.StatusInternalServerError
		var de *dashboard.Error
		if errors.As(err, &de) {
			switch de.Kind {
			case dashboard.KindValidation:
				code = http.StatusBadRequest
			case dashboard.KindNotFound:
				code = http.StatusNotFound
			case dashboard.KindConflict:
				code = http.StatusConflict
			}
		}
		writeError(w, code, err.Error())
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
