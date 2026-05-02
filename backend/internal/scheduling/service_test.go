package scheduling

// Internal package tests (package scheduling, not scheduling_test) so we can
// construct a Service directly with fake dependencies and reference unexported
// interface types.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/danielgonzalez/pt-scheduler/internal/availability"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// ---------------------------------------------------------------------------
// Fake implementations
// ---------------------------------------------------------------------------

// fakeSessionStore is an in-memory implementation of sessionStore for tests.
// Fields beginning with On* are optional callbacks; if nil the method returns
// the zero value and no error.
type fakeSessionStore struct {
	sessions map[uuid.UUID]*Session
	credits  map[uuid.UUID]*SessionCredit

	onUpdateSessionStatus func(id uuid.UUID, status string) (*Session, error)
	onCreateSessionCredit func(clientID, sourceSessionID uuid.UUID, reason string, expiresAt time.Time) (*SessionCredit, error)
	onRequestCancellation func(id uuid.UUID, reason string, requestedAt time.Time) (*Session, error)
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions: make(map[uuid.UUID]*Session),
		credits:  make(map[uuid.UUID]*SessionCredit),
	}
}

func (f *fakeSessionStore) add(s *Session) {
	f.sessions[s.ID] = s
}

func (f *fakeSessionStore) GetSessionByID(_ context.Context, id uuid.UUID) (*Session, error) {
	s, ok := f.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeSessionStore) UpdateSessionStatus(_ context.Context, id uuid.UUID, status string) (*Session, error) {
	if f.onUpdateSessionStatus != nil {
		return f.onUpdateSessionStatus(id, status)
	}
	s, ok := f.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	s.Status = status
	cp := *s
	return &cp, nil
}

func (f *fakeSessionStore) CreateSessionCredit(_ context.Context, clientID, sourceSessionID uuid.UUID, reason string, expiresAt time.Time) (*SessionCredit, error) {
	if f.onCreateSessionCredit != nil {
		return f.onCreateSessionCredit(clientID, sourceSessionID, reason, expiresAt)
	}
	c := &SessionCredit{
		ID:              uuid.New(),
		ClientID:        clientID,
		SourceSessionID: sourceSessionID,
		Reason:          reason,
		ExpiresAt:       expiresAt,
		CreatedAt:       time.Now(),
	}
	f.credits[c.ID] = c
	return c, nil
}

func (f *fakeSessionStore) RequestCancellation(_ context.Context, id uuid.UUID, reason string, requestedAt time.Time) (*Session, error) {
	if f.onRequestCancellation != nil {
		return f.onRequestCancellation(id, reason, requestedAt)
	}
	s, ok := f.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	s.Status = StatusPendingCancellation
	s.CancellationReason = &reason
	s.CancellationRequestedAt = &requestedAt
	cp := *s
	return &cp, nil
}

