---
version: '3'

services:
  maas:
    image: craigbender/cmaas:jammy-3.3
    environment:
      CHAT_RESTRICTION: "Disabled"
      ENABLE_LAN_VISIBILITY: "true"
      EULA: "TRUE"
      GROW_TREES: "TRUE"
      SERVER_NAME: "Minecraft Bedrock"
    name:
    network_mode: "host"
    ports:
      - "19132:19132/udp"
    restart: unless-stopped
    stdin_open: true
    tty: true
    volumes:
      - bedrock:/data

volumes:
  bedrock:
  # not { external: true } because we want to create this volume as-needed
  # TODO: can we configure this at Wizard ?

# export PGCON="postgres://maas:maas@192.168.0.159:5432/maasdb";
# export MAAS_DOMAIN=craigbender.me
# export MAAS_CONTAINER_NAME=maas
# export MAAS_PROFILE=admin
# export MAAS_PASS=admin
# export MAAS_EMAIL=maas-admin@'MAAS_DOMAIN'
# export MAAS_SSH_IMPORT_ID=gh:chickenandpork
# export MAAS_URL="http://localhost:5240/MAAS"
#
# docker run \
#   --rm \
#   -it \
#   --name 'MAAS_CONTAINER_NAME' \
#   -h 'MAAS_CONTAINER_NAME'.'MAAS_DOMAIN' \
#   -p 5240:5240 \
#   --privileged \
#   craigbender/cmaas:jammy-3.3 \
#   "maas init region+rack --maas-url 'MAAS_URL' --database-uri 'PGCON' --force;maas createadmin --username 'MAAS_PROFILE' --password 'MAAS_PASS' --email 'MAAS_EMAIL' --ssh-import 'MAAS_SSH_IMPORT_ID';maas login 'MAAS_PROFILE' http://localhost:5240/MAAS \$(maas apikey --username 'MAAS_PROFILE');bash"
#
