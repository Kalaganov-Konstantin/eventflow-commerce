-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create notification templates table
CREATE TABLE notification_templates (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) UNIQUE NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('email', 'sms', 'push', 'webhook')),
    subject_template TEXT,
    body_template TEXT NOT NULL,
    metadata JSONB, -- template-specific settings
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create notifications table
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    recipient_id UUID NOT NULL, -- user/customer ID
    recipient_address VARCHAR(255) NOT NULL, -- email, phone, device_token, webhook_url
    type VARCHAR(50) NOT NULL CHECK (type IN ('email', 'sms', 'push', 'webhook')),
    template_id UUID REFERENCES notification_templates(id),
    subject VARCHAR(500),
    body TEXT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'delivered', 'failed', 'bounced')),
    priority INTEGER DEFAULT 3 CHECK (priority BETWEEN 1 AND 5), -- 1 = highest, 5 = lowest
    scheduled_at TIMESTAMP WITH TIME ZONE,
    sent_at TIMESTAMP WITH TIME ZONE,
    delivered_at TIMESTAMP WITH TIME ZONE,
    failed_at TIMESTAMP WITH TIME ZONE,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    error_message TEXT,
    metadata JSONB, -- delivery-specific data
    reference_id UUID, -- order_id, payment_id, etc.
    reference_type VARCHAR(50), -- 'order', 'payment', etc.
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create notification events table for tracking delivery events
CREATE TABLE notification_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    notification_id UUID NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL CHECK (event_type IN ('sent', 'delivered', 'bounced', 'opened', 'clicked', 'failed')),
    event_data JSONB,
    provider_id VARCHAR(100), -- external provider's message ID
    occurred_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create user preferences table
CREATE TABLE user_notification_preferences (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL,
    notification_type VARCHAR(100) NOT NULL, -- 'order_confirmation', 'payment_success', etc.
    channel VARCHAR(50) NOT NULL CHECK (channel IN ('email', 'sms', 'push')),
    is_enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for notification templates
CREATE INDEX idx_notification_templates_type ON notification_templates(type);
CREATE INDEX idx_notification_templates_name ON notification_templates(name);
CREATE INDEX idx_notification_templates_active ON notification_templates(is_active);

-- Create indexes for notifications
CREATE INDEX idx_notifications_recipient_id ON notifications(recipient_id);
CREATE INDEX idx_notifications_type ON notifications(type);
CREATE INDEX idx_notifications_status ON notifications(status);
CREATE INDEX idx_notifications_priority ON notifications(priority);
CREATE INDEX idx_notifications_scheduled_at ON notifications(scheduled_at);
CREATE INDEX idx_notifications_created_at ON notifications(created_at DESC);
CREATE INDEX idx_notifications_reference ON notifications(reference_id, reference_type);
CREATE INDEX idx_notifications_retry ON notifications(status, retry_count) WHERE status = 'failed';

-- Create indexes for notification events
CREATE INDEX idx_notification_events_notification_id ON notification_events(notification_id);
CREATE INDEX idx_notification_events_type ON notification_events(event_type);
CREATE INDEX idx_notification_events_occurred_at ON notification_events(occurred_at DESC);
CREATE INDEX idx_notification_events_provider_id ON notification_events(provider_id);

-- Create indexes for user preferences
CREATE INDEX idx_user_preferences_user_id ON user_notification_preferences(user_id);
CREATE INDEX idx_user_preferences_type ON user_notification_preferences(notification_type);
CREATE UNIQUE INDEX idx_user_preferences_unique ON user_notification_preferences(user_id, notification_type, channel);

-- Create trigger function for updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create triggers for updated_at
CREATE TRIGGER update_notification_templates_updated_at
    BEFORE UPDATE ON notification_templates
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_notifications_updated_at
    BEFORE UPDATE ON notifications
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_user_preferences_updated_at
    BEFORE UPDATE ON user_notification_preferences
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
