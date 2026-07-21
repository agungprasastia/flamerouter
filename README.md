# FlameRouter

Go rewrite of 9router.

```bash
go run ./cmd/flamerouter version
```

Default port: `20128`. Data dir: `~/.flamerouter`.

## Dashboard (SPA)

Dev:
1. `go run ./cmd/flamerouter serve`
2. `cd web && npm run dev` → http://127.0.0.1:5173

Prod UI into binary:
1. `cd web && npm run build`  # copies to internal/gateway/ui/dist
2. `go build -o flamerouter.exe ./cmd/flamerouter`
3. `./flamerouter serve` → http://localhost:20128
