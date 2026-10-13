# Drawing scenes in SVG

Every scene is one SVG file. The renderer draws each element as a marker
stroke along its path, then scribbles in its fill, in **document order**,
during the narration beat named by `data-beat`. The look is a sketch on warm
paper: every line wobbles a little and is drawn twice, ends taper like a real
marker, and most fills are hatched. Text is written glyph by glyph in a
handwriting font (Caveat, Latin and Cyrillic).

## Canvas

The storyboard's `format` sets the shape of the video. **Vertical is the
default**: most viewers watch on a phone.

| `format` | One frame (`viewBox`) | Camera board | Output, medium |
|---|---|---|---|
| `vertical` (default) | `0 0 900 1600` | `0 0 1800 3200` | 720x1280 |
| `horizontal` | `0 0 1600 900` | `0 0 3200 1800` | 1280x720 |

- Every scene SVG uses the frame of the storyboard's format. A landscape
  `viewBox` in a vertical video (or the other way round) is an error.
- A board larger than one frame, in the same shape, turns on the **camera**:
  it frames each beat in turn and pulls back to the whole board at the end of
  the scene. See "Camera boards" below.
- The paper background is drawn by the renderer; do not draw one.
- Keep everything at least 40 units from the edges.
- Sizes below are in viewBox units. One unit is the same size on screen in
  both formats, so fonts and stroke widths do not change; only the layout
  does.

## Vertical layout

A 900-unit-wide frame holds far less side by side than 1600. Lay vertical
scenes out as a column:

- **Top to bottom in narration order.** Rows of one icon, or two icons side
  by side at most. Arrows mostly run down.
- **Safe zone.** Players on phones put buttons and captions over the bottom
  and the right edge (Telegram, Shorts, Reels, TikTok). Keep everything that
  matters inside `x` 60-840 and `y` 60-1380; the band under 1380 is for a
  small detail at most.
- **Heading** at the top center, `y` around 140, `font-size` 64-72. Up to
  about 20 characters fit on a line; split a longer heading into two
  `<text>` lines (`y` 130 and 205) or shorten it.
- **Icons** are larger than in a wide frame: 220-340 units, about a third of
  the width, so they read on a small screen.
- **Captions** `font-size` 46-54 directly under their icon; at most about 14
  characters per line at 52.
- Three or four rows fill the frame. If a scene needs five or more steps,
  use a camera board (`0 0 1800 3200`) or split the scene.

## Style

- Ink: `stroke="#1e1e1e"`, `stroke-width` 5-6 for outlines, 4 for details.
- Fills come from one small palette and keep their meaning for the whole
  video (the same object is always the same color):

  | Color | Hex |
  |---|---|
  | blue | `#74c0fc` |
  | green | `#8ce99a` |
  | yellow | `#ffd43b` |
  | orange | `#ffa94d` |
  | pink | `#ffa8a8` |
  | gray | `#ced4da` |
  | white knockout (faces, paper, screens) | `#ffffff` |

- Accent red `#e03131` is for the one thing per scene that matters most: a
  warning, a mistake, the key arrow. Highlights use it automatically.
- Fills are **hatched** by default: a pale wash with diagonal scribbles in the
  fill color. Text, dark fills, white knockouts, translucent fills and
  small shapes (under about 50 units) stay flat on their own. Add
  `data-fill="solid"` to keep a larger shape flat, for example a thin bar, a
  small badge or a cell of a grid.
- Simple doodle icons, flat colors, no gradients, shadows, 3D or texture.
- Each icon is 150-300 units (220-340 in a vertical frame) and
  clearly separated from every other icon, caption and arrow by white space.
  Nothing touches or overlaps except an arrow tip near its target.
- Spread elements over the whole frame: top to bottom in narration order in
  a vertical video; left to right, or around the main subject in the middle,
  in a horizontal one.
- Heading at the top center: vertical `font-size` 64-72 at `y` around 140;
  horizontal `font-size` 70-80 at `y` around 110-120.
