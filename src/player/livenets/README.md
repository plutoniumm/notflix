Live-action upscalers, vendored unmodified (LGPL-3.0, header in each file) so
they can be re-pulled by overwriting the file:

- `FSRCNNX_x2_16-0-4-1.glsl` — https://github.com/igv/FSRCNN-TensorFlow/releases/tag/1.1
- `nnedi3-nns64-win8x4.hook` — https://github.com/bjin/mpv-prescalers (master)

Both hook LUMA, so `mpvshader.ts` derives Rec.709 luma from the frame, runs the
network on that, and adds its brightness detail back onto a bilinear upscale of
the colour frame.

Measured 2026-11-12 on six 4K live-action frames (Revenge of the Sith, Obsession,
two DIRT episodes), each shrunk to half size and upscaled back on an M4 Max in
Brave, scored on luma PSNR against the original:

    through the real Upscaler (frame as video)   vs FSR 1.0
    FSRCNNX x2 16-0-4-1                           +0.59 dB  (ahead on 5/6, up to +2.43)
    NNEDI3 nns64                                  +0.01 dB  (level)
    Anime4K (animation net)                       -5.16 dB

    networks alone (frame as PNG)                 vs bilinear
    FSRCNNX                                       +3.03 dB  (6/6, up to +6.91)
    NNEDI3                                        +1.68 dB  (6/6)

A first run scored every method within ~0.1 dB of bilinear. That run was wrong:
the test PNGs carry cICP/gAMA tags and the browser colour-converted them on
upload, shifting every output darker than the original and swamping the
differences. Upload untagged pixels (`UNPACK_COLORSPACE_CONVERSION_WEBGL` =
`NONE`), as a video frame arrives, when measuring. Video-range vs full-range luma
made no difference to either network.
