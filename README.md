# traefik-throttle

Experimental Traefik middleware plugin to limit concurrent requests routed to services by delaying (rather than rejecting) requests over the limit. Useful for underpowered servers in very low-traffic environments (ie. homelabs).

## Background

Created as a hack to stabilize NextCloud running on a Raspberry Pi 2 behind Traefik. NextCloud's web UI fires many concurrent requests on load, which overwhelmed the Pi and caused crashes. This middleware queues the excess requests and lets them through gradually, rather than hammering the backend all at once.

## Configuration

| Option | Default | Description |
|---|---|---|
| `maxRequests` | `10` | Max concurrent requests passed to the service |
| `maxQueue` | `100` | Max requests held in the queue; excess requests receive a 429 |
| `maxWait` | `5s` | How long a queued request will wait for a slot before receiving a 429 |