- Captions directly under or beside their own icon: `font-size` 44-56, the
  beat's `label` text, uppercase for Latin and Cyrillic alike.
- Arrows: a curved `path` with `fill="none"`, `stroke-width` 5, plus a
  separate open "V" path for the head, right after the shaft and in the same
  beat. The head is then recognized and pops in at the shaft's tip. Arrows
  stop short of what they connect.
- Draw only what the narration talks about: no decorations, borders or
  watermarks.

## Beats and order

- Wrap each beat's elements in `<g data-beat="N">`; `N` is the 1-based beat
  index from the storyboard. Elements inherit it from the nearest parent.
- Every element needs a beat and every beat needs at least one element (a
  highlight counts).
- The heading belongs to beat 1.
- The beat's caption text belongs to the same beat as its icon.
- An arrow belongs to the beat that talks about the relation, usually the
  beat of the element it points to.
- Inside a beat, order elements the way a hand would draw them: the main
  outline first, then inner details, then the caption.
- One `<path>` with several subpaths (`M ... M ...`) is drawn as one
  continuous motion; separate elements are drawn one after another.
- Drawing starts when the narrator says the beat's label word, so the
  element appears exactly when it is named.

## Animation attributes

Set these on an element or on a `<g>`; children inherit `data-fill`,
`data-anim` and `data-nodim`. Unknown `data-*` attributes are errors.

| Attribute | Effect |
|---|---|
| `data-fill="hachure"` / `"solid"` | force hatched or flat fill |
| `data-anim="pop"` | appears at once with a springy scale-in and a pop sound, instead of being drawn. For badges, icons that should "arrive", a lock snapping shut |
| `data-anim="fade"` | fades in |
| `data-highlight="circle@N"` | in beat N a red hand-drawn circle goes around this element, then fades when beat N ends. Use it to point back at something already drawn ("these small gears are the satellites") |
| `data-highlight="underline@N"` | the same with a line under the element; good on a caption or a formula |
| `data-nodim` | stays bright when the scene dims earlier beats (put it on the heading) |
| `data-spin="90"` | once the group is drawn it keeps turning, degrees per second, clockwise; negative is counter-clockwise. Gears, wheels, fans |
| `data-origin="x,y"` | spin center in the element's coordinates; default is the center of its box |
| `data-move="#guide-id"` | once drawn, the group travels along a guide path |
| `data-move="dx,dy"` | once drawn, the group slides by this offset |
| `data-move-dur="2"` | seconds for the move, default 2 |
| `data-orient="auto"` | the moving group turns with the guide's direction (a car on a curved road) |
| `data-motion-beat="N"` | start the spin or move when beat N starts, instead of when the group is drawn |
| `data-role="guide"` | on a `path` with an `id`: an invisible track for `data-move`. It is never drawn and needs no beat |

Motion rules:

- A moving group must be drawn at the guide's **start point**: the group
  is carried from there.
- Motions cannot be nested: a moving group may not contain another one.
- Use motion only where the motion is the explanation (gears meshing, a car
  turning, a piston pushing). Two moving things per scene at most.

```svg
<path id="road" data-role="guide" d="M800 455 A365 365 0 0 0 542 562" fill="none"/>
<g data-beat="1" data-move="#road" data-orient="auto" data-motion-beat="4" data-move-dur="3">
  <rect x="680" y="385" width="260" height="140" rx="40" fill="#74c0fc" stroke="#1e1e1e" stroke-width="6"/>
</g>
<g data-beat="2" data-spin="45" stroke="#1e1e1e" stroke-width="6">
  <polygon points="..." fill="#ffd43b"/>
</g>
```

## Camera boards

For a scene with many parts (a mechanism, a map, a process with stages), draw
a board larger than one frame and of the same shape: `viewBox="0 0 1800 3200"`
for vertical, `"0 0 3200 1800"` for horizontal. The camera then:

- frames each beat's elements with some margin, never closer than a view
  whose short side is 562 units (562x1000 vertical, 1000x562 horizontal), so
  captions stay readable;
