# nutreek-store-plugins

Store plugins for [Nutreek](https://github.com/easly1989/nutreek), the self-hosted meal planner: each one reads a store's public website and answers Nutreek's plugin contract (search, product, availability), so ingredient searches can show the store's products with their nutrition, and the shopping list can show an estimate of what it costs.

| Plugin | Store | What it reads |
|---|---|---|
| `conad` | [Conad Spesa Online](https://spesaonline.conad.it) | products, prices, stock and nutrition tables |

## Use

**In Nutreek.** Settings → Stores lists these as *integrated stores*: turn one on and it runs inside Nutreek, with no extra service. Nothing to install or configure.

**As a service.** The same plugins can run on their own, for a Nutreek that connects them by address:

```sh
go run ./cmd/nutreek-store-plugins -addr 127.0.0.1:8110 -token SECRET
```

serves each under its ID (`http://127.0.0.1:8110/conad/search?q=pasta`), with the token as `Authorization: Bearer SECRET`.

## The contract

Package [`contract`](contract) has the shapes. Three `GET` calls: `/search?q=`, `/product?id=`, `/availability?id=A&id=B`. Nutrition is per 100 g or ml, and a product needs the four macros (kcal, protein, carbs, fat) to be offered. The description Nutreek keeps is `docs/store-plugins.md` in its repository.

## How it behaves with a store's site

A plugin reads what the store shows to anyone, the way a browser does, and nothing more:

- plain HTTP, a few requests a second at most, answers kept for a while;
- it says what it is in its `User-Agent`;
- it never tries to look like a browser or to get past anything a site uses to tell programs from people. If a site starts refusing it, the plugin answers with an error and that is the end of it.

Prices are those the site shows to a visitor who has chosen no store, so they can differ from a given shop's: treat them as indicative. A plugin is not an agreement with the store, and whoever runs it answers for how they use it.

## Writing a plugin

A store is a package that returns an `http.Handler` for the three calls, and an entry in [`plugins.go`](plugins.go). `conad` is the one to read: `parse.go` turns pages into products (tested on trimmed copies of real pages in `testdata`), `conad.go` fetches, limits and caches. Run `go test -tags live ./conad -run Live -v` to try it against the real site.

## License

AGPL-3.0, like Nutreek. See [LICENSE](LICENSE).
