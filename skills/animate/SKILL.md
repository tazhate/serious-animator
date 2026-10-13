---
name: animate
description: Make a narrated hand-drawn whiteboard explainer video (MP4) from a topic or question. Claude writes the storyboard, draws every scene as SVG, and the serious-animator renderer animates each stroke while edge-tts reads the narration. Use when the user asks for an explainer video, a whiteboard animation, or "animate/explain X as a video".
argument-hint: "<topic or question> [--scenes N] [--quality low|medium|high] [--voice NAME]"
allowed-tools: Bash(serious-animator *) Read Write Edit
---

# Whiteboard explainer video

Request: $ARGUMENTS

You produce three things: a storyboard, one SVG per scene, and the rendered
MP4. The `serious-animator` command (on PATH while this plugin is enabled)
checks your work, renders previews and encodes the video. It builds itself
with Go on first use, which takes a few seconds.

## Steps

1. **Project directory.** Use `./animations/<short-slug>/` under the current
   working directory, with a `scenes/` subdirectory. If it already exists and
   holds another topic, add `-2`, `-3`, and so on.

2. **Storyboard.** Read [references/storyboard.md](references/storyboard.md),
   then write `storyboard.json`. Narration language = the language of the
   request. Honour `--scenes N` and `--voice` if given.

3. **Scenes.** Read [references/svg-style.md](references/svg-style.md), then
   write `scenes/s01.svg`, `scenes/s02.svg`, ... Every element must carry the
   beat it belongs to (`data-beat` on it or on a parent `<g>`). Pick the
   tools per scene: a camera board for a mechanism with many parts, a spin or
   a move where motion is the point, a `circle@N` highlight for callbacks,
   `"dim": true` for busy scenes, `"music": "generated"` for the whole video.

4. **Validate.** Run `serious-animator validate --json <dir>`. Fix every
   error. Treat warnings as real problems too: an unspoken label, overlapping
   elements, or an element outside the canvas all hurt the video. Repeat
   until there are no errors.

5. **Preview.** Run `serious-animator preview --frames <dir>`. It writes
   `<dir>/preview/<scene>.png` with each beat's area outlined and numbered,
   and for camera boards `<scene>-bN.png` with what the camera shows in beat
   N. Read every PNG and check that:
   - nothing overlaps or is cut off at the edges;
   - captions are readable and sit under or beside their own icon;
   - the numbered boxes follow the narration order and do not cover each
     other badly;
   - the scene is not crowded into one corner;
   - on camera boards, every beat view shows its element and caption whole.

   Fix what you find and preview again. Two rounds are usually enough; stop
   after three.

6. **Render.** Run `serious-animator render <dir> --quality medium`, adding
   the user's `--quality` if given. The first render calls edge-tts once per
   beat. When it fails:
   - Finished clips are cached, so run the same command again; only missing
     clips are fetched.
   - `403` errors come in bursts from the speech service; wait a minute and
     run again.
   - If the same clips keep failing with `NoAudioReceived`, the voice is
     unavailable right now. List voices with
     `serious-animator voices --lang <code>`, set another `voice` in
     `storyboard.json` and render again.
   - `--no-audio` renders a silent video timed by text length, if the user
     only needs the animation.

7. **Report.** Give the output path, duration and scene count. Offer concrete
   follow-ups: change a scene, slow the voice (`"rate": "-10%"`), render in
   high quality.

## When something fails

- `serious-animator doctor` checks ffmpeg with libx264, edge-tts and the
  embedded font.
- Error messages name the file and line. Fix the SVG or storyboard and rerun
  the same command; do not work around the validator.