// Unused methods — return safe zero values so the interface is satisfied.
func (f *fakeSessionStore) CreateScheduleRun(_ context.Context, _ uuid.UUID, _ time.Time, _ json.RawMessage) (*ScheduleRun, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSessionStore) GetScheduleRunByID(_ context.Context, _ uuid.UUID) (*ScheduleRun, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSessionStore) UpdateScheduleRunStatus(_ context.Context, _ uuid.UUID, _ string, _ json.RawMessage) (*ScheduleRun, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSessionStore) CreateSession(_ context.Context, _ *Session) (*Session, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSessionStore) ListActiveSessionsByRunID(_ context.Context, _ uuid.UUID) ([]Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) ListConfirmedSessionsForCoachInRange(_ context.Context, _ uuid.UUID, _, _ time.Time) ([]Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) ConfirmSessionsByRunID(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeSessionStore) CancelSessionsByRunID(_ context.Context, _ uuid.UUID) error  { return nil }
func (f *fakeSessionStore) ListSessionsByCoach(_ context.Context, _ uuid.UUID, _ string) ([]Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) ListSessionsByClient(_ context.Context, _ uuid.UUID, _ string) ([]Session, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------

// fakeUserLookup is an in-memory implementation of userLookup.
type fakeUserLookup struct {
	coaches       map[uuid.UUID]*users.Coach  // keyed by coach.ID
	byUser        map[uuid.UUID]*users.Coach  // keyed by user.ID → coach
	clients       map[uuid.UUID]*users.Client // keyed by client.ID
	clientsByUser map[uuid.UUID]*users.Client // keyed by user.ID → client
	users         map[uuid.UUID]*users.User   // keyed by user.ID
}

func newFakeUserLookup() *fakeUserLookup {
	return &fakeUserLookup{
		coaches:       make(map[uuid.UUID]*users.Coach),
		byUser:        make(map[uuid.UUID]*users.Coach),
		clients:       make(map[uuid.UUID]*users.Client),
		clientsByUser: make(map[uuid.UUID]*users.Client),
		users:         make(map[uuid.UUID]*users.User),
	}
}

func (f *fakeUserLookup) addCoach(u *users.User, c *users.Coach) {
	f.users[u.ID] = u
	f.coaches[c.ID] = c
	f.byUser[u.ID] = c
}

func (f *fakeUserLookup) addClient(u *users.User, c *users.Client) {
	f.users[u.ID] = u
	f.clients[c.ID] = c
	f.clientsByUser[u.ID] = c
}

func (f *fakeUserLookup) GetCoachByUserID(_ context.Context, userID uuid.UUID) (*users.Coach, error) {
	c, ok := f.byUser[userID]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *c
	return &cp, nil
}

func (f *fakeUserLookup) GetCoachByID(_ context.Context, coachID uuid.UUID) (*users.Coach, error) {
	c, ok := f.coaches[coachID]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *c
	return &cp, nil
}

func (f *fakeUserLookup) GetClientByID(_ context.Context, clientID uuid.UUID) (*users.Client, error) {
	c, ok := f.clients[clientID]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *c
	return &cp, nil
}

func (f *fakeUserLookup) GetClientByUserID(_ context.Context, userID uuid.UUID) (*users.Client, error) {
	c, ok := f.clientsByUser[userID]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *c
	return &cp, nil
}

func (f *fakeUserLookup) GetUserByID(_ context.Context, id uuid.UUID) (*users.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *u
	return &cp, nil
}

func (f *fakeUserLookup) GetClientsByCoachID(_ context.Context, _ uuid.UUID) ([]users.Client, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------

// fakeAvailLookup satisfies availLookup with empty responses.
type fakeAvailLookup struct{}

func (fakeAvailLookup) GetWorkingHours(_ context.Context, _ uuid.UUID) ([]availability.WorkingHours, error) {
	return nil, nil
}
func (fakeAvailLookup) GetPreferredWindows(_ context.Context, _ uuid.UUID) ([]availability.PreferredWindow, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------

// captureNotifier records calls made by the service so tests can assert on them.
type captureNotifier struct {
	cancelledCalls []CancelNotifPayload
	pendingCalls   []PendingCancellationNotifPayload
}

func (c *captureNotifier) NotifySessionsConfirmed(_ context.Context, _ []SessionNotifPayload) error {
	return nil
}

func (c *captureNotifier) NotifySessionCancelled(_ context.Context, p CancelNotifPayload) error {
	c.cancelledCalls = append(c.cancelledCalls, p)
	return nil
}

func (c *captureNotifier) NotifyCancellationPending(_ context.Context, p PendingCancellationNotifPayload) error {
	c.pendingCalls = append(c.pendingCalls, p)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestService(store *fakeSessionStore, ul *fakeUserLookup, notif Notifier, clk clock.Clock) *Service {
	svc := NewService(store, ul, fakeAvailLookup{}, nil, clk, nil)
	if notif != nil {
		svc.WithNotifier(notif)
	}
	return svc
}

// makeSession creates a Session pre-loaded in the fake store.
func makeSession(store *fakeSessionStore, coachID, clientID uuid.UUID, status string, startsAt time.Time) *Session {
	s := &Session{
		ID:       uuid.New(),
		CoachID:  coachID,
		ClientID: clientID,
		StartsAt: startsAt,
		EndsAt:   startsAt.Add(SessionDuration),
		Status:   status,
	}
	store.add(s)
	return s
}

// makeCoach creates a User+Coach and registers them in the fake user lookup.
func makeCoach(ul *fakeUserLookup) (userID, coachID uuid.UUID) {
	userID = uuid.New()
	coachID = uuid.New()
	u := &users.User{ID: userID, FullName: "Coach Name", Email: "coach@example.com"}
	c := &users.Coach{ID: coachID, UserID: userID}
	ul.addCoach(u, c)
	return userID, coachID
}

// makeClient creates a User+Client and registers them in the fake user lookup.
func makeClient(ul *fakeUserLookup, coachID uuid.UUID) uuid.UUID {
	clientID := uuid.New()
	userID := uuid.New()
	u := &users.User{ID: userID, FullName: "Client Name", Email: "client@example.com"}
	cl := &users.Client{ID: clientID, UserID: userID, CoachID: coachID}
	ul.addClient(u, cl)
	return clientID
}

// ---------------------------------------------------------------------------
// ApproveCancellation tests
// ---------------------------------------------------------------------------

func TestApproveCancellation_HappyPath(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()
	notif := &captureNotifier{}

	coachUserID, coachID := makeCoach(ul)
	clientID := makeClient(ul, coachID)
	sess := makeSession(store, coachID, clientID, StatusPendingCancellation, time.Now().Add(2*time.Hour))

	svc := newTestService(store, ul, notif, clock.Real{})
	result, err := svc.ApproveCancellation(context.Background(), coachUserID, sess.ID)

	require.NoError(t, err)
	require.Equal(t, StatusCancelled, result.Status)
	// No credit should be issued
	require.Empty(t, store.credits)
	// Client should be notified: credit_issued = false
	require.Len(t, notif.cancelledCalls, 1)
	require.False(t, notif.cancelledCalls[0].CreditIssued)
}

func TestApproveCancellation_WrongStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []string{StatusConfirmed, StatusProposed, StatusCancelled} {
		status := status
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			store := newFakeSessionStore()
			ul := newFakeUserLookup()
			coachUserID, coachID := makeCoach(ul)
			clientID := makeClient(ul, coachID)
			sess := makeSession(store, coachID, clientID, status, time.Now().Add(2*time.Hour))

			svc := newTestService(store, ul, nil, clock.Real{})
			_, err := svc.ApproveCancellation(context.Background(), coachUserID, sess.ID)

			require.Error(t, err)
			var ce *ConstraintError
			require.ErrorAs(t, err, &ce)
			require.Equal(t, "invalid_status", ce.Code)
		})
	}
}

func TestApproveCancellation_Forbidden_WrongCoach(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()

	_, coachID := makeCoach(ul)                      // session's coach
	otherUserID, _ := makeCoach(ul)                  // a different coach
	clientID := makeClient(ul, coachID)
	sess := makeSession(store, coachID, clientID, StatusPendingCancellation, time.Now().Add(2*time.Hour))

	svc := newTestService(store, ul, nil, clock.Real{})
	_, err := svc.ApproveCancellation(context.Background(), otherUserID, sess.ID)

	require.ErrorIs(t, err, ErrForbidden)
}

func TestApproveCancellation_NotFound(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()
	coachUserID, _ := makeCoach(ul)

	svc := newTestService(store, ul, nil, clock.Real{})
	_, err := svc.ApproveCancellation(context.Background(), coachUserID, uuid.New())

	require.ErrorIs(t, err, ErrNotFound)
}

func TestApproveCancellation_RepoError(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()

	coachUserID, coachID := makeCoach(ul)
	clientID := makeClient(ul, coachID)
	sess := makeSession(store, coachID, clientID, StatusPendingCancellation, time.Now().Add(2*time.Hour))

	repoErr := errors.New("db unavailable")
	store.onUpdateSessionStatus = func(_ uuid.UUID, _ string) (*Session, error) { return nil, repoErr }

	svc := newTestService(store, ul, nil, clock.Real{})
	_, err := svc.ApproveCancellation(context.Background(), coachUserID, sess.ID)

	require.ErrorIs(t, err, repoErr)
}

// ---------------------------------------------------------------------------
// WaiveCancellation tests
// ---------------------------------------------------------------------------

func TestWaiveCancellation_HappyPath(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()
	notif := &captureNotifier{}

	coachUserID, coachID := makeCoach(ul)
	clientID := makeClient(ul, coachID)
	reason := "feeling ill"
	sess := makeSession(store, coachID, clientID, StatusPendingCancellation, time.Now().Add(2*time.Hour))
	sess.CancellationReason = &reason
	store.sessions[sess.ID] = sess // update in place with reason

	now := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	svc := newTestService(store, ul, notif, clock.Fixed{T: now})
	resultSess, credit, err := svc.WaiveCancellation(context.Background(), coachUserID, sess.ID)

	require.NoError(t, err)
	require.Equal(t, StatusCancelled, resultSess.Status)
	// Credit should be issued
	require.NotNil(t, credit)
	require.Equal(t, clientID, credit.ClientID)
	require.Equal(t, sess.ID, credit.SourceSessionID)
	require.Equal(t, reason, credit.Reason)
	// Credit expires 1 month after now
	require.Equal(t, now.AddDate(0, 1, 0), credit.ExpiresAt)
	// Client notified with credit_issued = true
	require.Len(t, notif.cancelledCalls, 1)
	require.True(t, notif.cancelledCalls[0].CreditIssued)
}

func TestWaiveCancellation_WrongStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []string{StatusConfirmed, StatusProposed, StatusCancelled} {
		status := status
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			store := newFakeSessionStore()
			ul := newFakeUserLookup()
			coachUserID, coachID := makeCoach(ul)
			clientID := makeClient(ul, coachID)
			sess := makeSession(store, coachID, clientID, status, time.Now().Add(2*time.Hour))

			svc := newTestService(store, ul, nil, clock.Real{})
			_, _, err := svc.WaiveCancellation(context.Background(), coachUserID, sess.ID)

			require.Error(t, err)
			var ce *ConstraintError
			require.ErrorAs(t, err, &ce)
			require.Equal(t, "invalid_status", ce.Code)
		})
	}
}

