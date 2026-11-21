DELETE FROM oidc_identities WHERE user_id IN (SELECT user_id FROM account_states WHERE email_verified=FALSE AND verification_required=TRUE);
DELETE FROM account_tokens WHERE purpose='verify' AND EXISTS (SELECT 1 FROM settings WHERE setting_key='manual_email_verification' AND value='true');
