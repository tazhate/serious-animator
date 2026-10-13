# Storyboard

`storyboard.json` is the script. Each scene is one whiteboard frame drawn
piece by piece while the narrator speaks. A scene is a list of **beats**.
Each beat is one spoken sentence plus the one simple element that gets drawn
while that sentence is spoken, so the picture always shows what the narrator
is saying at that moment.

## Format

```json
{
  "title": "How planes fly",
  "lang": "en",
  "voice": "en-US-AndrewMultilingualNeural",
  "rate": "+0%",
  "music": "generated",
  "scenes": [
    {
      "id": "s01",
      "title": "Why wings lift",
      "svg": "scenes/s01.svg",
      "beats": [
        { "label": "WING", "narration": "How does a heavy plane stay up? It starts with the shape of the wing." },
        { "label": "AIR", "narration": "As the plane moves forward, air flows over and under the wing.", "pause": 0.6 }
      ]
    }
  ]
}
```

| Field | Rules |
|---|---|
| `title` | Video title, required. |
| `lang` | Narration language code. Picks the default voice when `voice` is empty. |
| `voice` | edge-tts voice. Defaults: `ru` = `ru-RU-SvetlanaNeural`, `en` = `en-US-AndrewMultilingualNeural`. List voices with `serious-animator voices --lang ru`. |
| `rate` | Speech speed, `+0%`, `-10%`, `+5%`. |
| `scenes` | 1-12 scenes. Unknown fields are rejected, so mind typos. |
| `id` | Lowercase, `[a-z0-9_-]`, unique: `s01`, `s02`... |
| `svg` | Path relative to the project directory, ends in `.svg`. |
| `beats` | 2-8 beats per scene; 4-6 is the sweet spot. |
| `label` | 1-3 word caption of the element (`SUNLIGHT`, `ХЛОРОФИЛЛ`). |
| `narration` | One spoken sentence of 10-20 words, at most 300 characters. |
| `pause` | Optional, beat: seconds of silence after the sentence, 0-3. Use 0.5-0.8 after a key point so it lands. |
| `camera` | Optional, beat, camera boards only: `"board"` (whole board), `"hold"` (stay), `"#id"` (frame that element or group). Empty: frame what the beat draws. |
| `dim` | Optional, scene: `true` fades earlier beats while a new one is drawn, so the eye goes to the new part. The last beat brings everything back. Good for busy scenes and camera boards. |
| `transition` | Optional, scene: how it hands over to the next scene, `"swipe"` (default), `"fade"` or `"cut"`. |
| `camera` (scene) | Optional: `"auto"` or `"off"`. Empty: on when the SVG board is larger than one 1600x900 frame. |
| `music` | Optional: `"generated"` for a soft generated pad, or a music file path inside the project (`mp3`, `wav`, `ogg`, `m4a`, `flac`, `opus`), looped. It ducks under the voice. |
| `music_volume` | Optional: music level in dB, default -20, range -40 to -6. |
| `sfx` | Optional: `false` turns off the pop, whoosh and marker sounds. |

## Writing beats

- **Say the label.** The narration must contain the label's words, because
  the viewer reads the caption while hearing the sentence. If the label is
  WATER, say "water", not "moisture". The validator warns when a label word
  is not spoken. For Russian, inflected forms count ("корень" and "корню").
- **The label word starts the drawing.** The element is drawn from the moment
  the narrator says the label, so put the label word in the first half of
  the sentence when the element is big, and near the end when the sentence
  builds up to it. Either way the picture appears on the word.
- Short everyday words, the way a good teacher explains to a curious
  12-year-old.
- Do not describe what the drawing looks like. No stage directions, markdown
  or emoji: every character is read aloud.
- Spell out symbols the voice would mangle: "CO2" is fine, "→" is not.

## Scene rules

- The scene's main subject (the thing everything else acts on) is itself one
  of the beats, so arrows have something to point at.
- Arrows connect two elements of the same scene and must be factually right:
  inputs point into the subject, outputs point out of it.
- No repeated element or label within a scene.
- Describe recurring objects the same way in every scene, so they are drawn
  the same way.

## Story arc

The video must feel complete, not cut off.

- **Opening hook:** the first sentence is a question or a small paradox the
  viewer has noticed in real life ("Why doesn't a car scrub its tires in a
  turn, when its wheels travel different distances?"), not a definition. It
  names the topic and the question the video answers.
- **Concrete before abstract:** show one familiar example first, then the
  rule or the formula. A formula comes after the viewer has seen what each
  letter stands for.
- **One analogy:** for the hardest step, compare it with something everyday
  (a seesaw, a budget, a queue) and draw that thing next to the real one.
  One analogy per video, reused, beats three different ones.
- **One idea per scene.** If a scene needs two headings, split it.
- **Signposts:** short phrases that say where we are going ("First, ...",
  "Now let's look inside", "Here is the catch") at the start of scenes.
- **Callbacks:** when a later scene relies on an earlier element, redraw it
  the same way and say so ("remember the two arcs?"); a `circle@N` highlight
  points at it.
- **Middle:** each scene answers one step of that question. Its first sentence
  picks up where the previous scene ended.
- **Ending:** the last scene concludes. Its last beat is the takeaway: one
  plain sentence that answers the opening question and tells the viewer what
  to remember. Never end on a detail or an example. The takeaway beat still
  needs its own label and element.
- **Recurring character:** when the topic has a natural face (the person
  behind the idea, a historical figure), draw them as a simple friendly
  doodle that appears in the first scene, the last scene and about every other
  scene in between, with the same label and look every time. Topics without
  such a face need no character.
- Cover everything the request names. If it asks about two things, explain
  both and how they connect.

## Length and pace

3-5 scenes cover most topics: about 15-25 seconds of narration per scene,
1-2 minutes in total. Use `--scenes N` from the request when given.

The voice reads about 150 words a minute. Dense or technical scenes go
better slower: `"rate": "-10%"`. Give the key sentence of each scene a
`pause` instead of padding it with words.
