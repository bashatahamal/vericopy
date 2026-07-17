# Brand and accessibility

Vericopy uses a restrained editorial system derived from the supplied Basha
Editorial reference. The project borrows its hierarchy, spacing, and color
roles, but does not copy personal imagery, project thumbnails, or identity
assets.

## Design intent

Vericopy should read as calm, exact, and accountable. Security claims use plain
language with visible constraints. Decoration never competes with paths,
checksums, permission modes, or remediation steps.

The wordmark is the product name in a serif face with a green period:
`Vericopy.` Metadata uses monospace. Gold appears only as a hairline or small
status detail, never as a primary action color.

## Derived tokens

| Role | Light | Dark | Use |
| --- | --- | --- | --- |
| Ground | `#faf9f7` | `#191917` | Documentation and banner background |
| Surface | `#ffffff` | `#21201d` | Terminal and card surfaces |
| Ink | `#1d1c1a` | `#e9e6df` | Primary text |
| Soft ink | `#52504b` | `#b3afa5` | Secondary text |
| Faint ink | `#8a877f` | `#7d7a71` | Nonessential metadata only |
| Accent | `#0e6e55` | `#4aa88c` | Verified state and identity period |
| Accent ink | `#0a5a46` | `#63bda2` | Links and readable accent text |
| Gold detail | `#a16f0b` | `#c9993f` | Hairlines, section numbers, small details |
| Border | `#e6e3dc` | `#34322d` | Quiet separation |

The primary light theme combinations use near-black or deep green on warm
off-white. Gold is not used for body copy because it is a detail color, not a
general text color. Dark-theme colors are reserved for any later documentation
site and must be retested in the exact surface.

## Typography

- Headings and editorial prose: Charter when licensed assets are intentionally
  distributed, otherwise Georgia and the documented serif fallback stack.
- Command chrome and controls: the platform system sans stack.
- Paths, checksums, versions, tags, dates, modes, and status labels: the platform
  monospace stack.

The repository banner uses fallbacks only and embeds no font file. This keeps the
asset portable and avoids transferring reference assets unnecessarily.

## CLI behavior

The CLI is fully usable without color. The current release emits no ANSI color,
which makes `--no-color`, redirected output, and non-interactive environments
equivalent. Future color may reinforce success or severity, but it must never be
the only carrier of meaning. Diagnostic code, wording, and exit status remain
authoritative.

JSON output contains no styling or progress animation. Human output uses short
headings, concrete paths, lower-noise status lines, and a direct `Next:` remedy.

## Motion and shape

Static repository assets use flat token colors, a 6 px corner radius, 1 px
hairlines, and no decorative gradients. An optional documentation site would
use near-zero motion, short hover transitions, and a full
`prefers-reduced-motion` opt-out.

## Voice

- Use sentence case.
- Make concrete claims that tests or code can support.
- State limitations near the guarantee they qualify.
- Avoid hype, novelty language, emoji, and security theater.
- Use plain hyphens for ranges and punctuation.
- Prefer a precise remedy over a generic error.

## Asset inventory

- `docs/assets/vericopy-banner.svg`: original project banner built from the
  derived token system and system font fallbacks.
- Mermaid diagrams: use the same warm ground, green border, neutral ink, and
  gold detail roles.

No reference photo, favicon, portfolio thumbnail, or personal wordmark is
included in this repository.

