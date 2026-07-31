#!/usr/bin/env bash
# Start serveo SSH tunnel in background
export http_proxy=""
export https_proxy=""

nohup ssh \
    -o StrictHostKeyChecking=no \
    -o ServerAliveInterval=60 \
    -o ServerAliveCountMax=3 \
    -o ExitOnForwardFailure=yes \
    -o ProxyCommand="nc -X connect -x 127.0.0.1:18080 %h %p" \
    -R 80:localhost:8080 \
    serveo.net > /tmp/serveo-tunnel.log 2>&1 &
echo $! > /tmp/serveo-tunnel.pid
