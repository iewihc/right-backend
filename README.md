# Command

```sh
docker-compose stop right-backend
docker build -f Dockerfile.fast -t right-backend-fast:latest .
docker-compose up -d right-backend
docker logs -f right-backend-app
```


http://localhost:3000/d/driver-login-api-monitoring/b9e906a?orgId=1&from=now-1h&to=now&timezone=browser&refresh=5s

http://localhost:9090/query?g0.expr=%7B__name__%3D%7E%22http.*%22%7D&g0.show_tree=0&g0.tab=graph&g0.end_input=2025-09-25+21%3A58%3A48&g0.moment_input=2025-09-25+21%3A58%3A48&g0.range_input=1h&g0.res_type=auto&g0.res_density=medium&g0.display_mode=lines&g0.show_exemplars=0

http://localhost:16686/search?end=1758896287739000&limit=20&lookback=1h&maxDuration&minDuration&operation=driver_controller_accept_order&service=right-backend&start=1758892687739000