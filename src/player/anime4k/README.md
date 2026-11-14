Vendored from https://github.com/bloc97/Anime4K (MIT, header in each file),
unmodified. `mpvshader.ts` adapts them at runtime rather than editing them, so
they can be re-pulled from upstream by overwriting the file.

`Upscale_CNN_x2_M` is a 9-pass 2x super-resolution convnet; `Restore_CNN_M` is
an 8-pass denoise/deblock net. Both are trained on cel animation. Weights are
inline GLSL literals, which is why the files are large and why nothing is
fetched at runtime.

The live-action networks (FSRCNNX, NNEDI3) live in `../livenets/`.
