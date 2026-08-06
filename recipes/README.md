# Official Grain recipes

Portable `grain/v1` **Sandbox** recipes. Index: [`catalog.json`](./catalog.json).

## Use

```bash
grain recipe search
grain recipe add git-lab          # installs into ~/.grain/recipes/
grain recipe preview git-lab
grain new --recipe git-lab
```

Desktop: **Recipes → Browse official → Add**, or **New from form…**.

## Contribute (official only)

1. Add `recipes/<id>.yaml` (valid `apiVersion: grain/v1`, `kind: Sandbox`).
2. Update `catalog.json` (include `sha256` of the file bytes).
3. Open a **GitHub PR** — no accounts/marketplace; PRs are the only path into official.

Do **not** bulk-install all recipes into releases by default (keep installs small).

## Download counts

When/if recipes are published as GitHub Release assets, public `download_count` is available from the Releases API. Until then, catalog is git-sourced (clone/raw traffic is not a public counter).
