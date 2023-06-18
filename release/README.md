The release build of exim_exporter on linux amd64 currently enables systemd journald support via CGO. The docker compose
configuration here can be used to test the release build process from other platforms. Simply run:

```bash
docker compose -f release/docker-compose.yml run release
```

Which will output build artifacts to `dist/`.