---
version: '3'

services:
  netbootxyz:
    image: {IMAGE}
    container_name: netbootxyz
    environment:
      - MENU_VERSION={MENU_VERSION}
      - NGINX_PORT={NGINX_PORT}  # optional
      - WEB_APP_PORT={WEB_APP_PORT}  # optional
    volumes:
      - {PACKAGE_HOME}/config:/config
      - {PACKAGE_HOME}/assets:/assets
      - {DNSMASQ_CONFIG}:/etc/dnsmasq.d
    ports:
      - {EXT_DHCP_PORT}:69/udp
      # optional, destination should match env(NGINX_PORT) variable above.
      - {EXT_NGINX_PORT}:{NGINX_PORT}
      # optional, destination should match env(WEB_APP_PORT) variable above.
      - {EXT_WEB_APP_PORT}:{WEB_APP_PORT}
    restart: unless-stopped
