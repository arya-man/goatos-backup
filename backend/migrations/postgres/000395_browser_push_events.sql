-- +goose Up

-- seed-fixture-guard:ignore: browser_push_events is runtime browser-push telemetry written by notification receipts, not vaccination or HRMS seed data.
CREATE TABLE IF NOT EXISTS public.browser_push_events (
    browser_push_event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    notification_request_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    browser_registration_id uuid NOT NULL,
    browser_install_id text,
    event_type text NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    trace_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT browser_push_events_pkey PRIMARY KEY (browser_push_event_id),
    CONSTRAINT browser_push_events_event_type_check CHECK (event_type = ANY (ARRAY['displayed'::text, 'opened'::text])),
    CONSTRAINT browser_push_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id),
    CONSTRAINT browser_push_events_notification_request_id_fkey FOREIGN KEY (notification_request_id) REFERENCES public.notification_requests(notification_request_id),
    CONSTRAINT browser_push_events_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id),
    CONSTRAINT browser_push_events_registration_id_fkey FOREIGN KEY (browser_registration_id) REFERENCES public.workforce_member_browser_push_registrations(browser_registration_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS browser_push_events_once_idx
    ON public.browser_push_events (tenant_id, notification_request_id, browser_registration_id, event_type);

CREATE INDEX IF NOT EXISTS browser_push_events_request_idx
    ON public.browser_push_events (tenant_id, notification_request_id, occurred_at);

-- +goose Down

DROP TABLE IF EXISTS public.browser_push_events;
