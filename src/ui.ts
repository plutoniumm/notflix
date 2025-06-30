import { rename } from "./video";

function renderSerie(dir, movies) {
    const ENC = encodeURIComponent;

    return movies
        .map(
            (mov) => `
    <li class="movie">
      <a href="?video=${ENC(`${dir}/${mov.name}`)}">
      <img class="rx10 m10" src="/images/${mov.key}.jpg" alt="${mov.name}" />
      <div class="mname">${rename(mov.name)}</div>
      </a>
    </li>
  `,
        )
        .join(" ");
}

type Serie = [string, string[]];

export function Lolomo(series, video) {
    let strings: string[] = [];
    let match = -1;

    const loop = Object.entries(series);
    for (let i = 0; i < loop.length; i++) {
        const [dir, movies] = loop[i] as Serie;
        let open = "";
        if (video.dir === dir) {
            match = i;
            open = "open";
        }

        if (!movies.length) continue;
        strings.push(`<li>
        <details class="p10" ${open}>
            <summary> ${dir} </summary>
            <ul class="series f w-100">
                ${renderSerie(dir, movies)}
            </ul>
        </details>
        </li>`);
    }

    if (match > 0) {
        const matched = strings.splice(match, 1);
        strings = matched.concat(strings);
    }

    return `<ul>${strings.join("")}</ul>`;
}
