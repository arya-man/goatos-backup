# Grafana Alloy Docker image for Goat OS Faro RUM receiver.
# Exports browser RUM (traces + logs) directly to Google Cloud.

FROM grafana/alloy:latest

# Copy the Alloy configuration into the container.
COPY alloy-config.alloy /etc/alloy/config.alloy

# Alloy runs on port 8080 (the faro.receiver listen_port — see alloy-config.alloy).
EXPOSE 8080

# Use the base image's default entrypoint (/bin/alloy) and pass config args.
#
# PORT COLLISION GUARD (live incident, 2026-07-13): Alloy's OWN control-plane
# HTTP server (UI/API, started by --server.http.listen-addr) and the
# faro.receiver component's dedicated `server { listen_port = 8080 }` listener
# are TWO SEPARATE net.Listen calls inside the same process. Cloud Run only
# forwards the container's single exposed port (8080) to the internet, so only
# ONE of them may bind 0.0.0.0:8080. Pointing --server.http.listen-addr at
# 0.0.0.0:8080 (as before) makes Alloy's own control server win the race on
# every cold start; the faro.receiver's listener then fails with "address
# already in use" and NEVER comes up (confirmed live: every request to the
# Alloy URL, including a raw Faro-shaped POST, returned Alloy's own React UI
# `index.html`, not a receiver response — RUM was silently not being ingested
# despite the service reporting Ready). Fix: bind Alloy's own control server to
# loopback-only (not externally reachable, and not exposed via Cloud Run
# ingress anyway) so faro.receiver's `listen_port = 8080` is the one listener
# on the container's public port.
# --feature.community-components.enabled is required for otelcol.exporter.googlecloud.
CMD ["run", \
     "--server.http.listen-addr=127.0.0.1:12345", \
     "--feature.community-components.enabled=true", \
     "/etc/alloy/config.alloy"]
