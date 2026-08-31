# Custom domain via Cloudflare Tunnel

Merchants can serve their forms on their own domain using a Cloudflare
Tunnel — no Traefik config, no DNS records on our side, no open ports.

## How it works

```
Buyer → https://shop.yourdomain.com/f/test
      → Cloudflare (your zone)
      → cloudflared tunnel (runs anywhere: your PC, VPS, NAS)
      → JomForm API with Host header shop.yourdomain.com
```

JomForm resolves the form by the `Host` header, so **no JomForm-side
configuration** is needed beyond claiming the domain in Settings for the
verification badge.

## Steps for the merchant

1. In your Cloudflare account: **Zero Trust → Networks → Tunnels** → Create
   tunnel (cloudflared connector).
2. Public hostname mapping:
   - Subdomain: `shop` (or apex), Domain: `yourdomain.com`
   - Service type: `HTTP`, URL: `jomform-api.jomform.com` — **unless you run
     your own connector**; easiest is pointing the tunnel straight at:
     `https://jomform.com` (protocol HTTPS, no verify off needed as CF
     re-encrypts end-to-end).
3. In JomForm → Settings → Custom domain, add `shop.yourdomain.com` and
   press **Verify**. JomForm does an HTTP probe against your domain; when
   the tunnel routes correctly, it flips to VERIFIED.

## Docker compose example

```yaml
services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    command: tunnel --no-autoupdate run --token <YOUR_TUNNEL_TOKEN>
    restart: unless-stopped
```

## Notes

- TLS certificates are issued by Cloudflare automatically (`Full` mode is
  fine — the tunnel encrypts outbound too).
- The `Host` header must be preserved (default in cloudflared) so JomForm
  can map the domain to your account.
- Subpaths work: `yourdomain.com/f/<slug>` maps by host, no path
  rewrites needed.
- Remove the mapping in the CF dashboard to take the domain offline; then
  clear it here so your stats badge resets.