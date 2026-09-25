.PHONY: install up wait wizard seed typecheck reset down logs ps sh

install: typecheck up wait wizard seed

# Отсутствующий источник bind-mount Docker создаёт от root, и tools под uid хоста туда не запишет
.state:
	mkdir -p $@

up: | .state
	docker compose up -d youtrack

wait:
	@until [ "$$(docker compose ps --format '{{.Health}}' youtrack)" = healthy ]; do sleep 5; done

wizard: | .state
	docker compose run --rm -e WIZARD_TOKEN="$$(docker compose logs youtrack \
		| grep -o 'wizard_token=[A-Za-z0-9]*' | tail -1 | cut -d= -f2)" tools node src/wizard.ts

seed: typecheck | .state
	docker compose run --rm tools node src/seed.ts

typecheck: | .state
	docker compose run --rm tools npx tsc

reset:
	docker compose down -v
	rm -f .state/admin-token .state/limited-token .state/member-token

down:
	docker compose down

logs:
	docker compose logs -f --tail=100 youtrack

ps:
	docker compose ps

sh: | .state
	docker compose run --rm tools bash
