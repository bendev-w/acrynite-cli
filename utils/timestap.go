package utils

import (
	"fmt"
	"time"
)

// TimestampFormat defines different timestamp format types
type TimestampFormat int

const (
	// TimestampLog format for log messages (2006-01-02 15:04:05)
	TimestampLog TimestampFormat = iota
	// TimestampFile format for filenames (20060102_150405)
	TimestampFile
	// TimestampBackup format for backup files (20060102-150405)
	TimestampBackup
	// TimestampSchedule format for scheduled tasks (2006-01-02T15:04:05Z07:00)
	TimestampSchedule
	// TimestampHuman format for human-readable output (Jan 2, 2006 at 3:04pm)
	TimestampHuman
	// TimestampUnix Unix timestamp (seconds since epoch)
	TimestampUnix
	// TimestampDetailed detailed format with milliseconds (2006-01-02 15:04:05.000)
	TimestampDetailed
)

// FormatTimestamp formats a timestamp according to the specified format
func FormatTimestamp(t time.Time, format TimestampFormat) string {
	switch format {
	case TimestampLog:
		return t.Format("2006-01-02 15:04:05")
	case TimestampFile:
		return t.Format("20060102_150405")
	case TimestampBackup:
		return t.Format("20060102-150405")
	case TimestampSchedule:
		return t.Format(time.RFC3339)
	case TimestampHuman:
		return t.Format("Jan 2, 2006 at 3:04pm")
	case TimestampUnix:
		return fmt.Sprintf("%d", t.Unix())
	case TimestampDetailed:
		return t.Format("2006-01-02 15:04:05.000")
	default:
		return t.Format(time.RFC3339)
	}
}

// ParseTimestamp parses a timestamp string with the given format
func ParseTimestamp(s string, format TimestampFormat) (time.Time, error) {
	var layout string
	switch format {
	case TimestampLog:
		layout = "2006-01-02 15:04:05"
	case TimestampFile:
		layout = "20060102_150405"
	case TimestampBackup:
		layout = "20060102-150405"
	case TimestampSchedule:
		layout = time.RFC3339
	case TimestampHuman:
		layout = "Jan 2, 2006 at 3:04pm"
	case TimestampDetailed:
		layout = "2006-01-02 15:04:05.000"
	default:
		layout = time.RFC3339
	}
	return time.Parse(layout, s)
}

// Now returns the current time formatted according to the specified format
func Now(format TimestampFormat) string {
	return FormatTimestamp(time.Now(), format)
}

// NowUTC returns the current UTC time formatted according to the specified format
func NowUTC(format TimestampFormat) string {
	return FormatTimestamp(time.Now().UTC(), format)
}

// GenerateBackupName generates a backup filename with timestamp
func GenerateBackupName(baseName, extension string) string {
	timestamp := FormatTimestamp(time.Now(), TimestampBackup)
	if extension != "" && extension[0] != '.' {
		extension = "." + extension
	}
	return fmt.Sprintf("%s_%s%s", baseName, timestamp, extension)
}

// GenerateLogFileName generates a log filename with date
func GenerateLogFileName(prefix string) string {
	date := time.Now().Format("20060102")
	return fmt.Sprintf("%s_%s.log", prefix, date)
}

// TimeAgo returns a human-readable "time ago" string
func TimeAgo(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		seconds := int(duration.Seconds())
		if seconds <= 1 {
			return "just now"
		}
		return fmt.Sprintf("%d seconds ago", seconds)
	case duration < time.Hour:
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case duration < 30*24*time.Hour:
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case duration < 365*24*time.Hour:
		months := int(duration.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		years := int(duration.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}

// TimestampRange represents a time range
type TimestampRange struct {
	Start time.Time
	End   time.Time
}

// NewTimestampRange creates a new timestamp range
func NewTimestampRange(start, end time.Time) *TimestampRange {
	return &TimestampRange{
		Start: start,
		End:   end,
	}
}

// Duration returns the duration of the range
func (tr *TimestampRange) Duration() time.Duration {
	return tr.End.Sub(tr.Start)
}

// Contains checks if a time is within the range
func (tr *TimestampRange) Contains(t time.Time) bool {
	return (t.Equal(tr.Start) || t.After(tr.Start)) && (t.Equal(tr.End) || t.Before(tr.End))
}

// String returns a string representation of the range
func (tr *TimestampRange) String() string {
	return fmt.Sprintf("%s to %s", 
		FormatTimestamp(tr.Start, TimestampLog),
		FormatTimestamp(tr.End, TimestampLog))
}

// MeasureExecution measures the execution time of a function
func MeasureExecution(name string, fn func()) time.Duration {
	start := time.Now()
	fn()
	duration := time.Since(start)
	return duration
}

// MeasureExecutionWithLog measures and logs the execution time
func MeasureExecutionWithLog(name string, fn func()) {
	duration := MeasureExecution(name, fn)
	Info("Execution time for '%s': %v", name, duration)
}