- glides to the next beat (0.8-1.5 s) just before it is drawn, and stays put
  when the next beat is already in view;
- pulls back to the whole board at the end of the scene as a recap.

Rules for boards:

- Lay the board out like a map: related parts next to each other, the
  narration order following a path across it, not jumping corner to corner.
- Size text for the zoomed view: captions 60-70, heading 90-100.
- The beat camera can be overridden in the storyboard (`"camera": "board"`
  to show everything, `"hold"` to stay, `"#id"` to frame one element or
  group by id). Opening with `"board"` gives an overview before the tour.
- Check the beat views with `serious-animator preview --frames DIR`.

## Supported SVG

| Element | Notes |
|---|---|
| `g` | `transform`, inherited paint, `data-beat` and the attributes above |
| `path` | All commands: `M L H V C S Q T A Z`, absolute and relative |
| `rect` | `x y width height rx ry` |
| `circle`, `ellipse` | |
| `line`, `polyline`, `polygon` | |
| `text` | One line per element. `x y font-size text-anchor` (`start`, `middle`, `end`), `fill`. `tspan` positioning is ignored. |

Attributes: `fill`, `stroke`, `stroke-width`, `opacity`, `fill-opacity`,
`stroke-opacity`, `transform` (`translate scale rotate matrix skewX skewY`),
`style="..."` with the same properties. Colors: `#rgb`, `#rrggbb`,
`#rrggbbaa`, `rgb()`, common names. Lengths are plain numbers or `px`.

Not supported, reported as errors: `defs`, `use`, `symbol`, `style`
elements, `image`, `mask`, `clipPath`, `filter`, `pattern`, gradients
(`url(#...)`), markers (`marker-end`). Line caps and joins are always round.
The font has no `Δ`: draw it as a small triangle path.

**Default fill is black.** An open `path` with a stroke but no `fill`
attribute gets filled black, as in any SVG viewer. Always write
`fill="none"` on lines, arrows and open curves, or on their `<g>`.

## Example scene

Vertical (`format` unset or `"vertical"`):

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 900 1600">
  <g data-beat="1" stroke="#1e1e1e" stroke-width="6">
    <text x="450" y="145" font-size="68" text-anchor="middle" fill="#1e1e1e" stroke="none" data-nodim="">HOW PLANTS EAT</text>
    <circle cx="450" cy="390" r="95" fill="#ffd43b"/>
    <path d="M450 255 V230 M450 525 V550 M315 390 H290 M585 390 H610" fill="none" stroke-width="5"/>
    <text x="450" y="635" font-size="52" text-anchor="middle" fill="#1e1e1e" stroke="none">SUNLIGHT</text>
  </g>
  <g data-beat="2" stroke="#1e1e1e" stroke-width="5" fill="none">
    <path d="M450 670 Q470 740 450 800"/>
    <path d="M428 776 L450 805 L472 776"/>
  </g>
  <g data-beat="3" stroke="#1e1e1e" stroke-width="6">
    <path d="M450 1180 V900" fill="none"/>
    <path d="M450 1020 Q340 960 315 860 Q425 885 450 990" fill="#8ce99a"/>
    <path d="M450 970 Q560 910 585 810 Q475 835 450 940" fill="#8ce99a"/>
    <path d="M450 1180 Q410 1240 370 1255 M450 1180 Q490 1240 530 1260" fill="none" stroke-width="4"/>
    <text x="450" y="1340" font-size="52" text-anchor="middle" fill="#1e1e1e" stroke="none">PLANT</text>
  </g>
</svg>
```

Horizontal (`"format": "horizontal"`): the same ideas on `viewBox="0 0 1600
900"`, flowing left to right; see `examples/dns` in the plugin.

## Checking your drawing

`serious-animator preview <dir>` renders the finished frame of every scene
into `<dir>/preview/`. Colored boxes with numbers show each beat's area; on
camera boards gray boxes show what the camera frames for each beat. With
`--frames`, camera scenes also get `<scene>-bN.png`: the exact view of every
beat. Read the PNGs: they show what the viewer will see.
