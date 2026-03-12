<div align="center">
<img src="./public/assets/tight.svg" height="150" alt="Home Area Network" />
<hr/>
</div>

### Keyboard Shortcuts

| Key | Action |
| --- | --- |
| `p` | picture-in-picture |
| `b` | brightness toggle |
| `n` | next |
|  `m` | mute |
| `f` | toggle fullscreen |
| digit | seek to that*10 percent of vid |
| `d` | speed +0.1 |
| `s` | speed -0.1 |
| `w` | whisper subs |
| `c` | classical subs |
| `b` |  brightness toggle |
| `space` | play/pause |

### Requirements
All streaming is done in mp4, so files which aren't mp4s will be converted via ffmpeg. It also means you need some amount of free disk space to hold the converted files and the compute to do the conversion.

Automatic sub geneneration uses whisper.cpp, large, so a whisper installation and a decent GPU is recommended.

`make run`.

### Lic
2023 plutoniumm MIT License