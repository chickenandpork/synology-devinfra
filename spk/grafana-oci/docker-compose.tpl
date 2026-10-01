---
version: '3.6'

services:
  grafana:
    container_name: {{CONTAINER_NAME}}
    image: grafana/grafana:{{VERSION}}@{{DIGEST}}
    user: "472"
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
  # Created by the shared SPK lifecycle helper; never deleted by this package.
  {{VOLUME}}: {external: true}
