-- Issue #121: drop OAuth 2.1 authorization-code + PKCE tables.
DROP TABLE IF EXISTS oauth_tokens;
DROP TABLE IF EXISTS oauth_auth_codes;
DROP TABLE IF EXISTS oauth_clients;
