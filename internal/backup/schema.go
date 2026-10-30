// Package backup implements offline, portable backups without executing supplied SQL.
package backup

import "strings"

type table struct {
	name, columns, types, order string
}

// i/b/s are integer/boolean/string; uppercase permits NULL. Order is also the
// dependency order used by restore. Credentials are deliberately absent.
var tables = []table{
	{"users", "id,email,email_key,nickname,password_hash,password_method,password_salt,score,permission,banned", "issssssiib", "id"},
	{"textures", "hash,name,kind,model,is_public,deleted", "ssssbb", "hash"},
	{"texture_records", "record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted", "ssIssssbb", "record_id"},
	{"texture_metadata", "record_id,category,tags,review_state,review_reason", "sssss", "record_id"},
	{"characters", "id,user_id,uuid,name,name_key,skin_hash,cape_hash,model", "iissssss", "id"},
	{"wardrobe", "user_id,record_id,display_name,collected_at", "issi", "user_id,record_id"},
	{"account_states", "user_id,email_verified,verification_required", "ibb", "user_id"},
	{"user_profiles", "user_id,avatar_mode,character_uuid,avatar_image", "isss", "user_id"},
	{"user_totp", "user_id,secret_encrypted,enabled,created_at,last_used_step", "isbii", "user_id"},
	{"user_totp_recovery", "user_id,code_hash", "is", "user_id,code_hash"},
	{"oidc_identities", "identity_key,issuer_key,user_id,issuer,subject,created_at", "ssissi", "identity_key"},
	{"oauth_clients", "client_id,name,redirect_uris,secret_hash,is_public,enabled", "ssssbb", "client_id"},
	{"external_identities", "identity_key,connector_id,subject,username,user_id,created_at", "ssssii", "identity_key"},
	{"settings", "setting_key,value", "ss", "setting_key"},
	{"plugins", "plugin_id,version,enabled,settings_json", "ssbs", "plugin_id"},
	{"site_notices", "notice_id,content,format,created_at", "sssi", "notice_id"},
	{"migration_state", "id,source_fingerprint,status,report", "isss", "id"},
	{"admin_events", "event_id,actor_id,action,target_type,target_id,created_at", "sisssi", "event_id"},
	{"reports", "report_id,reporter_id,record_id,texture_name,reason,status,resolution,reply,reviewer_id,created_at,resolved_at", "sissssssIii", "report_id"},
	{"point_entries", "entry_id,user_id,kind,reference_id,delta,balance_after,created_at", "sissiii", "entry_id"},
	{"legacy_reports", "id,tid,uploader,reporter,reason,status,report_at", "iiiisiS", "id"},
}

var credentialTables = []string{"web_sessions", "ygg_join_sessions", "game_tokens", "account_tokens", "invitation_codes", "oidc_flows", "oauth_codes", "oauth_tokens"}

func (t table) insert() string {
	return "INSERT INTO " + t.name + "(" + t.columns + ") VALUES(" + strings.TrimSuffix(strings.Repeat("?,", len(t.types)), ",") + ")"
}
