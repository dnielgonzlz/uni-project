// Package calendar generates iCalendar (RFC 5545) feeds for coaches and clients.
// Each user has a long-lived, opaque calendar token embedded in their subscription
// URL. The token never expires and can be regenerated from profile settings to
// revoke an old URL.
package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/danielgonzalez/pt-scheduler/internal/scheduling"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

const (
	prodID          = "-//PT Scheduler//PT Scheduler 1.0//EN"
	icsDateTimeForm = "20060102T150405Z"
)

// GenerateFeed builds a complete VCALENDAR document from the given sessions.
// The ownerName is used to personalise SUMMARY lines ("PT Session with Alice").
// Cancelled sessions are included with STATUS:CANCELLED so calendar clients
// remove them automatically on the next subscription refresh.
func GenerateFeed(owner *users.User, sessions []scheduling.Session, counterpartNames map[string]string) string {
	var b strings.Builder

	writeLine(&b, "BEGIN:VCALENDAR")
	writeLine(&b, "VERSION:2.0")
	writeLine(&b, "PRODID:"+prodID)
	writeLine(&b, "CALSCALE:GREGORIAN")
	writeLine(&b, "METHOD:PUBLISH")
	writeLine(&b, "X-WR-CALNAME:PT Scheduler — "+owner.FullName)
	writeLine(&b, "X-WR-TIMEZONE:Europe/London")
	// Sync hint: ask calendar clients to refresh frequently (in seconds).
	// Google Calendar ignores this and polls every 12–24 h regardless.
	writeLine(&b, "X-PUBLISHED-TTL:PT1H")

	for _, s := range sessions {
		writeEvent(&b, owner, s, counterpartNames)
	}

	writeLine(&b, "END:VCALENDAR")
	return b.String()
}

func writeEvent(b *strings.Builder, owner *users.User, s scheduling.Session, counterpartNames map[string]string) {
	counterpart := counterpartNames[s.ClientID.String()]
	if owner.Role == users.RoleClient {
		counterpart = counterpartNames[s.CoachID.String()]
	}

	var summary string
	if counterpart != "" {
		if owner.Role == users.RoleCoach {
			summary = fmt.Sprintf("PT Session — %s", counterpart)
		} else {
			summary = fmt.Sprintf("PT Session with %s", counterpart)
		}
	} else {
		summary = "PT Session"
	}

	status := "CONFIRMED"
	if s.Status == scheduling.StatusCancelled || s.Status == scheduling.StatusPendingCancellation {
		status = "CANCELLED"
	}

	writeLine(b, "BEGIN:VEVENT")
	// UID must be stable across refreshes so calendar apps update rather than duplicate.
	writeLine(b, "UID:"+s.ID.String()+"@pt-scheduler")
	writeLine(b, "DTSTAMP:"+formatUTC(s.UpdatedAt))
	writeLine(b, "DTSTART:"+formatUTC(s.StartsAt))
	writeLine(b, "DTEND:"+formatUTC(s.EndsAt))
	writeLine(b, foldLine("SUMMARY:"+escapeText(summary)))
	writeLine(b, "STATUS:"+status)
	writeLine(b, "LAST-MODIFIED:"+formatUTC(s.UpdatedAt))

	if s.CancellationReason != nil && *s.CancellationReason != "" {
		writeLine(b, foldLine("DESCRIPTION:Cancellation reason: "+escapeText(*s.CancellationReason)))
	}

	writeLine(b, "END:VEVENT")
}

// formatUTC formats a time as an iCalendar UTC datetime string (RFC 5545 §3.3.5).
func formatUTC(t time.Time) string {
	return t.UTC().Format(icsDateTimeForm)
}

// escapeText escapes special characters in iCalendar TEXT values (RFC 5545 §3.3.11).
func escapeText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// foldLine ensures a single property line does not exceed 75 octets (RFC 5545 §3.1).
// Longer lines are folded with CRLF + single space.
func foldLine(line string) string {
	const maxOctets = 75
	if len(line) <= maxOctets {
		return line
	}
	var b strings.Builder
	for len(line) > maxOctets {
		b.WriteString(line[:maxOctets])
		b.WriteString("\r\n ")
		line = line[maxOctets:]
	}
	b.WriteString(line)
	return b.String()
}

// writeLine writes a property line terminated by CRLF as required by RFC 5545.
func writeLine(b *strings.Builder, s string) {
	b.WriteString(s)
	b.WriteString("\r\n")
}
