# The near-term Cloud Tasks queue (goatos-dev-near-term-kernel) was retired with
# the notification-dispatcher Cloud Run Job. Notifications now stay durable in
# notification_requests and are drained idempotently by the kernel worker's
# 1-minute fast-lane NotificationDispatcherStage. See
# docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
