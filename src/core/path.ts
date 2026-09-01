// The root dir is spelled "." — "./name" is rejected by every API route.
export const join = (dir: string, name: string) =>
  !dir || dir === "." ? name : `${dir}/${name}`;
