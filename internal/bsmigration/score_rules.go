package bsmigration

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Defaults from config/options.php at CoreCommit. Only these nonsecret options
// may enter the reconciliation report; mail, URLs and plugin secrets stay out.
var scoreDefaults = map[string]string{
	"user_initial_score": "1000", "sign_gap_time": "24", "sign_score": "10,100",
	"score_per_storage": "true", "private_score_per_storage": "10",
	"return_score": "true", "score_per_player": "100", "sign_after_zero": "false",
	"score_per_closet_item": "0", "score_award_per_texture": "0",
	"take_back_scores_after_deletion": "true", "score_award_per_like": "0",
	"reporter_score_modification": "0", "reporter_reward_score": "0",
}

var scoreValue = regexp.MustCompile(`^(true|false|-?[0-9]+(,-?[0-9]+)?)$`)

func readScoreRules(ctx context.Context, tx *sql.Tx, quotedOptionsTable string) (map[string]string, error) {
	rules := make(map[string]string, len(scoreDefaults))
	keys := make([]string, 0, len(scoreDefaults))
	for key, value := range scoreDefaults {
		keys = append(keys, "'"+key+"'")
		rules[key] = value
	}
	sort.Strings(keys)
	rows, err := tx.QueryContext(ctx, "SELECT option_name,option_value FROM "+quotedOptionsTable+" WHERE option_name IN ("+strings.Join(keys, ",")+")")
	if err != nil {
		return nil, fmt.Errorf("read source score rules")
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("read source score rule")
		}
		if seen[key] || !validScoreRule(key, value) {
			return nil, fmt.Errorf("invalid or duplicate source score rule %s", key)
		}
		seen[key], rules[key] = true, value
	}
	return rules, rows.Err()
}

func validScoreRule(key, value string) bool {
	_, known := scoreDefaults[key]
	return known && len(value) <= 128 && scoreValue.MatchString(value)
}
