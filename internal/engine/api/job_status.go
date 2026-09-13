package api

import (
	"net/http"
)

func newJobStatusHandler(store SubmissionStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jobID, route, ok := jobRouteFromPath(r.URL.Path)
		if !ok || route != jobRouteStatus {
			http.NotFound(w, r)
			return
		}

		handleJobStatus(w, r, store, jobID)
	})
}

func jobIDFromPath(path string) (string, bool) {
	jobID, route, ok := jobRouteFromPath(path)
	if !ok || route != jobRouteStatus {
		return "", false
	}

	return jobID, true
}
