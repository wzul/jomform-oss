-- Issue #92 down: drop the event-tracking tables and their indexes.
DROP TABLE IF EXISTS email_delivery_events;
DROP TABLE IF EXISTS events;
