-- +goose Up

-- THE DASHBOARD IS A DELIVERY TARGET (2026-09-18): the CEO and the CxOs live in Chrome on
-- admin-web, not in the Android app. A leadership task raised at 11:40 reached the phone push
-- registry (workforce_member_devices) and nothing else, so the one surface those people
-- actually have open all day was the only surface that stayed silent.
--
-- WHY A SEPARATE TABLE AND NOT A 'web' ROW IN workforce_member_devices:
-- workforce_member_devices is the OPERATOR DEVICE IDENTITY table. Its columns are an Android
-- install's credentials -- device_public_key_hash, app_install_id, app_version, and a
-- status/revoked_by/revoked_at lifecycle that is the device's ability to AUTHENTICATE and
-- bootstrap (see the P0 device-lockout note on notification SuppressInvalidRecipient). A
-- browser push registration is none of that: it is a revocable, self-expiring delivery address
-- for one Chrome profile, it carries no signing key, it grants no bootstrap authority, and it
-- must be creatable and destroyable by the signed-in person themselves without touching an
-- operator's login. Widening the platform CHECK would also silently re-target a dozen existing
-- android-shaped reads (heartbeat, revoke, the operator device list UI) at rows that are not
-- phones. Separate table, same conventions.
--
-- WHY fcm_token AND NOT {endpoint, p256dh, auth}:
-- the raw Web Push subscription shape is deliberately NOT stored here. admin-web already
-- depends on the Firebase JS SDK for auth, so the browser registers through
-- firebase/messaging getToken() and receives ONE FCM registration token -- the identical shape
-- the Android registry already stores and the identical shape the backend's existing FCM HTTP
-- v1 send path (notification/adapters/gateway, channel 'push_fcm') already addresses. FCM
-- performs the VAPID signing and the aes128gcm payload encryption on our behalf. Storing an
-- endpoint/keys triple instead would have required a second, parallel send path, a second
-- delivery credential, and a second dead-address detector for no gain.
--
-- GRAIN: one row per (tenant, workforce member, browser profile). browser_install_id is a
-- client-generated opaque id persisted in that profile's localStorage; it is what makes a
-- re-register from the same Chrome profile an UPDATE instead of a second live registration, and
-- it is what lets the backend target one browser rather than a person's every browser.
--
-- EXPIRY IS NORMAL, NOT EXCEPTIONAL: Chrome silently invalidates a push subscription on profile
-- clear, on a long idle period, and whenever the user revokes the site's notification
-- permission. Nothing tells the server. Without a stale path this table only ever grows and the
-- dispatcher burns retries on addresses that can never resolve, so status carries an explicit
-- 'stale' value written by the send path's permanent-failure branch (notification
-- SuppressInvalidRecipient), alongside stale_at/stale_reason for observability.
--
-- Seed coupling note (docs/runbooks/initial-seed-migration-coupling.md): this table is born at
-- runtime from a person clicking "Enable notifications" in their own browser. No seed command
-- can or should hand-fill it -- a seeded push address belongs to no real browser and would be
-- pruned on its first send.
-- seed-fixture-guard:ignore: runtime, browser-owned push addresses; not seed input.

CREATE TABLE IF NOT EXISTS public.workforce_member_browser_push_registrations (
    browser_registration_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    -- 'web_fcm' today. Named rather than assumed so a future raw-VAPID or a second provider is
    -- an added value here, not a reinterpretation of every existing row.
    provider text DEFAULT 'web_fcm'::text NOT NULL,
    browser_install_id text NOT NULL,
    fcm_token text NOT NULL,
    -- What the browser reported about itself at register time. Display/diagnostics only -- never
    -- a predicate, never a routing input.
    user_agent text DEFAULT ''::text NOT NULL,
    browser_label text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    stale_at timestamp with time zone,
    stale_reason text,
    registered_by uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT workforce_member_browser_push_registrations_pkey PRIMARY KEY (browser_registration_id),
    CONSTRAINT workforce_member_browser_push_registrations_install_check CHECK ((btrim(browser_install_id) <> ''::text)),
    CONSTRAINT workforce_member_browser_push_registrations_token_check CHECK ((btrim(fcm_token) <> ''::text)),
    CONSTRAINT workforce_member_browser_push_registrations_provider_check CHECK ((provider = 'web_fcm'::text)),
    CONSTRAINT workforce_member_browser_push_registrations_row_version_check CHECK ((row_version >= 1)),
    -- 'stale'        -> the provider told us this address is gone (send-path prune).
    -- 'unsubscribed' -> the person turned notifications off themselves, in the dashboard.
    CONSTRAINT workforce_member_browser_push_registrations_status_check CHECK ((status = ANY (ARRAY['active'::text, 'stale'::text, 'unsubscribed'::text]))),
    CONSTRAINT workforce_member_browser_push_registrations_stale_check CHECK (((status = 'active'::text) = (stale_at IS NULL))),
    CONSTRAINT workforce_member_browser_push_registrations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id),
    CONSTRAINT workforce_member_browser_push_registrations_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id)
);

COMMENT ON TABLE public.workforce_member_browser_push_registrations IS
  'Browser (Chrome) web push addresses for admin-web, one per tenant/member/browser profile. fcm_token is an FCM web registration token addressed by the existing push_fcm channel. status=stale is written by the send path on a provider-confirmed dead address.';

COMMENT ON COLUMN public.workforce_member_browser_push_registrations.browser_install_id IS
  'Opaque client-generated id persisted in that browser profile localStorage. Upsert conflict target: a re-register from the same profile refreshes the token in place.';

-- The upsert target. A browser profile holds exactly one registration per tenant, whatever its
-- current status -- re-enabling after an unsubscribe revives the same row rather than racing a
-- second one into existence.
CREATE UNIQUE INDEX IF NOT EXISTS workforce_member_browser_push_registrations_install_unique_idx
    ON public.workforce_member_browser_push_registrations (tenant_id, browser_install_id);

-- The push fan-out predicate, in the shape of workforce_member_devices_push_reachable_idx
-- (000066): resolve a member to the browsers we can actually reach, and nothing else.
CREATE INDEX IF NOT EXISTS workforce_member_browser_push_registrations_push_reachable_idx
    ON public.workforce_member_browser_push_registrations (tenant_id, workforce_member_id)
    WHERE status = 'active';

-- The prune path is addressed BY TOKEN, not by member: the send path knows only the
-- recipient_ref that FCM rejected. Without this index the prune is a tenant-wide scan on every
-- dead address, on the dispatcher's hot path.
CREATE INDEX IF NOT EXISTS workforce_member_browser_push_registrations_token_idx
    ON public.workforce_member_browser_push_registrations (tenant_id, fcm_token);

-- Operator/diagnostic read: this person's browsers, newest activity first.
CREATE INDEX IF NOT EXISTS workforce_member_browser_push_registrations_member_status_idx
    ON public.workforce_member_browser_push_registrations (tenant_id, workforce_member_id, status, last_seen_at DESC);

-- +goose Down

DROP TABLE IF EXISTS public.workforce_member_browser_push_registrations;
