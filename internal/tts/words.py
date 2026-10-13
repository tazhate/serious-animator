"""Synthesize one sentence with edge-tts and record when every word starts.

Usage: words.py TEXT_FILE VOICE RATE OUT_MP3 OUT_JSON
The JSON is a list of {"text", "offset", "duration"}, times in seconds.
"""

import asyncio
import json
import sys
from pathlib import Path

import edge_tts

TICKS_PER_SECOND = 10_000_000  # edge-tts reports offsets in 100 ns units


async def synthesize(text: str, voice: str, rate: str, mp3: Path, out: Path) -> None:
    com = edge_tts.Communicate(text, voice, rate=rate, boundary="WordBoundary")
    words: list[dict[str, str | float]] = []
    with mp3.open("wb") as audio:
        async for chunk in com.stream():
            match chunk["type"]:
                case "audio":
                    audio.write(chunk["data"])
                case "WordBoundary":
                    words.append({
                        "text": chunk["text"],
                        "offset": chunk["offset"] / TICKS_PER_SECOND,
                        "duration": chunk["duration"] / TICKS_PER_SECOND,
                    })
    out.write_text(json.dumps(words, ensure_ascii=False), encoding="utf-8")


def main() -> None:
    text_file, voice, rate, mp3, out = sys.argv[1:6]
    text = Path(text_file).read_text(encoding="utf-8")
    asyncio.run(synthesize(text, voice, rate, Path(mp3), Path(out)))


if __name__ == "__main__":
    main()