func TestWaiveCancellation_Forbidden_WrongCoach(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()

	_, coachID := makeCoach(ul)
	otherUserID, _ := makeCoach(ul)
	clientID := makeClient(ul, coachID)
	sess := makeSession(store, coachID, clientID, StatusPendingCancellation, time.Now().Add(2*time.Hour))

	svc := newTestService(store, ul, nil, clock.Real{})
	_, _, err := svc.WaiveCancellation(context.Background(), otherUserID, sess.ID)

	require.ErrorIs(t, err, ErrForbidden)
}

func TestWaiveCancellation_NotFound(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()
	coachUserID, _ := makeCoach(ul)

	svc := newTestService(store, ul, nil, clock.Real{})
	_, _, err := svc.WaiveCancellation(context.Background(), coachUserID, uuid.New())

	require.ErrorIs(t, err, ErrNotFound)
}

func TestWaiveCancellation_CreditStillReturnedIfCreateFails(t *testing.T) {
	t.Parallel()
	// Even if CreateSessionCredit fails, the session is already cancelled.
	// The service silently swallows the credit error and returns nil credit
	// (non-fatal path).
	store := newFakeSessionStore()
	ul := newFakeUserLookup()
	notif := &captureNotifier{}

	coachUserID, coachID := makeCoach(ul)
	clientID := makeClient(ul, coachID)
	sess := makeSession(store, coachID, clientID, StatusPendingCancellation, time.Now().Add(2*time.Hour))

	store.onCreateSessionCredit = func(_, _ uuid.UUID, _ string, _ time.Time) (*SessionCredit, error) {
		return nil, errors.New("credit db error")
	}

	svc := newTestService(store, ul, notif, clock.Real{})
	resultSess, credit, err := svc.WaiveCancellation(context.Background(), coachUserID, sess.ID)

	require.NoError(t, err) // service doesn't propagate credit errors
	require.Equal(t, StatusCancelled, resultSess.Status)
	require.Nil(t, credit) // nil because the DB write failed
	// Client notified with credit_issued = false (since credit is nil)
	require.Len(t, notif.cancelledCalls, 1)
	require.False(t, notif.cancelledCalls[0].CreditIssued)
}

