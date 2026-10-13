.PHONY: seed typecheck sh

# Отсутствующий источник bind-mount Docker создаёт от root, и tools под uid хоста туда не запишет
.state:
	mkdir -p $@

seed: typecheck | .state
	docker compose run --rm tools node src/seed.ts

typecheck: | .state
	docker compose run --rm tools npx tsc

sh: | .state
	docker compose run --rm tools bash
