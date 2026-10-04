---
version: '3.6'

services:
  grafana:
    container_name: {{CONTAINER_NAME}}
    image: grafana/grafana:{{VERSION}}@{{DIGEST}}
    # DSM drops quotes around numeric users when generating its Compose file.
    user: grafana
    environment:
      GF_SERVER_HTTP_PORT: "3000"
      GF_PATHS_DATA: /var/lib/grafana
      GF_PATHS_PLUGINS: /var/lib/grafana/plugins
      GF_LOG_MODE: console
    ports:
      - "3000:3000"
    restart: unless-stopped
    stop_grace_period: 30s
    volumes:
      - {{VOLUME}}:/var/lib/grafana

volumes:
  # DSM runs Compose during installation, before the SPK service-start hook.
  # A stable name lets Compose create/reuse the volume at that point.
  {{VOLUME}}:
    name: {{VOLUME}}
