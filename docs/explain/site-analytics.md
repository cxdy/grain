---
title: Site analytics (grainvm.com)
description: Optional privacy choices for the documentation site — disabled by default.
---

The documentation site ships **without** analytics enabled.

## Options if you enable metrics later

| Option | Cookies | Notes |
|--------|---------|--------|
| **None** (default) | No | Best privacy; no setup |
| **[Plausible](https://plausible.io)** | No | Privacy-friendly, simple script; set `plausible: true` in `docs/_config.yml` |
| **Fathom / GoatCounter / Umami** | Usually no | Similar self-host or hosted privacy analytics |
| **Cloudflare Web Analytics** | No | If DNS is on Cloudflare |
| **Google Analytics** | Yes | Requires cookie consent / privacy policy in many jurisdictions |

To enable Plausible (example already wired):

```yaml
# docs/_config.yml
plausible: true
plausible_domain: grainvm.com
```

No change is required for a normal deploy of grainvm.com.
