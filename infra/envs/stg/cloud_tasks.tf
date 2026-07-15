# The near-term Cloud Tasks queue (goatos-stg-near-term-kernel) was retired with
# the notification-dispatcher Cloud Run Job. That queue enqueued tasks that
# invoked the job's :run URL for sub-minute notification delivery. Notifications
# now stay durable in notification_requests and are drained idempotently by the
# kernel worker's 1-minute fast-lane NotificationDispatcherStage (<=1-minute
# added latency, no loss). If a real sub-minute SLA is required later, reintroduce
# a queue that targets a dedicated authenticated dispatch path — never an HTTP
# work-trigger on the worker service. See
# docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
