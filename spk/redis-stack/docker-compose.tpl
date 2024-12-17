---
version: '3.6'

services:
  redis-stack:
    command: --max_size=1000
    container_name: {{CONTAINER_NAME}}
    image: redis/redis-stack:{{VERSION}}
    extra_hosts:
      - "host.docker.internal:host-gateway"
    #network: host
    ports:
      - "6379:6379"
      - "8001:8001"
    restart: "unless-stopped"
    volumes:
      - {{VOLUME}}:/data

volumes:
  # created during start-stop-script, never deleted
  {{VOLUME}}: {external: true}