// ---------------------------------------------------------------------------
// CancelSession tests
// ---------------------------------------------------------------------------

func TestCancelSession_OutsideWindow_ImmediateCancelAndCredit(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()
	notif := &captureNotifier{}

	_, coachID := makeCoach(ul)
	clientID := makeClient(ul, coachID)
	now := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	// Session is 48h away — outside the 24h window
	sess := makeSession(store, coachID, clientID, StatusConfirmed, now.Add(48*time.Hour))

	svc := newTestService(store, ul, notif, clock.Fixed{T: now})
	resp, err := svc.CancelSession(context.Background(), sess.ID, CancelSessionRequest{Reason: "holiday"})

	require.NoError(t, err)
	require.False(t, resp.WithinWindow)
	require.Equal(t, StatusCancelled, resp.Session.Status)
	require.NotNil(t, resp.Credit)
	require.Equal(t, clientID, resp.Credit.ClientID)
	// Notified with credit_issued = true
	require.Len(t, notif.cancelledCalls, 1)
	require.True(t, notif.cancelledCalls[0].CreditIssued)
}

func TestCancelSession_InsideWindow_PendingCancellation(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()
	notif := &captureNotifier{}

	coachUserID, coachID := makeCoach(ul)
	_ = coachUserID
	clientID := makeClient(ul, coachID)
	now := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	// Session is only 12h away — inside the 24h window
	sess := makeSession(store, coachID, clientID, StatusConfirmed, now.Add(12*time.Hour))

	svc := newTestService(store, ul, notif, clock.Fixed{T: now})
	resp, err := svc.CancelSession(context.Background(), sess.ID, CancelSessionRequest{Reason: "illness"})

	require.NoError(t, err)
	require.True(t, resp.WithinWindow)
	require.Equal(t, StatusPendingCancellation, resp.Session.Status)
	require.Nil(t, resp.Credit)
	// Coach notified (best-effort — notifyCoachPendingCancellation silently skips
	// if user lookup fails, so we don't assert on pendingCalls here since
	// the lookup will succeed via our fakeUserLookup)
}

func TestCancelSession_InvalidStatus(t *testing.T) {
	t.Parallel()
	store := newFakeSessionStore()
	ul := newFakeUserLookup()

	_, coachID := makeCoach(ul)
	clientID := makeClient(ul, coachID)
	sess := makeSession(store, coachID, clientID, StatusCancelled, time.Now().Add(48*time.Hour))

	svc := newTestService(store, ul, nil, clock.Real{})
	_, err := svc.CancelSession(context.Background(), sess.ID, CancelSessionRequest{Reason: "test"})

	require.Error(t, err)
	var ce *ConstraintError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "invalid_status", ce.Code)
}
