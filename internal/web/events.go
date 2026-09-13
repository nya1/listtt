package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const keepAliveEvery = 15 * time.Second

// serveEvents streams every snapshot as a server-sent event until the client leaves.
func serveEvents(w http.ResponseWriter, r *http.Request, b Backend) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()
	ticker := time.NewTicker(keepAliveEvery)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case snap, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(snap)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
		case <-ticker.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
