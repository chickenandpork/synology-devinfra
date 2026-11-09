---
version: "3.6"

services:
  frontend:
    container_name: {{FRONTEND_CONTAINER}}
    command:
      - /config/frontend.jsonnet
    depends_on:
      - scheduler
      - storage
    expose:
      - 9980
    image: {{STORAGE_IMAGE}}
    ports:
      - "8980:8980"
    volumes:
      - ./config:/config

  scheduler:
    container_name: {{SCHEDULER_CONTAINER}}
    command:
      - /config/scheduler.jsonnet
    depends_on:
      - storage
    expose:
      - 8982
      - 8983
      - 8984
      - 9980
    image: {{SCHEDULER_IMAGE}}
    ports:
      - "7982:7982"
      - "8983:8983"
      - "8984:8984"
    volumes:
      - ./config:/config

  storage:
    container_name: {{STORAGE_CONTAINER}}
    command:
      - /config/storage.jsonnet
    expose:
      - 8981
      - 9980
    image: {{STORAGE_IMAGE}}
    volumes:
      - ./config:/config
      - {{STORAGE_VOLUME_AC}}:/storage-ac
      - {{STORAGE_VOLUME_CAS}}:/storage-cas
      - {{STORAGE_VOLUME_FSAC}}:/storage-fsac

volumes:
  {{STORAGE_VOLUME_AC}}:
  {{STORAGE_VOLUME_CAS}}:
  {{STORAGE_VOLUME_FSAC}}:
