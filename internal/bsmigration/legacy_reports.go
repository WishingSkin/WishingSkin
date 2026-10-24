package bsmigration

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Preserve DATETIME text, including zero dates. Do not assign a source timezone.
var legacyReportTime = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?$`)

func readLegacyReports(ctx context.Context, tx *sql.Tx, quotedTable string) (*LegacyReports, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,tid,uploader,reporter,reason,status,CAST(report_at AS CHAR) FROM "+quotedTable+" ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("read source report history")
	}
	defer rows.Close()
	history := &LegacyReports{Items: []LegacyReport{}}
	for rows.Next() {
		var item LegacyReport
		var at sql.NullString
		if err := rows.Scan(&item.ID, &item.Texture, &item.Uploader, &item.Reporter, &item.Reason, &item.Status, &at); err != nil {
			return nil, fmt.Errorf("read source report history row")
		}
		if at.Valid {
			item.ReportAt = &at.String
		}
		history.Items = append(history.Items, item)
	}
	return history, rows.Err()
}

func checkLegacyReports(s *Snapshot, r *Report, block func(string, string, string)) {
	r.LegacyReports = LegacyReportsReport{Policy: "read_only_history", Source: "unavailable_in_snapshot"}
	if s.LegacyReports == nil {
		return
	}
	if s.FormatVersion == 1 {
		block("version", "legacy_reports", "report history requires snapshot format v2")
	}
	if s.LegacyReports.Items == nil {
		block("legacy_reports", "legacy_reports", "report history requires an explicit item list")
		return
	}
	r.LegacyReports.Source = "included"
	r.LegacyReports.Total = len(s.LegacyReports.Items)
	seen := map[int64]bool{}
	for _, item := range s.LegacyReports.Items {
		scope := fmt.Sprintf("legacy_report:%d", item.ID)
		if item.ID <= 0 || seen[item.ID] {
			block("duplicate_id", scope, "invalid or duplicate historical report ID")
		}
		seen[item.ID] = true
		switch item.Status {
		case 0:
			r.LegacyReports.Pending++
		case 1:
			r.LegacyReports.Resolved++
		case 2:
			r.LegacyReports.Rejected++
		default:
			block("report_status", scope, "unknown source report status")
		}
		// PostgreSQL TEXT cannot store NUL; stop instead of modifying source text.
		if !utf8.ValidString(item.Reason) || strings.ContainsRune(item.Reason, 0) {
			block("report_reason", scope, "historical reason cannot be represented without changing its text")
		}
		if item.ReportAt != nil && !legacyReportTime.MatchString(*item.ReportAt) {
			block("report_time", scope, "historical time must remain source DATETIME text or null")
		}
	}
}
