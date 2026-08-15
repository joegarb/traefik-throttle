# traefik-throttle

[![CI](https://github.com/joegarb/traefik-throttle/actions/workflows/ci.yml/badge.svg)](https://github.com/joegarb/traefik-throttle/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/joegarb/traefik-throttle.svg)](https://pkg.go.dev/github.com/joegarb/traefik-throttle)
[![Release](https://img.shields.io/github/v/release/joegarb/traefik-throttle)](https://github.com/joegarb/traefik-throttle/releases)
[![License: MIT](https://img.shields.io/github/license/joegarb/traefik-throttle)](LICENSE)

Traefik middleware to limit concurrent requests routed to services by delaying (rather than rejecting) requests over the limit. Useful for underpowered servers in very low-traffic environments (ie. homelabs).

## Background

Originally built to stabilize NextCloud running on a Raspberry Pi 2 behind Traefik. NextCloud's web UI fires many concurrent requests, which overwhelmed the Pi and caused crashes. This middleware queues the excess requests and lets them through gradually, rather than hammering the backend all at once.

## How it works

- Up to `maxRequests` requests are passed to the backend at a time.
- Additional requests wait in a FIFO queue (up to `maxQueue`). When a slot frees, it is handed to the longest-waiting request, so newly arriving requests don't jump ahead of those already waiting.
- A queued request that waits longer than `maxWait`, or whose client disconnects, is dropped with `429 Too Many Requests` and a `Retry-After` header.

The goal is to *delay* bursts rather than reject them, smoothing load on a fragile backend.

## Installation

Add the plugin to Traefik's static configuration:

```yaml
experimental:
  plugins:
    throttle:
      moduleName: github.com/joegarb/traefik-throttle
      version: v0.3.1 # see the Releases page for the latest version
```

## Usage

Define a middleware and attach it to a router.

Dynamic configuration (file provider):

```yaml
http:
  middlewares:
    my-throttle:
      plugin:
        throttle:
          maxRequests: 10
          maxQueue: 100
          maxWait: 5s
  routers:
    my-router:
      rule: Host(`example.com`)
      service: my-service
      middlewares:
        - my-throttle
```

Docker labels:

```yaml
labels:
  - "traefik.http.middlewares.my-throttle.plugin.throttle.maxRequests=10"
  - "traefik.http.middlewares.my-throttle.plugin.throttle.maxQueue=100"
  - "traefik.http.middlewares.my-throttle.plugin.throttle.maxWait=5s"
  - "traefik.http.routers.my-router.middlewares=my-throttle"
```

## Configuration

| Option | Default | Description |
|---|---|---|
| `maxRequests` | `10` | Max concurrent requests passed to the service |
| `maxQueue` | `100` | Max requests held in the queue; excess requests receive a 429 |
| `maxWait` | `5s` | How long a queued request will wait for a slot before receiving a 429 |
| `verbose` | `false` | Log a line per queued/rejected request (off by default to avoid flooding logs under load) |
| `spacing` | `20ms` | Minimum gap between admissions to the service, staggering bursts so a slow upstream isn't hit all at once. `0s` disables it |

Rejections (429) include a `Retry-After` header derived from `maxWait`.

## Tuning

These are protection knobs, and the right values depend on **your** service and its traffic — the defaults suit a small, underpowered homelab app, but a busier or beefier backend will want different numbers. Watch how the app behaves under load and adjust:

- **`maxRequests`** limits *concurrency* — the number of requests in flight at the backend at once. Too high and you can exhaust its CPU/memory (e.g. PHP workers on a tiny box); too low and normal traffic queues needlessly.
- **`spacing`** limits *rate* — how fast requests are admitted. This is a separate dimension: `maxRequests` alone can't cap the rate, because fast requests free their slot quickly and get replaced, so far more than `maxRequests` can reach the backend each second. `spacing` staggers bursts so a slow upstream — or a slow dependency behind it, like network storage — isn't hammered all at once. It only adds delay during a burst; isolated requests pass straight through.
- **`maxQueue`** and **`maxWait`** bound how much is held back, and for how long, before excess requests are shed with a `429`.

A reasonable starting point for a fragile backend is a modest `maxRequests` with a small `spacing` (a few tens of ms), tuned from there: if the app struggles or errors under bursts, lower `maxRequests` or raise `spacing`; if it feels needlessly slow, do the opposite.
