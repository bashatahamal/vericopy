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
| Ground | `#f6f3ed` | `#171714` | Desktop workspace background |
| Surface | `#fbfaf7` | `#1d1d19` | Review and instructional surfaces |
| Ink | `#1c1a17` | `#eeeae1` | Primary text |
| Soft ink | `#555149` | `#beb8ad` | Secondary text |
| Faint ink | `#7b756b` | `#878177` | Nonessential metadata only |
| Accent | `#0b6b52` | `#55b394` | Verified state and primary action |
| Accent deep | `#084c3b` | `#174c3d` | Trust statement and primary hover |
| Gold detail | `#9b6815` | `#d1a04c` | Section numbers and structural detail |
| Border | `#dcd6ca` | `#393830` | Quiet separation |

The primary light theme combinations use near-black or deep green on warm
off-white. Gold is not used for body copy because it is a detail color, not a
general text color. The desktop app implements the dark palette and keeps the
same hierarchy and semantic roles in both themes.

## Typography

- Headings and product voice: bundled Source Serif 4 variable.
- Interface text and controls: bundled IBM Plex Sans variable.
- Paths, checksums, versions, ports, dates, modes, and technical status: bundled
  IBM Plex Mono regular.

The desktop fonts are bundled under the SIL Open Font License so the native
application keeps a stable offline identity across operating systems. The
repository banner remains portable and uses fallback fonts only.

## Desktop application

The desktop app uses the same editorial hierarchy as the repository assets: a
warm ground, one restrained green action, a gold hairline, serif product name,
and monospace technical facts. A transfer is presented as a reviewable sequence,
not an opaque progress gadget. Host identity, source and destination paths,
policy, byte count, checksum, and next action stay legible at every state.

The primary action remains green only when the request is locally valid. Red or
gold status always has accompanying plain-language text. The UI avoids
decorative gradients and excessive motion. Authentication is a visible choice:
recommended SSH key/agent authentication or a clearly labeled one-time password
that is never persisted by Vericopy.

The composition avoids generic dashboard conventions. Each view has one clear
focal task; readiness is a secondary preflight strip; working records use rows
instead of repeated cards; and the transfer form reads as a numbered sequence.
Monospace is reserved for technical facts rather than used as an all-purpose
visual texture.

The transfer manager follows the same restraint. Active, queued, paused, and
finished jobs use one continuous record list with compact progress and actions;
they do not become a grid of repeated download cards.

## Technical and diagnostic output

The supporting command adapter is an engineering surface, not part of the
desktop visual identity. It remains fully usable without color so redirected
output and non-interactive environments are unambiguous. Diagnostic code,
wording, and exit status remain authoritative.

JSON output contains no styling or progress animation. Human output uses short
headings, concrete paths, lower-noise status lines, and a direct `Next:` remedy.

## Motion and shape

Static repository assets use flat token colors, a 4 px corner radius, 1 px
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
- `cmd/vericopy-desktop/frontend/dist/fonts/`: unmodified Source Serif 4 and IBM
  Plex desktop font files, provenance hashes, and SIL Open Font licenses.
- Mermaid diagrams: use the same warm ground, green border, neutral ink, and
  gold detail roles.

No reference photo, favicon, portfolio thumbnail, or personal wordmark is
included in this repository.
