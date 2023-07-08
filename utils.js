export const CSP = [
  "default-src *  data: 'unsafe-inline' 'unsafe-eval';",
  "script-src * data: 'unsafe-inline' 'unsafe-eval';",
  "img-src * data: 'unsafe-inline';",
  "font-src * data: 'unsafe-inline' 'unsafe-eval';",
  "worker-src * data: blob: 'unsafe-inline' 'unsafe-eval'"
].join( " " );
