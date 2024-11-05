---
version: '3.6'

services:
  bazel-remote-cache:
    command: --max_size=1000
    container_name: {{CONTAINER_NAME}}
    image: buchgr/bazel-remote-cache:{{VERSION}}
    environment:
      BAZEL_REMOTE_DIR: "/data"
      BAZEL_REMOTE_MAX_SIZE: 1000
      BAZEL_REMOTE_HTTP_PORT: 8080
      BAZEL_REMOTE_GRPC_PORT: 9092
      BAZEL_REMOTE_EXPERIMENTAL_REMOTE_ASSET_API: "true"
    ports:
      - "8082:8080"
      - "9092:9092"
    restart: unless-stopped
    volumes:
      - {{VOLUME}}:/data

volumes:
  # created during start-stop-script, never deleted
  {{VOLUME}}: {external: true}
