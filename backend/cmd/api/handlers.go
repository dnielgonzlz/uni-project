package main

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
)

// healthzHandler returns 200 OK as a liveness check.
// AWS Elastic Beanstalk and UptimeRobot ping this endpoint.
func healthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// readyzHandler returns 200 only when the DB is reachable.
// Returns 503 if the database is down — signals Elastic Beanstalk to stop routing traffic.
func readyzHandler(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			// FRONTEND: surface DB outage in an ops/admin dashboard
			httpx.Error(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
