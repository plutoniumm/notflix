// Library paths are dir-relative and the root dir is spelled "." — joining it
// naively yields "./name", which no API route accepts.
export const join = (dir: string, name: string) =>
  !dir || dir === "." ? name : `${dir}/${name}`;
