.PHONY: publish

ENV_FILE ?= .env

publish:
	@if [ ! -f "$(ENV_FILE)" ]; then \
		echo "$(ENV_FILE) file is required. Copy .env.example first."; \
		exit 1; \
	fi
	@if ! grep -Eq '^CLOUDFLARE_TUNNEL_TOKEN=.+$$' "$(ENV_FILE)"; then \
		echo "CLOUDFLARE_TUNNEL_TOKEN must be set in $(ENV_FILE)"; \
		exit 1; \
	fi
	@APP_HOSTNAME="$${APP_HOSTNAME:-monitor.namjaeyoun.com}" docker compose --env-file "$(ENV_FILE)" --profile tunnel up -d --build
