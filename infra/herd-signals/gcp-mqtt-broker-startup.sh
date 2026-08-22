#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="goatos-stg"
MQTT_USER="gw-514060"
PASSWORD_SECRET="herd-signals-mqtt-gateway-514060-password"
CA_SECRET="herd-signals-mqtt-ca-crt"
SERVER_CERT_SECRET="herd-signals-mqtt-server-crt"
SERVER_KEY_SECRET="herd-signals-mqtt-server-key"

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y mosquitto mosquitto-clients

install -d -m 0755 /etc/mosquitto/certs
gcloud secrets versions access latest --project="${PROJECT_ID}" --secret="${CA_SECRET}" > /etc/mosquitto/certs/ca.crt
gcloud secrets versions access latest --project="${PROJECT_ID}" --secret="${SERVER_CERT_SECRET}" > /etc/mosquitto/certs/server.crt
gcloud secrets versions access latest --project="${PROJECT_ID}" --secret="${SERVER_KEY_SECRET}" > /etc/mosquitto/certs/server.key
chmod 0644 /etc/mosquitto/certs/ca.crt /etc/mosquitto/certs/server.crt
chmod 0600 /etc/mosquitto/certs/server.key
chown -R mosquitto:mosquitto /etc/mosquitto/certs

gcloud secrets versions access latest --project="${PROJECT_ID}" --secret="${PASSWORD_SECRET}" > /root/mqtt-password.txt
mosquitto_passwd -b -c /etc/mosquitto/passwd "${MQTT_USER}" "$(cat /root/mqtt-password.txt)"
rm -f /root/mqtt-password.txt
chmod 0640 /etc/mosquitto/passwd
chown mosquitto:mosquitto /etc/mosquitto/passwd

cat >/etc/mosquitto/conf.d/herd-signals.conf <<'EOF'
listener 8883 0.0.0.0
allow_anonymous false
password_file /etc/mosquitto/passwd

cafile /etc/mosquitto/certs/ca.crt
certfile /etc/mosquitto/certs/server.crt
keyfile /etc/mosquitto/certs/server.key
require_certificate false
tls_version tlsv1.2

log_dest syslog
log_type error
log_type warning
log_type notice
connection_messages true
EOF

systemctl enable mosquitto
systemctl restart mosquitto
