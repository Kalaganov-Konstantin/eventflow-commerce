-- Drop triggers
DROP TRIGGER IF EXISTS update_user_preferences_updated_at ON user_notification_preferences;
DROP TRIGGER IF EXISTS update_notifications_updated_at ON notifications;
DROP TRIGGER IF EXISTS update_notification_templates_updated_at ON notification_templates;

-- Drop trigger function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop indexes
DROP INDEX IF EXISTS idx_user_preferences_unique;
DROP INDEX IF EXISTS idx_user_preferences_type;
DROP INDEX IF EXISTS idx_user_preferences_user_id;

DROP INDEX IF EXISTS idx_notification_events_provider_id;
DROP INDEX IF EXISTS idx_notification_events_occurred_at;
DROP INDEX IF EXISTS idx_notification_events_type;
DROP INDEX IF EXISTS idx_notification_events_notification_id;

DROP INDEX IF EXISTS idx_notifications_retry;
DROP INDEX IF EXISTS idx_notifications_reference;
DROP INDEX IF EXISTS idx_notifications_created_at;
DROP INDEX IF EXISTS idx_notifications_scheduled_at;
DROP INDEX IF EXISTS idx_notifications_priority;
DROP INDEX IF EXISTS idx_notifications_status;
DROP INDEX IF EXISTS idx_notifications_type;
DROP INDEX IF EXISTS idx_notifications_recipient_id;

DROP INDEX IF EXISTS idx_notification_templates_active;
DROP INDEX IF EXISTS idx_notification_templates_name;
DROP INDEX IF EXISTS idx_notification_templates_type;

-- Drop tables (order matters due to foreign keys)
DROP TABLE IF EXISTS user_notification_preferences;
DROP TABLE IF EXISTS notification_events;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS notification_templates;
