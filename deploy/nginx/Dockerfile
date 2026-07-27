FROM nginx:1.27-alpine

RUN apk --no-cache add curl certbot

# Remove default config
RUN rm -f /etc/nginx/conf.d/default.conf

# Copy our production config
COPY deploy/nginx/nginx.conf /etc/nginx/conf.d/nexa.conf

# Create SSL directory
RUN mkdir -p /etc/letsencrypt/live/nexa

# Health check
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD curl -f http://localhost/health || exit 1

EXPOSE 80 443

CMD ["nginx", "-g", "daemon off;"]