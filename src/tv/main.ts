// One bundle, two pages. The pathname decides which: /rc is the phone remote,
// everything else is the screen. Nothing here imports the main app — a TV
// serves the display and the remote, not the library UI.
// The design tokens (--tx-*, --bg-*, --red, --r-*, --ease-spring) and the
// atomic utilities both pages lean on. Imported here rather than <link>ed from
// tv.html so Vite inlines them into tv.css — one file for the TV to fetch, and
// nothing that can 404 independently.
import "../../public/assets/global.css";
import "../../public/assets/atomic.css";
import "./tv.css";

import { mount } from "svelte";
import Remote from "./Remote.svelte";
import Screen from "./Screen.svelte";

const remote = location.pathname.startsWith("/rc");

document.documentElement.dataset.tv = remote ? "rc" : "screen";

mount(remote ? Remote : Screen, {
  target: document.getElementById("tv")!,
});
