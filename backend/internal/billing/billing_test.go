package billing

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// --- IdempotencyKey ---

func TestIdempotencyKey_Format(t *testing.T) {
	id := uuid.MustParse("12345678-1234-1234-1234-123456789012")
	key := IdempotencyKey("stripe", id, 2025, 1)
	require.Equal(t, "stripe-12345678-1234-1234-1234-123456789012-2025-01", key)
}

func TestIdempotencyKey_DecemberPadding(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	key := IdempotencyKey("gocardless", id, 2025, 12)
	require.Equal(t, "gocardless-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee-2025-12", key)
}

func TestIdempotencyKey_UniquePerMonth(t *testing.T) {
	id := uuid.New()
	jan := IdempotencyKey("stripe", id, 2025, 1)
	feb := IdempotencyKey("stripe", id, 2025, 2)
	require.NotEqual(t, jan, feb)
}

func TestIdempotencyKey_UniquePerProvider(t *testing.T) {
	id := uuid.New()
	stripe := IdempotencyKey("stripe", id, 2025, 6)
	gc := IdempotencyKey("gocardless", id, 2025, 6)
	require.NotEqual(t, stripe, gc)
}

func TestIdempotencyKey_UniquePerClient(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	keyA := IdempotencyKey("stripe", a, 2025, 6)
	keyB := IdempotencyKey("stripe", b, 2025, 6)
	require.NotEqual(t, keyA, keyB)
}

// --- BacsEarliestChargeDate ---

// weekday returns a time.Time on the given weekday in the given week.
func monday(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return d.UTC()
}

func TestBacsEarliestChargeDate_SubsequentPayment_SkipsWeekend(t *testing.T) {
	// Friday 2025-01-03: +2 working days = Tuesday 2025-01-07 (skipping Sat+Sun)
	from := monday(t, "2025-01-03") // Friday
	got := BacsEarliestChargeDate(from, false)
	want := monday(t, "2025-01-07") // Tuesday
	require.Equal(t, want, got)
}

func TestBacsEarliestChargeDate_FirstPayment_ThreeWorkingDays(t *testing.T) {
	// Monday 2025-01-06: +3 working days = Thursday 2025-01-09
	from := monday(t, "2025-01-06") // Monday
	got := BacsEarliestChargeDate(from, true)
	want := monday(t, "2025-01-09") // Thursday
	require.Equal(t, want, got)
}

func TestBacsEarliestChargeDate_Thursday_SubsequentSpansWeekend(t *testing.T) {
	// Thursday 2025-01-09: +2 working days = Monday 2025-01-13 (skipping Sat+Sun)
	from := monday(t, "2025-01-09") // Thursday
	got := BacsEarliestChargeDate(from, false)
	want := monday(t, "2025-01-13") // Monday
	require.Equal(t, want, got)
}

func TestBacsEarliestChargeDate_Wednesday_FirstPayment_SpansWeekend(t *testing.T) {
	// Wednesday 2025-01-08: +3 working days = Mon 2025-01-13 (Thu+Fri+Mon, skipping Sat+Sun)
	from := monday(t, "2025-01-08") // Wednesday
	got := BacsEarliestChargeDate(from, true)
	want := monday(t, "2025-01-13") // Monday
	require.Equal(t, want, got)
}

func TestBacsEarliestChargeDate_AlwaysFutureDate(t *testing.T) {
	from := time.Now().UTC()
	got := BacsEarliestChargeDate(from, false)
	require.True(t, got.After(from), "charge date must be strictly after from")
}

func TestBacsEarliestChargeDate_NeverOnWeekend(t *testing.T) {
	// Test every day of a full week to ensure result is never Sat/Sun.
	base := monday(t, "2025-01-06") // Monday
	for i := 0; i < 7; i++ {
		from := base.AddDate(0, 0, i)
		got := BacsEarliestChargeDate(from, false)
		require.NotEqual(t, time.Saturday, got.Weekday(),
			"charge date %s from %s falls on Saturday", got.Format("2006-01-02"), from.Format("2006-01-02"))
		require.NotEqual(t, time.Sunday, got.Weekday(),
			"charge date %s from %s falls on Sunday", got.Format("2006-01-02"), from.Format("2006-01-02"))
	}
}

// --- pad helpers (internal, tested via IdempotencyKey) ---

func TestPad2_SingleDigit(t *testing.T) {
	require.Equal(t, "01", pad2(1))
	require.Equal(t, "09", pad2(9))
}

func TestPad2_TwoDigits(t *testing.T) {
	require.Equal(t, "12", pad2(12))
}

func TestPad4_Year(t *testing.T) {
	require.Equal(t, "2025", pad4(2025))
}
