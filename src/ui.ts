function renderSerie(serie) {
    const dir = serie[0];
    const movies = serie[1];
    const ENC = encodeURIComponent;

    return movies
        .map(
            (mov) => `
    <li class="movie">
      <a href="?video=${ENC(`${dir}/${mov.name}`)}">
      <img class="rx10 m10" src="/images/${mov.key}.jpg" alt="${mov.name}" />
      <div>${mov.name.replace(".mp4", "")}</div>
      </a>
    </li>
  `,
        )
        .join(" ");
}

export function Lolomo(series) {
    return `
    <ul>
      ${Object.entries(series)
          .map(
              (serie) => `<li>
          <details open>
            <summary>
                ${serie[0]}
            </summary>
            <ul class="series f w-100">
              ${renderSerie(serie)}
            </ul>
          </details>
        </li>`,
          )
          .join("")}
    </ul>
  `;
}
