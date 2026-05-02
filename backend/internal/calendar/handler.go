package calendar

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/auth"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/config"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/scheduling"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// Handler serves the calendar feed and calendar-token management endpoints.
type Handler struct {
	users    *users.Repository
	sessions *scheduling.Repository
	cfg      *config.Config
}

func NewHandler(u *users.Repository, s *scheduling.Repository, cfg *config.Config) *Handler {
	return &Handler{users: u, sessions: s, cfg: cfg}
}

// GetCalendarURL handles GET /api/v1/me/calendar-url
// Returns the full subscription URL for the authenticated user.
//
//	@Summary      Get calendar subscription URL
//	@Description  Returns the user's personal ICS feed URL. Paste it into Google Calendar, Apple Calendar, or Outlook to subscribe. Note: calendar apps poll for changes every 12–24 hours, so new or cancelled sessions may not appear immediately.
//	@Tags         calendar
//	@Produce      json
//	@Success      200  {object}  calendarURLResponse
//	@Failure      401  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /me/calendar-url [get]
func (h *Handler) GetCalendarURL(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	user, err := h.users.GetUserByID(r.Context(), userID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	httpx.JSON(w, http.StatusOK, calendarURLResponse{
		URL:     h.buildURL(user.CalendarToken),
		Warning: "Calendar apps (Google, Apple, Outlook) check for updates every 12–24 hours. New sessions and cancellations may take up to 24 hours to appear in your calendar.",
	})
}

// RegenerateCalendarURL handles POST /api/v1/me/calendar-url/regenerate
// Issues a new token, invalidating any previous subscription URL.
//
//	@Summary      Regenerate calendar subscription URL
//	@Description  Generates a new calendar token, making the old subscription URL invalid. Use this if you think your URL has been shared without your permission.
//	@Tags         calendar
//	@Produce      json
//	@Success      200  {object}  calendarURLResponse
//	@Failure      401  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /me/calendar-url/regenerate [post]
func (h *Handler) RegenerateCalendarURL(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	user, err := h.users.RegenerateCalendarToken(r.Context(), userID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to regenerate token")
		return
	}

	httpx.JSON(w, http.StatusOK, calendarURLResponse{
		URL:     h.buildURL(user.CalendarToken),
		Warning: "Calendar apps (Google, Apple, Outlook) check for updates every 12–24 hours. New sessions and cancellations may take up to 24 hours to appear in your calendar.",
	})
}

// ServeICS handles GET /calendar/{token}.ics
// Public endpoint — authenticated by the calendar token in the URL path.
// No JWT required so calendar apps can subscribe without user interaction.
//
//	@Summary      ICS calendar feed
//	@Description  Serves a personal iCalendar (RFC 5545) feed. Subscribe to this URL in Google Calendar, Apple Calendar, or Outlook. The URL contains a secret token — do not share it.
//	@Tags         calendar
//	@Produce      text/calendar
//	@Param        token  path  string  true  "Calendar token (UUID)"
//	@Success      200    {string}  string  "iCalendar feed"
//	@Failure      404    {object}  httpx.ErrorResponse
//	@Router       /calendar/{token}.ics [get]
func (h *Handler) ServeICS(w http.ResponseWriter, r *http.Request) {
	rawToken := chi.URLParam(r, "token")
	token, err := uuid.Parse(rawToken)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}

	user, err := h.users.GetUserByCalendarToken(r.Context(), token)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}

	sessions, counterparts, err := h.loadSessionsAndCounterparts(r.Context(), user)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load sessions")
		return
	}

	feed := GenerateFeed(user, sessions, counterparts)

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.ics"`, user.ID.String()))
	// Instruct proxies not to cache the feed — always serve fresh.
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(feed))
}

// --- private helpers ---

type calendarURLResponse struct {
	URL     string `json:"url"`
	Warning string `json:"warning"`
}

func (h *Handler) buildURL(token uuid.UUID) string {
	base := fmt.Sprintf("http://localhost:%s", h.cfg.Port)
	if h.cfg.Env == "production" {
		// FRONTEND: replace with the real production base URL.
		base = fmt.Sprintf("https://api.pt-scheduler.io")
	}
	return fmt.Sprintf("%s/calendar/%s.ics", base, token.String())
}

// loadSessionsAndCounterparts fetches sessions and a name lookup map.
// Coaches get all their sessions; clients get their own.
// The counterparts map is keyed by the counterpart's profile ID (coach.ID or client.ID as string).
func (h *Handler) loadSessionsAndCounterparts(ctx context.Context, user *users.User) ([]scheduling.Session, map[string]string, error) {
	counterparts := make(map[string]string)

	switch user.Role {
	case users.RoleCoach:
		coach, err := h.users.GetCoachByUserID(ctx, user.ID)
		if err != nil {
			return nil, nil, err
		}
		sessions, err := h.sessions.ListSessionsByCoach(ctx, coach.ID, "")
		if err != nil {
			return nil, nil, err
		}
		// Build client name map: client.ID → client user full name.
		for _, s := range sessions {
			if _, seen := counterparts[s.ClientID.String()]; seen {
				continue
			}
			client, err := h.users.GetClientByID(ctx, s.ClientID)
			if err != nil {
				continue
			}
			clientUser, err := h.users.GetUserByID(ctx, client.UserID)
			if err != nil {
				continue
			}
			counterparts[s.ClientID.String()] = clientUser.FullName
		}
		return sessions, counterparts, nil

	case users.RoleClient:
		client, err := h.users.GetClientByUserID(ctx, user.ID)
		if err != nil {
			return nil, nil, err
		}
		sessions, err := h.sessions.ListSessionsByClient(ctx, client.ID, "")
		if err != nil {
			return nil, nil, err
		}
		// Build coach name map: coach.ID → coach user full name.
		for _, s := range sessions {
			if _, seen := counterparts[s.CoachID.String()]; seen {
				continue
			}
			coach, err := h.users.GetCoachByID(ctx, s.CoachID)
			if err != nil {
				continue
			}
			coachUser, err := h.users.GetUserByID(ctx, coach.UserID)
			if err != nil {
				continue
			}
			counterparts[s.CoachID.String()] = coachUser.FullName
		}
		return sessions, counterparts, nil

	default:
		return nil, nil, fmt.Errorf("calendar: unsupported role %q", user.Role)
	}
}
